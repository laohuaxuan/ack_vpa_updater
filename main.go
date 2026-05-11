package main

import (
	"fmt"
	"os"
	"time"

	"ack_vpa_updater/pkg/ack"
	"ack_vpa_updater/pkg/config"
	"ack_vpa_updater/pkg/filter"
	"ack_vpa_updater/pkg/kubernetes"
	"ack_vpa_updater/pkg/notification"
	"ack_vpa_updater/pkg/persistence"
	"ack_vpa_updater/pkg/update"

	"k8s.io/client-go/dynamic"
)

func main() {
	fmt.Println("ACK VPA Updater 启动...")
	//加载配置
	cfg, err := config.LoadConfig("config.yaml")
	if err != nil {
		fmt.Printf("加载配置失败: %v\n", err)
		os.Exit(1)
	}

	filter, err := filter.NewFilter(&cfg.Filters)
	if err != nil {
		fmt.Printf("初始化过滤器失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("配置加载成功\n")
	fmt.Printf("   批处理大小: %d\n", cfg.UpdatePolicy.BatchSize)
	fmt.Printf("   成功阈值: %.0f%%\n", cfg.UpdatePolicy.SuccessRateThreshold*100)
	fmt.Printf("   Pod 就绪超时时间: %d\n", cfg.UpdatePolicy.PodReadyTimeout)
	fmt.Printf("   检查间隔时间: %d\n", cfg.UpdatePolicy.CheckInterval)
	fmt.Printf("   安全冗余值: %.2f\n", cfg.UpdatePolicy.SafetyRedundancy)

	// 初始化 Kubernetes 动态客户端
	// 支持 kubeconfig 文件和 in-cluster (ServiceAccount) 两种模式
	var dynamicClient *dynamic.DynamicClient
	if cfg.InCluster {
		fmt.Println("使用 in-cluster 模式 (ServiceAccount) 连接 Kubernetes")
		dynamicClient, err = kubernetes.InitClientWithInCluster()
	} else {
		fmt.Printf("使用 kubeconfig 模式连接 Kubernetes: %s\n", cfg.Kubeconfig)
		dynamicClient, err = kubernetes.InitClientWithKubeconfig(cfg.Kubeconfig)
	}
	if err != nil {
		fmt.Printf("初始化 K8s 客户端失败: %v\n", err)
		os.Exit(1)
	}
	//判断是否启用资源画像
	profile, err := ack.GetRecommendationProfile(dynamicClient)
	if err != nil {
		fmt.Printf("获取资源画像失败: %v\n", err)
		os.Exit(1)
	}

	if !profile.Enabled {
		fmt.Printf("未开启资源画像，无法进行分析\n")
		os.Exit(1)
	}
	fmt.Println("已开启资源画像")

	//定时执行更新任务
	duration := time.Duration(cfg.UpdatePolicy.CheckInterval) * time.Second
	ticker := time.NewTicker(duration)
	defer ticker.Stop()
	for range ticker.C {
		//防止阻塞到下一个周期，每个周期独立执行
		go func() {
			defer func() {
				if err := recover(); err != nil {
					fmt.Printf("任务执行发生panic：%\v\n", err)
					// 发送告警：notification.SendErrorNotification(err)
				}
			}()
			err := updateTask(dynamicClient, cfg, filter)
			if err != nil {
				fmt.Printf("执行任务失败: %v\n", err)
			}
		}()
	}
}

func updateTask(dynamicClient *dynamic.DynamicClient, cfg *config.Config, filter *filter.Filter) error {
	fmt.Printf("============开始执行任务: %v============\n", time.Now())
	result := &update.UpdateResult{
		StartTime: time.Now().Format(time.RFC3339),
		Records:   make([]update.UpdateRecord, 0),
	}

	namespaces, err := kubernetes.GetNamespace(dynamicClient)
	if err != nil {
		fmt.Printf("获取命名空间失败: %v\n", err)
		return err
		// os.Exit(1)
	}

	fmt.Printf("发现 %d 个命名空间\n", len(namespaces))

	//循环namespace
	for _, ns := range namespaces {
		if !filter.ShouldProcessNamespace(ns) {
			fmt.Printf("跳过命名空间: %s\n", ns)
			continue
		}

		fmt.Printf("\n处理命名空间: %s\n", ns)

		//获取命名空间下的推荐资源（recommendation）
		recommendations, err := ack.GetRecommendations(dynamicClient, ns, cfg.UpdatePolicy.SafetyRedundancy)
		if err != nil {
			fmt.Printf("获取推荐列表失败: %v\n", err)
			continue
		}

		fmt.Printf("发现 %d 个推荐项\n", len(recommendations))

		toUpdate := make([]ack.Recommendation, 0)
		for _, rec := range recommendations {
			if !filter.ShouldProcessDeployment(rec.Namespace, rec.DeployName) {
				fmt.Printf("跳过 Deployment: %s\n", rec.DeployName)
				continue
			}
			toUpdate = append(toUpdate, rec)
			fmt.Printf("需要更新的 Deployment: %s\n", rec.DeployName)
		}

		if len(toUpdate) == 0 {
			fmt.Printf("没有需要更新的 Deployment\n")
			continue
		}

		batchResult := update.ProcessNamespaceBatch(dynamicClient, ns, toUpdate, cfg.UpdatePolicy)
		result.Records = append(result.Records, batchResult.Records...)
		result.SuccessCount += batchResult.SuccessCount
		result.FailureCount += batchResult.FailureCount
		result.TotalCount += batchResult.TotalCount
	}

	result.EndTime = time.Now().Format(time.RFC3339)

	fmt.Printf("\n更新统计:\n")
	fmt.Printf("   总数: %d\n", result.TotalCount)
	fmt.Printf("   成功: %d\n", result.SuccessCount)
	fmt.Printf("   失败: %d\n", result.FailureCount)

	if cfg.Persistence.Enabled {
		if err := persistence.SaveResults(cfg, result); err != nil {
			fmt.Printf("保存结果失败: %v\n", err)
		} else {
			if cfg.Persistence.Type == "mysql" {
				fmt.Printf("结果已保存到MySQL数据库: %s\n", cfg.Persistence.MySQL.Database)
			} else {
				fmt.Printf("结果已保存到: %s\n", cfg.Persistence.DataDir)
			}
		}
	}

	if cfg.Feishu.Enabled {
		if err := notification.SendFeishuNotification(cfg.Feishu, result); err != nil {
			fmt.Printf("发送飞书通知失败: %v\n", err)
		} else {
			fmt.Printf("飞书通知已发送\n")
		}
	}

	fmt.Printf("============完成任务: %v============\n", time.Now())
	return nil
}

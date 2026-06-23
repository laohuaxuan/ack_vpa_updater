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
	cfg, err := config.LoadConfig("./config/config.yaml")
	if err != nil {
		fmt.Printf("加载配置失败: %v\n", err)
		os.Exit(1)
	}

	// 设置全局配置
	config.SetConfig(cfg)
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

	// 启动配置文件监控（热更新）
	err = config.WatchConfig("config.yaml", func(newConfig *config.Config) {
		fmt.Printf("\n配置已更新:\n")
		fmt.Printf("   批处理大小: %d\n", newConfig.UpdatePolicy.BatchSize)
		fmt.Printf("   成功阈值: %.0f%%\n", newConfig.UpdatePolicy.SuccessRateThreshold*100)
		fmt.Printf("   Pod 就绪超时时间: %d\n", newConfig.UpdatePolicy.PodReadyTimeout)
		fmt.Printf("   检查间隔时间: %d\n", newConfig.UpdatePolicy.CheckInterval)
		fmt.Printf("   安全冗余值: %.2f\n", newConfig.UpdatePolicy.SafetyRedundancy)
	})
	if err != nil {
		fmt.Printf("启动配置监控失败: %v\n", err)
	}
	fmt.Println("已启动配置监控")

	// 动态定时器 - 支持配置热更新
	runWithDynamicTicker(dynamicClient)
}

// runWithDynamicTicker 使用动态间隔时间的定时器
func runWithDynamicTicker(dynamicClient *dynamic.DynamicClient) {
	var ticker *time.Ticker
	var tickerChan <-chan time.Time

	// 初始化定时器
	updateTicker := func() {
		cfg := config.GetConfig()
		if cfg == nil {
			cfg = &config.Config{}
			cfg.UpdatePolicy.CheckInterval = 300 // 默认5分钟
		}
		duration := time.Duration(cfg.UpdatePolicy.CheckInterval) * time.Second

		if ticker != nil {
			ticker.Stop()
		}
		ticker = time.NewTicker(duration)
		tickerChan = ticker.C
		fmt.Printf("定时器已更新，间隔: %v\n", duration)
	}

	// 初始创建定时器
	updateTicker()
	defer ticker.Stop()

	// 创建配置变化监听通道
	configChangeChan := make(chan struct{}, 1)
	go func() {
		lastCheckInterval := config.GetConfig().UpdatePolicy.CheckInterval
		for {
			time.Sleep(5 * time.Second) // 每5秒检查一次配置变化
			cfg := config.GetConfig()
			if cfg != nil && cfg.UpdatePolicy.CheckInterval != lastCheckInterval {
				lastCheckInterval = cfg.UpdatePolicy.CheckInterval
				select {
				case configChangeChan <- struct{}{}:
				default:
				}
			}
		}
	}()

	for {
		select {
		case <-tickerChan:
			// 执行更新任务
			go func() {
				defer func() {
					if err := recover(); err != nil {
						fmt.Printf("任务执行发生panic：%v\n", err)
					}
				}()
				// 使用全局配置（支持热更新）
				currentConfig := config.GetConfig()
				err := updateTask(dynamicClient, currentConfig)
				if err != nil {
					fmt.Printf("执行任务失败: %v\n", err)
				}
			}()

		case <-configChangeChan:
			// 配置变化，更新定时器
			fmt.Println("检测到检查间隔配置变化，更新定时器...")
			updateTicker()
		}
	}
}

func updateTask(dynamicClient *dynamic.DynamicClient, cfg *config.Config) error {
	// 根据最新配置创建 filter
	fl, err := filter.NewFilter(&cfg.Filters)
	if err != nil {
		return fmt.Errorf("创建过滤器失败: %v", err)
	}
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
		if !fl.ShouldProcessNamespace(ns) {
			//fmt.Printf("跳过命名空间: %s\n", ns)
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
			if !fl.ShouldProcessDeployment(rec.Namespace, rec.ResourceName) {
				//fmt.Printf("跳过 Deployment: %s\n", rec.DeployName)
				continue
			}
			toUpdate = append(toUpdate, rec)
			fmt.Printf("需要更新的 Deployment: %s\n", rec.ResourceName)
			//显示最新的值
			fmt.Printf("   CPU-request: %v\n", rec.Containers[0].Request["cpu"])
			fmt.Printf("   Memory-request: %v\n", rec.Containers[0].Request["memory"])
			fmt.Printf("   CPU-limit: %v\n", rec.Containers[0].Limit["cpu"])
			fmt.Printf("   Memory-limit: %v\n", rec.Containers[0].Limit["memory"])

		}

		if len(toUpdate) == 0 {
			fmt.Printf("没有需要更新的 Deployment\n")
			continue
		}
		//处理命名空间下的推荐项
		batchResult := update.ProcessNamespaceBatch(dynamicClient, ns, toUpdate, cfg.UpdatePolicy)
		result.Records = append(result.Records, batchResult.Records...)
		result.SuccessCount += batchResult.SuccessCount
		result.FailureCount += batchResult.FailureCount
		result.TotalCount += batchResult.TotalCount
	}
	result.Cluster = cfg.Cluster
	result.EndTime = time.Now().Format(time.RFC3339)

	fmt.Printf("\n更新统计:\n")
	fmt.Printf("   集群: %s\n", result.Cluster)
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

package update

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"ack_vpa_updater/pkg/ack"
	"ack_vpa_updater/pkg/config"
	"ack_vpa_updater/pkg/kubernetes"
	"ack_vpa_updater/utils"
)

// 处理更新操作和批次管理
type UpdateRecord struct {
	Kind          string            `json:"kind"` // 资源类型
	Timestamp     string            `json:"timestamp"`
	Namespace     string            `json:"namespace"`
	ResourceName  string            `json:"resource_name"`
	ContainerName string            `json:"container_name"`
	Request       map[string]string `json:"request"` // request资源推荐值
	Limit         map[string]string `json:"limit"`   // limit资源推荐值
	Status        string            `json:"status"`
	Error         string            `json:"error,omitempty"`
	OrignRequest  map[string]string `json:"or_request,omitempty"` // 原始request
	OrignLimit    map[string]string `json:"or_limit,omitempty"`   // 原始limit
}

type UpdateResult struct {
	Cluster      string         `json:"cluster"` // 集群名称
	TotalCount   int            `json:"total_count"`
	SuccessCount int            `json:"success_count"`
	FailureCount int            `json:"failure_count"`
	Records      []UpdateRecord `json:"records"`
	StartTime    string         `json:"start_time"`
	EndTime      string         `json:"end_time"`
}

func ProcessNamespaceBatch(dynamicClient *dynamic.DynamicClient, namespace string, recommendations []ack.Recommendation, policy config.UpdatePolicy) *UpdateResult {
	result := &UpdateResult{
		Records: make([]UpdateRecord, 0),
	}

	totalBatches := (len(recommendations) + policy.BatchSize - 1) / policy.BatchSize

	for i := 0; i < len(recommendations); i += policy.BatchSize {
		batchNum := i/policy.BatchSize + 1
		end := i + policy.BatchSize
		if end > len(recommendations) {
			end = len(recommendations)
		}

		batch := recommendations[i:end]
		fmt.Printf("  处理批次 %d/%d (%d 个)\n", batchNum, totalBatches, len(batch))

		batchRecords := ProcessBatch(dynamicClient, namespace, batch)
		result.Records = append(result.Records, batchRecords...)
		result.SuccessCount += CountSuccess(batchRecords)
		result.FailureCount += CountFailure(batchRecords)
		result.TotalCount += len(batchRecords)

		successRate := float64(CountSuccess(batchRecords)) / float64(len(batchRecords))
		fmt.Printf("     批次成功率: %.1f%%\n", successRate*100)

		if successRate >= policy.SuccessRateThreshold {
			fmt.Printf("  批次 %d 达到成功阈值，继续下一批\n", batchNum)
		} else {
			fmt.Printf("  批次 %d 未达到成功阈值，继续等待...\n", batchNum)
			WaitForNextCheck(dynamicClient, namespace, policy)
		}
	}

	return result
}

func ProcessBatch(dynamicClient *dynamic.DynamicClient, namespace string, batch []ack.Recommendation) []UpdateRecord {
	records := make([]UpdateRecord, 0)
	for _, rec := range batch {
		for _, container := range rec.Containers {
			record := UpdateRecord{
				Timestamp:     time.Now().Format(time.RFC3339),
				Namespace:     namespace,
				ResourceName:  rec.ResourceName,
				ContainerName: container.ContainerName,
				Request:       container.Request,
				Limit:         container.Limit,
				OrignRequest:  container.OriginRequest,
				OrignLimit:    container.OriginLimit,
				Status:        "success",
				Kind:          container.Kind, // 资源类型
			}
			err := UpdateDeploymentResources(dynamicClient, namespace, rec.ResourceName, container)
			if err != nil {
				record.Status = "failure"
				record.Error = err.Error()
				fmt.Printf("     更新失败 %s/%s: %v\n", namespace, rec.ResourceName, err)
			} else {
				fmt.Printf("     更新成功 %s/%s - %s: requestCPU=%v, limitCPU=%v, requestMem=%s, limitMem=%s\n",
					namespace, rec.ResourceName, container.ContainerName, container.Request["cpu"], container.Limit["cpu"], container.Request["memory"], container.Limit["memory"])
			}
			records = append(records, record)
		}
	}
	return records
}

func UpdateDeploymentResources(dynamicClient *dynamic.DynamicClient, namespace, resourceName string, container ack.ContainerRecommendation) error {
	ctx := context.Background()
	//定义Deployment的GVR资源
	var resourceGVR schema.GroupVersionResource
	switch container.Kind {
	case "Deployment":
		// 定义 Deployment 的 GVR 资源
		resourceGVR = schema.GroupVersionResource{
			Group:    "apps",
			Version:  "v1",
			Resource: "deployments",
		}
	case "StatefulSet":
		// 定义 StatefulSet 的 GVR 资源
		resourceGVR = schema.GroupVersionResource{
			Group:    "apps",
			Version:  "v1",
			Resource: "statefulsets",
		}
	default:
		return fmt.Errorf("不支持的资源类型: %s", container.Kind)
	}

	// deploymentGVR := schema.GroupVersionResource{
	// 	Group:    "apps",
	// 	Version:  "v1",
	// 	Resource: "deployments",
	// }

	//获取Kind资源
	deploy, err := dynamicClient.Resource(resourceGVR).Namespace(namespace).Get(ctx, resourceName, metav1.GetOptions{})
	if err != nil {
		fmt.Printf("     获取 %s %s/%s 失败: %v\n", container.Kind, namespace, resourceName, err)
		return err
	}
	//获取Kind资源的容器资源
	containers, found, err := unstructured.NestedSlice(deploy.Object, "spec", "template", "spec", "containers")
	if err != nil || !found {
		return fmt.Errorf("未找到容器列表或格式错误：%v\n", err)
	}

	targetIndex := -1
	//遍历查找目标容器
	for i, c := range containers {
		containerMap, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		if containerName := utils.GetString(containerMap, "name"); containerName == container.ContainerName {
			targetIndex = i
			//解析资源数量
			cpuQtyRequest, err := resource.ParseQuantity(container.Request["cpu"])
			if err != nil {
				return fmt.Errorf("解析 CPU request资源失败: %v\n", err)
			}
			cpuQtyLimit, err := resource.ParseQuantity(container.Limit["cpu"])
			if err != nil {
				return fmt.Errorf("解析 CPU limit资源失败: %v\n", err)
			}
			memQtyRequset, err := resource.ParseQuantity(container.Request["memory"])
			if err != nil {
				return fmt.Errorf("解析 Memory request资源失败: %v\n", err)
			}
			memQtyLimit, err := resource.ParseQuantity(container.Limit["memory"])
			if err != nil {
				return fmt.Errorf("解析 Memory limit资源失败: %v\n", err)
			}

			// 更新容器资源
			cpuRequestStr := cpuQtyRequest.String()
			cpuLimitStr := cpuQtyLimit.String()
			memRequestStr := memQtyRequset.String()
			memLimitStr := memQtyLimit.String()

			// 修改容器Map中的资源
			resources := make(map[string]interface{})
			resources["requests"] = map[string]interface{}{
				"cpu":    cpuRequestStr,
				"memory": memRequestStr,
			}
			resources["limits"] = map[string]interface{}{
				"memory": memLimitStr,
				"cpu":    cpuLimitStr,
			}
			containerMap["resources"] = resources
			containers[i] = containerMap
			break
		}
	}

	if targetIndex == -1 {
		return fmt.Errorf("未找到匹配的容器 %s\n", container.ContainerName)
	}

	// 将更新后的containers设置回deploy.Object
	if err := unstructured.SetNestedSlice(deploy.Object, containers, "spec", "template", "spec", "containers"); err != nil {
		return fmt.Errorf("设置容器列表失败: %v\n", err)
	}

	//更新Kind资源
	if _, err := dynamicClient.Resource(resourceGVR).Namespace(namespace).Update(ctx, deploy, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("更新 %s %s/%s 失败: %v\n", container.Kind, namespace, resourceName, err)
	}

	return nil
}

func WaitForNextCheck(dynamicClient *dynamic.DynamicClient, namespace string, policy config.UpdatePolicy) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(policy.PodReadyTimeout)*time.Second)
	defer cancel()

	ticker := time.NewTicker(time.Duration(policy.CheckInterval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			fmt.Printf("     等待超时\n")
			return
		case <-ticker.C:
			ready, total, err := kubernetes.CheckPodsReady(dynamicClient, namespace)
			if err != nil {
				fmt.Printf("     检查 Pod 状态失败: %v\n", err)
				continue
			}

			rate := float64(ready) / float64(total) * 100
			fmt.Printf("     Pod 就绪率: %d/%d (%.1f%%)\n", ready, total, rate)

			if rate >= policy.SuccessRateThreshold*100 {
				return
			}
		}
	}
}

func CountSuccess(records []UpdateRecord) int {
	count := 0
	for _, r := range records {
		if r.Status == "success" {
			count++
		}
	}
	return count
}

func CountFailure(records []UpdateRecord) int {
	count := 0
	for _, r := range records {
		if r.Status == "failure" {
			count++
		}
	}
	return count
}

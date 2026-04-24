package ack

import (
	"context"
	"fmt"
	"math"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"ack_vpa_updater/utils"
)

// 处理ack资源画像相关操作
type RecommendationProfile struct {
	// Namespace string `json:"namespace"`
	Enabled bool `json:"enabled"`
}

type Recommendation struct {
	Namespace   string                    `json:"namespace"`
	DeployName  string                    `json:"deploy_name"`
	ClusterName string                    `json:"cluster_name"`
	Containers  []ContainerRecommendation `json:"containers"`
	// OriginalTarget OriginalTarget            `json:"original_target"`
}

//原始目标
//P95: 95%分位数
//P99: 99%分位数
// type OriginalTarget struct {
// 	P95 map[string]string `json:"p95"`
// 	P99 map[string]string `json:"p99"`
// }

// 操作类型
// Increase: 增加资源
// Decrease: 减少资源
// Maintain/None: 保持当前资源
type Labels struct {
	CPUOperation    string `json:"cpu_operation"`
	MemoryOperation string `json:"memory_operation"`
}

// 条件推荐
// LowConfidence: 低置信度
// RecommendationProvided: 已提供推荐
// NoPodsMatched: 未匹配到Pod
type ConditionRecommendation struct {
	LowConfidence          string `json:"low_confidence"`
	RecommendationProvided string `json:"recommendation_provided"`
	NoPodsMatched          string `json:"no_pods_matched"`
}

// 容器推荐值
// ContainerName: 容器名称
// Target: request资源推荐值
type ContainerRecommendation struct {
	ContainerName string            `json:"container_name"`
	Request       map[string]string `json:"request"`
	Limit         map[string]string `json:"limit"`
}

func GetRecommendationProfile(dynamicClient *dynamic.DynamicClient) (*RecommendationProfile, error) {
	// 定义 Recommendation 资源的 GroupVersionResource (GVR)
	recommendationProfileGVR := schema.GroupVersionResource{
		Group:    "autoscaling.alibabacloud.com",
		Version:  "v1alpha1",
		Resource: "recommendationprofiles",
	}
	// 获取 RecommendationProfile 资源
	recommendationProfileList, err := dynamicClient.Resource(recommendationProfileGVR).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	if len(recommendationProfileList.Items) == 0 {
		return nil, nil
	}
	return &RecommendationProfile{
		// Namespace: namespace,
		Enabled: true,
	}, nil
}

func GetRecommendations(dynamicClient *dynamic.DynamicClient, namespace string, safetyRedundancy float64) ([]Recommendation, error) {
	// 定义 Recommendation 资源的 GroupVersionResource (GVR)
	recommendationGVR := schema.GroupVersionResource{
		Group:    "autoscaling.alibabacloud.com",
		Version:  "v1alpha1",
		Resource: "recommendations",
	}
	// 获取 Recommendation 资源
	recommendationList, err := dynamicClient.Resource(recommendationGVR).Namespace(namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	if len(recommendationList.Items) == 0 {
		fmt.Printf("在namespace：%s下未找到recommendations资源\n", namespace)
		return nil, nil
	}
	fmt.Printf("在namespace: %s下找到%d个recommendations资源\n", namespace, len(recommendationList.Items))

	recommendations := make([]Recommendation, 0)
	for _, rec := range recommendationList.Items {
		//获取deployment name
		deployName, found, err := unstructured.NestedString(rec.Object, "spec", "workloadRef", "name")
		if err != nil {
			fmt.Printf("解析deployName失败：%v\n", err)
			continue
		}
		if !found {
			fmt.Println("未找到deployName字段")
			continue
		}
		//判断是否需要资源变更
		labels := getLabels(&rec)
		if labels.CPUOperation == "" || labels.MemoryOperation == "" {
			fmt.Printf("deployment资源 %s/%s 未找到cpu_operation或memory_operation标签，跳过\n", deployName, namespace)
			continue
		}
		if (labels.CPUOperation == "Maintain" || labels.CPUOperation == "None") && (labels.MemoryOperation == "Maintain" || labels.MemoryOperation == "None") {
			continue
		}
		//解析conditions
		//判断recommendation是否需要更新
		//LowConfidence == false && RecommendationProvided == true  完美状态，推荐更新
		//LowConfidence == true && RecommendationProvided == true   数据不足，暂不更新
		//LowConfidence == false && RecommendationProvided == false 在计算中，暂不更新
		//LowConfidence == true && RecommendationProvided == false  有异常，需检查日志
		conditionsRecommendation := parseCondition(&rec)
		if conditionsRecommendation.NoPodsMatched == "True" {
			fmt.Printf("deployment资源 %s/%s 不存在，跳过\n", deployName, namespace)
			continue
		} else if conditionsRecommendation.LowConfidence == conditionsRecommendation.RecommendationProvided {
			fmt.Printf("deployment资源 %s/%s 低置信，跳过\n", deployName, namespace)
			continue
		} else if conditionsRecommendation.LowConfidence == "True" && conditionsRecommendation.RecommendationProvided == "False" {
			fmt.Printf("deployment资源 %s/%s 高置信，但跳过推荐，有异常\n", deployName, namespace)
			continue
		}

		//解析recommendation
		rec, err := parseContainerRecommendation(&rec, namespace, deployName, safetyRedundancy)
		if err != nil {
			return nil, err
		}
		recommendations = append(recommendations, rec)
	}
	return recommendations, nil
}

// 解析conditions
func parseCondition(rec *unstructured.Unstructured) ConditionRecommendation {
	conditions, found, err := unstructured.NestedSlice(rec.Object, "status", "conditions")
	if err != nil {
		fmt.Printf("解析conditions失败：%v\n", err)
		return ConditionRecommendation{}
	}
	if !found {
		fmt.Println("未找到conditions字段")
		return ConditionRecommendation{}
	}
	conditionsRecommendation := ConditionRecommendation{}
	for _, item := range conditions {
		//类型断言map
		cond, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		//提取字段
		condType := utils.GetString(cond, "type")
		status := utils.GetString(cond, "status")
		switch condType {
		case "LowConfidence":
			conditionsRecommendation.LowConfidence = status
		case "RecommendationProvided":
			conditionsRecommendation.RecommendationProvided = status
		case "NoPodsMatched":
			conditionsRecommendation.NoPodsMatched = status
		}
	}
	return conditionsRecommendation
}

// 解析recommendation
func parseContainerRecommendation(rec *unstructured.Unstructured, namespace string, deployName string, safetyRedundancy float64) (Recommendation, error) {
	//获取深层嵌套数组
	containerRecommendations, found, err := unstructured.NestedSlice(rec.Object, "status", "recommendResources", "containerRecommendations")
	if err != nil {
		fmt.Printf("解析containerRecommendations失败：%v\n", err)
		return Recommendation{}, err
	}
	if !found {
		fmt.Println("未找到containerRecommendations字段")
		return Recommendation{}, nil
	}
	for _, item := range containerRecommendations {
		// 解析item为map[string]interface{}
		containerRecommendation, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		//获取容器名称
		containerName := utils.GetString(containerRecommendation, "containerName")

		//获取target推荐值
		if target, found, _ := unstructured.NestedMap(containerRecommendation, "target"); found {
			requests, limits, err := calculateRecommendation(target, safetyRedundancy)
			if err != nil {
				continue
			}

			// 构建请求和限制映射
			requestMap := make(map[string]string)
			limitMap := make(map[string]string)

			// 处理CPU
			if val, ok := requests["cpu"]; ok {
				if cpuStr, ok := val.(string); ok {
					requestMap["cpu"] = cpuStr
				}
			}
			if val, ok := limits["cpu"]; ok {
				if cpuStr, ok := val.(string); ok {
					limitMap["cpu"] = cpuStr
				} else if cpuInt, ok := val.(int64); ok {
					limitMap["cpu"] = fmt.Sprintf("%dm", cpuInt)
				}
			}

			// 处理内存
			if val, ok := requests["memory"]; ok {
				if memStr, ok := val.(string); ok {
					requestMap["memory"] = memStr
				} else if memInt, ok := val.(int64); ok {
					requestMap["memory"] = fmt.Sprintf("%dMi", memInt)
				}
			}
			if val, ok := limits["memory"]; ok {
				if memStr, ok := val.(string); ok {
					limitMap["memory"] = memStr
				} else if memInt, ok := val.(int64); ok {
					limitMap["memory"] = fmt.Sprintf("%dMi", memInt)
				}
			}

			// 只有当请求和限制都有值时才返回
			if len(requestMap) > 0 && len(limitMap) > 0 {
				return Recommendation{
					Namespace:   namespace,
					DeployName:  deployName,
					ClusterName: "ack-cluster",
					Containers: []ContainerRecommendation{
						{
							ContainerName: containerName,
							Request:       requestMap,
							Limit:         limitMap,
						},
					},
				}, nil
			}
		}
	}
	return Recommendation{}, fmt.Errorf("未找到target字段")
}

// 获取标签
func getLabels(rec *unstructured.Unstructured) Labels {
	labels := rec.GetLabels()
	if labels == nil {
		fmt.Println("未找到labels字段")
		return Labels{}
	}
	return Labels{
		CPUOperation:    labels["alpha.alibabacloud.com/recommendation-cpu-operation"],
		MemoryOperation: labels["alpha.alibabacloud.com/recommendation-memory-operation"],
	}
}

// 计算推荐值
// memory: {request = limit = target*(1+安全冗余值)}
// cpu: {request = target；limit = target*(1+安全冗余值)}
func calculateRecommendation(target map[string]interface{}, safetyRedundancy float64) (requests, limits map[string]interface{}, err error) {
	cpu := utils.GetString(target, "cpu")
	memory := utils.GetString(target, "memory")

	requests = make(map[string]interface{})
	limits = make(map[string]interface{})
	//单位转换
	cpuVal, memVal, err := utils.ConvertUnit(cpu, memory)
	if err != nil {
		fmt.Printf("单位转换失败：%v\n", err)
		return nil, nil, err
	}
	//计算推荐值
	// 即使没有明确的操作类型，也设置默认值
	requests["cpu"] = fmt.Sprintf("%dm", cpuVal)
	limits["cpu"] = fmt.Sprintf("%dm", int64(math.Ceil(float64(cpuVal)*(1+safetyRedundancy))))

	result := int64(math.Ceil(float64(memVal) * (1 + safetyRedundancy)))
	requests["memory"] = fmt.Sprintf("%dMi", result)
	limits["memory"] = fmt.Sprintf("%dMi", result)

	return requests, limits, nil
}

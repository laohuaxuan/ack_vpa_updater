package ack

import (
	"context"
	"fmt"

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
	Namespace    string `json:"namespace"`
	ResourceName string `json:"deploy_name"`
	//ClusterName string `json:"cluster_name"`
	// Resources   map[string]string         `json:"resources"`
	Containers []ContainerRecommendation `json:"containers"`
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
type ConditionRecommendation struct {
	LowConfidence          string `json:"low_confidence"`          // 低置信度
	RecommendationProvided string `json:"recommendation_provided"` // 已提供推荐
	NoPodsMatched          string `json:"no_pods_matched"`         // 未匹配到Pod
}

// 容器推荐值
// ContainerName: 容器名称
// Target: request资源推荐值
type ContainerRecommendation struct {
	Kind          string            `json:"kind"`           // 资源类型
	ContainerName string            `json:"container_name"` // 容器名称
	Request       map[string]string `json:"request"`        // request资源推荐值
	Limit         map[string]string `json:"limit"`          // limit资源推荐值
	OriginRequest map[string]string `json:"origin_request"` // 原始request资源值
	OriginLimit   map[string]string `json:"origin_limit"`   // 原始limit资源值
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
		//获取deployment name（不一定是deploy，也有可能是statefulet）
		resourceName, found, err := unstructured.NestedString(rec.Object, "spec", "workloadRef", "name")
		if err != nil {
			fmt.Printf("解析resourceName失败：%v\n", err)
			continue
		}
		if !found {
			fmt.Println("未找到resourceName字段")
			continue
		}
		//判断是否需要资源变更
		labels := getLabels(&rec)
		if labels.CPUOperation == "" || labels.MemoryOperation == "" {
			fmt.Printf("resource资源 %s/%s 未找到cpu_operation或memory_operation标签，跳过\n", resourceName, namespace)
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
			//fmt.Printf("deployment资源 %s/%s 不存在，跳过\n", deployName, namespace)
			continue
		} else if conditionsRecommendation.LowConfidence == conditionsRecommendation.RecommendationProvided {
			//fmt.Printf("deployment资源 %s/%s 低置信，跳过\n", deployName, namespace)
			continue
		} else if conditionsRecommendation.LowConfidence == "True" && conditionsRecommendation.RecommendationProvided == "False" {
			//fmt.Printf("deployment资源 %s/%s 高置信，但跳过推荐，有异常\n", deployName, namespace)
			continue
		}
		//####################################### 获取原始resource #########################################
		//需要recommendation的workloadRef的kind（Deployment/StatefulSet）和name
		kind, found, err := unstructured.NestedString(rec.Object, "spec", "workloadRef", "kind")
		if err != nil {
			fmt.Printf("解析kind失败：%v\n", err)
			continue
		}
		if !found {
			fmt.Println("未找到kind字段")
			continue
		}
		fmt.Printf("资源类型: %s, 名称: %s, 命名空间: %s\n", kind, resourceName, namespace)
		resources, err := getResources(dynamicClient, kind, resourceName, namespace)
		if err != nil {
			fmt.Printf("获取原始资源失败：%v\n", err)
			continue
		}
		//#######################################################################################################
		//解析recommendation
		rec, err := parseContainerRecommendation(&rec, resources, namespace, resourceName, kind, safetyRedundancy)
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
func parseContainerRecommendation(rec *unstructured.Unstructured, resources map[string]string, namespace string, resourceName string, kind string, safetyRedundancy float64) (Recommendation, error) {
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
			// 将 resources map[string]string 转换为 map[string]interface{}
			resourcesMap := make(map[string]interface{})
			originRequestMap := make(map[string]string)
			originLimitMap := make(map[string]string)
			for k, v := range resources {
				resourcesMap[k] = v
				switch k {
				case "cpu_request", "memory_request":
					originRequestMap[k] = v
				case "cpu_limit", "memory_limit":
					originLimitMap[k] = v
				}
			}

			requests, limits, err := calculateRecommendation(resourcesMap, target, safetyRedundancy)
			if err != nil {
				continue
			}

			// 构建请求和限制映射
			requestMap := make(map[string]string)
			limitMap := make(map[string]string)
			// 处理CPU
			if val, ok := requests["cpu"]; ok {
				requestMap["cpu"] = val.(string)
			}
			if val, ok := limits["cpu"]; ok {
				limitMap["cpu"] = val.(string)
			}

			// 处理内存
			if val, ok := requests["memory"]; ok {
				requestMap["memory"] = val.(string)
			}
			if val, ok := limits["memory"]; ok {
				limitMap["memory"] = val.(string)
			}
			// 只有当请求和限制都有值时才返回
			if len(requestMap) > 0 && len(limitMap) > 0 {
				return Recommendation{
					Namespace:    namespace,
					ResourceName: resourceName,
					Containers: []ContainerRecommendation{
						{
							Kind:          kind,
							ContainerName: containerName,
							Request:       requestMap,
							Limit:         limitMap,
							OriginRequest: originRequestMap,
							OriginLimit:   originLimitMap,
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
func calculateRecommendation(resources map[string]interface{}, target map[string]interface{}, safetyRedundancy float64) (requests, limits map[string]interface{}, err error) {
	var cpuTargetVal, cpuLimitVal, memTargetVal, memLimitVal float64
	//获取target
	cpuImgVal, memImgVal, err := utils.ConvertUnit(utils.GetString(target, "cpu"), utils.GetString(target, "memory"))
	if err != nil {
		fmt.Printf("单位转换失败：%v\n", err)
		return nil, nil, err
	}
	//获取当前资源
	currentCPURequestVal, currentMEMRequestVal, err := utils.ConvertUnit(utils.GetString(resources, "cpu_request"), utils.GetString(resources, "memory_request"))
	if err != nil {
		fmt.Printf("单位转换失败：%v\n", err)
		return nil, nil, err
	}
	currentCPULimitVal, currentMEMLimitVal, err := utils.ConvertUnit(utils.GetString(resources, "cpu_limit"), utils.GetString(resources, "memory_limit"))
	if err != nil {
		fmt.Printf("单位转换失败：%v\n", err)
		return nil, nil, err
	}
	//返回推荐值
	requests = make(map[string]interface{})
	limits = make(map[string]interface{})

	//计算新的推荐值和limit值
	cpuTargetVal, cpuLimitVal = utils.GetTargetAndLimitResources(float64(cpuImgVal), safetyRedundancy, float64(currentCPULimitVal), float64(currentCPURequestVal))
	memTargetVal, memLimitVal = utils.GetTargetAndLimitResources(float64(memImgVal), safetyRedundancy, float64(currentMEMLimitVal), float64(currentMEMRequestVal))

	requests["cpu"] = fmt.Sprintf("%dm", int64(cpuTargetVal))
	limits["cpu"] = fmt.Sprintf("%dm", int64(cpuLimitVal))
	requests["memory"] = fmt.Sprintf("%dMi", int64(memTargetVal))
	limits["memory"] = fmt.Sprintf("%dMi", int64(memLimitVal))

	return requests, limits, nil
}

// 获取当前的原始resource
func getResources(dynamicClient *dynamic.DynamicClient, kind string, name string, namespace string) (map[string]string, error) {
	var gvr schema.GroupVersionResource
	var cpu, memory string
	switch kind {
	case "Deployment":
		gvr = schema.GroupVersionResource{
			Group:    "apps",
			Version:  "v1",
			Resource: "deployments",
		}
	case "StatefulSet":
		gvr = schema.GroupVersionResource{
			Group:    "apps",
			Version:  "v1",
			Resource: "statefulsets",
		}
	default:
		return nil, fmt.Errorf("不支持的资源类型: %s", kind)
	}

	resourceObj, err := dynamicClient.Resource(gvr).Namespace(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("获取 %s %s/%s 失败: %v", kind, namespace, name, err)
	}

	containers, found, err := unstructured.NestedSlice(resourceObj.Object, "spec", "template", "spec", "containers")
	if err != nil || !found {
		return nil, fmt.Errorf("未找到容器列表或格式错误: %v", err)
	}

	resources := make(map[string]string)
	for _, c := range containers {
		containerMap, ok := c.(map[string]interface{})
		if !ok {
			continue
		}

		// containerName := utils.GetString(containerMap, "name")

		containerResources, found, err := unstructured.NestedMap(containerMap, "resources")
		if err != nil || !found {
			continue
		}

		if requests, found, _ := unstructured.NestedMap(containerResources, "requests"); found {
			cpu = utils.GetString(requests, "cpu")
			memory = utils.GetString(requests, "memory")
			cpuVal, memVal, err := utils.ConvertUnit(cpu, memory)
			if err != nil {
				fmt.Printf("单位转换失败：%v\n", err)
				continue
			}
			resources["cpu_request"] = fmt.Sprintf("%dm", cpuVal)
			resources["memory_request"] = fmt.Sprintf("%dMi", memVal)
		}

		if limits, found, _ := unstructured.NestedMap(containerResources, "limits"); found {
			cpu = utils.GetString(limits, "cpu")
			memory = utils.GetString(limits, "memory")
			cpuVal, memVal, err := utils.ConvertUnit(cpu, memory)
			if err != nil {
				fmt.Printf("单位转换失败：%v\n", err)
				continue
			}
			resources["cpu_limit"] = fmt.Sprintf("%dm", cpuVal)
			resources["memory_limit"] = fmt.Sprintf("%dMi", memVal)
		}
	}
	return resources, nil
}

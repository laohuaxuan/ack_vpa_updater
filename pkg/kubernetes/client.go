package kubernetes

import (
	"ack_vpa_updater/utils"
	"context"
	"fmt"
	"path/filepath"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
)

// 处理k8s客户端初始化和相关操作

// 初始化动态客户端（用于处理自定义资源CRD）
func InitClient(kubeconfig string) (*dynamic.DynamicClient, error) {
	if kubeconfig == "" || kubeconfig == "~" {
		kubeconfig = filepath.Join("~", ".kube", "config")
	}
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, err
	}
	//创建动态客户端（因为Recommendation是自定义资源，所以需要使用动态客户端）
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	return dynamicClient, nil
}

// 静态客户端，无法处理自定义资源（CRD）
// func InitClient(kubeconfig string) (*kubernetes.Clientset, error) {
// 	if kubeconfig == "" || kubeconfig == "~" {
// 		kubeconfig = filepath.Join("~", ".kube", "config")
// 	}

// 	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
// 	if err != nil {
// 		return nil, err
// 	}

// 	clientset, err := kubernetes.NewForConfig(config)
// 	if err != nil {
// 		return nil, err
// 	}

// 	return clientset, nil
// }

// 获取所有命名空间
func GetNamespace(dynamicClient *dynamic.DynamicClient) ([]string, error) {
	//ctx := context.Background()
	// Namespace 属于核心 API 组 (group 为空), v1 版本
	namespaceGVR := schema.GroupVersionResource{
		Group:    "",
		Version:  "v1",
		Resource: "namespaces",
	}
	//获取所有命名空间
	nsList, err := dynamicClient.Resource(namespaceGVR).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	namespaces := make([]string, 0, len(nsList.Items))
	for _, item := range nsList.Items {
		namespaces = append(namespaces, item.GetName())
	}
	return namespaces, nil
}

// func GetNamespaces(clientset *kubernetes.Clientset) ([]string, error) {
// 	ctx := context.Background()
// 	nsList, err := clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
// 	if err != nil {
// 		return nil, err
// 	}

// 	namespaces := make([]string, 0, len(nsList.Items))
// 	for _, ns := range nsList.Items {
// 		namespaces = append(namespaces, ns.Name)
// 	}

// 	return namespaces, nil
// }

// 检查命名空间下所有Pod是否准备就绪
func CheckPodsReady(dynamicClient *dynamic.DynamicClient, namespace string) (ready, total int, err error) {
	//定义pod的GVR（GroupVersionResource），用于获取所有pod
	podGVR := schema.GroupVersionResource{
		Group:    "",
		Version:  "v1",
		Resource: "pods",
	}
	podList, err := dynamicClient.Resource(podGVR).Namespace(namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return 0, 0, err
	}
	if len(podList.Items) == 0 {
		fmt.Printf("manespace: %s 没有Pod\n", namespace)
		return 0, 0, nil
	}

	total = len(podList.Items)
	for _, pod := range podList.Items {
		if checkPodReadiness(pod) {
			ready++
			break
		}
	}
	return ready, total, nil
}

// 检查单个pod的就绪情况
func checkPodReadiness(pod unstructured.Unstructured) bool {
	// name := pod.GetName()
	//获取pod的phase描述信息:running、restart等
	//phase, _, _ := unstructured.NestedString(pod.Object, "status", "phase")
	//检查 status.conditions 中 type="Ready" 的 status 是否为 "True"
	//ready, found, err := unstructured.NestedString(pod.Object, "status", "conditions", "Ready")
	isReady := false
	conditions, found, _ := unstructured.NestedSlice(pod.Object, "status", "conditions")
	if found {
		for _, item := range conditions {
			if condMap, ok := item.(map[string]interface{}); ok {
				if utils.GetString(condMap, "type") == "Ready" {
					isReady = utils.GetString(condMap, "status") == "True"
				}
				break
			}
		}
	}
	return isReady
}

// func CheckPodsReady(clientset *kubernetes.Clientset, namespace string) (ready, total int, err error) {
// 	ctx := context.Background()
// 	pods, err := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
// 	if err != nil {
// 		return 0, 0, err
// 	}

// 	total = len(pods.Items)
// 	for _, pod := range pods.Items {
// 		for _, cond := range pod.Status.Conditions {
// 			if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
// 				ready++
// 				break
// 			}
// 		}
// 	}

// 	return ready, total, nil
// }

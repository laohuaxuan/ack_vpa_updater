package utils

import (
	"fmt"

	"k8s.io/apimachinery/pkg/api/resource"
)

// 安全获取字符串
func GetString(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// 单位转换
func ConvertUnit(cpuStr, memStr string) (cpu int64, mem int64, err error) {
	if cpuStr == "" || memStr == "" {
		fmt.Println("cpuStr or memStr is empty")
		return 0, 0, nil
	}
	//---- 转换CPU ----
	cpuVal, err := resource.ParseQuantity(cpuStr)
	if err != nil {
		fmt.Println("CPU解析失败: ", err)
		return 0, 0, err
	}
	//毫核
	cpu = cpuVal.MilliValue()
	//---- 转换内存 ----
	memVal, err := resource.ParseQuantity(memStr)
	if err != nil {
		fmt.Println("内存解析失败: ", err)
		return 0, 0, err
	}
	//MB
	mem = memVal.Value() / (1024 * 1024)
	return cpu, mem, nil
}

// 计算resource
func GetTargetAndLimitResources(imgVal, safetyRedundancy, currentLimitVal, currentRequestVal float64) (targetVal, limitVal float64) {
	//计算新的CPU推荐值和limit值
	targetVal = float64(imgVal) * (1 + safetyRedundancy)
	if targetVal > 0 && imgVal > 0 {
		// limitVal = targetVal * float64(currentLimitVal/currentRequestVal) #容易失真
		limitVal = targetVal * float64(currentLimitVal/imgVal) //根据画像值计算，贴合实际
		//Limit值不能超过3倍推荐值
		if limitVal > targetVal*3 {
			limitVal = targetVal * 3
		}
	}
	return targetVal, limitVal
}

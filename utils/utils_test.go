package utils

import (
	"testing"
)

func TestGetTargetAndLimitResources(t *testing.T) {
	testCases := []struct {
		name              string
		imgVal            float64
		safetyRedundancy  float64
		currentLimitVal   float64
		currentRequestVal float64
		wantLimitGEtTarget bool
	}{
		{
			name:              "告警场景：画像值大于当前limit",
			imgVal:            25,
			safetyRedundancy:  0.3,
			currentLimitVal:   20,
			currentRequestVal: 20,
			wantLimitGEtTarget: true,
		},
		{
			name:              "Advisor示例场景",
			imgVal:            500,
			safetyRedundancy:  0.1,
			currentLimitVal:   300,
			currentRequestVal: 100,
			wantLimitGEtTarget: true,
		},
		{
			name:              "正常场景：画像值小于当前limit",
			imgVal:            100,
			safetyRedundancy:  0.2,
			currentLimitVal:   200,
			currentRequestVal: 50,
			wantLimitGEtTarget: true,
		},
		{
			name:              "边界场景：画像值等于当前limit",
			imgVal:            50,
			safetyRedundancy:  0.3,
			currentLimitVal:   50,
			currentRequestVal: 50,
			wantLimitGEtTarget: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			targetVal, limitVal := GetTargetAndLimitResources(tc.imgVal, tc.safetyRedundancy, tc.currentLimitVal, tc.currentRequestVal)
			t.Logf("imgVal=%.1f, safety=%.1f, currLimit=%.1f, currReq=%.1f", tc.imgVal, tc.safetyRedundancy, tc.currentLimitVal, tc.currentRequestVal)
			t.Logf("target(request)=%.1f, limit=%.1f", targetVal, limitVal)
			
			if limitVal < targetVal {
				t.Errorf("limit(%.1f) < target(%.1f), 违反Kubernetes约束", limitVal, targetVal)
			}
		})
	}
}
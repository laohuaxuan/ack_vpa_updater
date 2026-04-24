package notification

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"ack_vpa_updater/pkg/config"
	"ack_vpa_updater/pkg/update"
)

// 处理飞书通知相关的操作
func SendFeishuNotification(config config.FeishuConfig, result *update.UpdateResult) error {
	if config.WebhookURL == "" {
		return nil
	}

	successRate := 0.0
	if result.TotalCount > 0 {
		successRate = float64(result.SuccessCount) / float64(result.TotalCount) * 100
	}

	message := fmt.Sprintf("**ACK VPA Updater 更新报告**\n\n"+
		"时间: %s\n"+
		"总数: %d\n"+
		"成功: %d\n"+
		"失败: %d\n"+
		"成功率: %.1f%%",
		result.EndTime, result.TotalCount, result.SuccessCount, result.FailureCount, successRate)

	payload := map[string]interface{}{
		"msg_type": "text",
		"content": map[string]string{
			"text": message,
		},
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", config.WebhookURL, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")

	// if config.Secret != "" {
	// 	timestamp := time.Now().UnixMilli()
	// 	sign := generateSign(config.Secret, timestamp)
	// 	req.Header.Set("X-Feishu-Signature", sign)
	// 	req.Header.Set("X-Feishu-Timestamp", fmt.Sprintf("%d", timestamp))
	// }

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("飞书响应状态码: %d, 响应体: %s", resp.StatusCode, string(body))
	}

	return nil
}

// func generateSign(secret string, timestamp int64) string {
// 	stringToSign := fmt.Sprintf("%d\n%s", timestamp, secret)
// 	h := hmac.New(sha256.New, []byte(secret))
// 	h.Write([]byte(stringToSign))
// 	return base64.StdEncoding.EncodeToString(h.Sum(nil))
// }

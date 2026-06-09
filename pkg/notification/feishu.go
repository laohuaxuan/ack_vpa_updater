package notification

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"ack_vpa_updater/pkg/config"
	"ack_vpa_updater/pkg/errors"
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
	} else {
		return fmt.Errorf(errors.ErrNoRecords)
	}

	// 构建报告消息
	var messageBuffer bytes.Buffer
	messageBuffer.WriteString("**ACK VPA Updater 更新报告**\n\n")
	messageBuffer.WriteString(fmt.Sprintf("时间: %s\n", result.EndTime))
	messageBuffer.WriteString(fmt.Sprintf("总数: %d | 成功: %d | 失败: %d | 成功率: %.1f%%\n",
		result.TotalCount, result.SuccessCount, result.FailureCount, successRate))
	messageBuffer.WriteString(fmt.Sprintf("集群: %s\n\n", result.Cluster))

	// 添加成功更新的 Deployment 详情
	successRecords := make([]update.UpdateRecord, 0)
	failureRecords := make([]update.UpdateRecord, 0)

	for _, record := range result.Records {
		if record.Status == "success" {
			successRecords = append(successRecords, record)
		} else {
			failureRecords = append(failureRecords, record)
		}
	}

	// 显示成功更新的 Deployment
	if len(successRecords) > 0 {
		messageBuffer.WriteString("---\n")
		messageBuffer.WriteString(fmt.Sprintf("**✅ 成功更新 (%d 个)**\n\n", len(successRecords)))

		for i, record := range successRecords {
			messageBuffer.WriteString(fmt.Sprintf("%d. **%s/%s**\n", i+1, record.Namespace, record.ResourceName))
			messageBuffer.WriteString(fmt.Sprintf("   资源类型: %s\n", record.Kind))

			if record.ContainerName != "" {
				messageBuffer.WriteString(fmt.Sprintf("   容器: %s\n", record.ContainerName))
			}
			//修改前资源
			messageBuffer.WriteString("   修改前资源:\n")
			if len(record.OrignRequest) > 0 {
				messageBuffer.WriteString("     requests: ")
				for k, v := range record.OrignRequest {
					messageBuffer.WriteString(fmt.Sprintf("%s=%s ", k, v))
				}
				messageBuffer.WriteString("\n")
			}
			if len(record.OrignLimit) > 0 {
				messageBuffer.WriteString("     limits: ")
				for k, v := range record.OrignLimit {
					messageBuffer.WriteString(fmt.Sprintf("%s=%s ", k, v))
				}
				messageBuffer.WriteString("\n")
			}
			//修改后资源
			messageBuffer.WriteString("   修改后资源:\n")
			if len(record.Request) > 0 {
				messageBuffer.WriteString("     requests: ")
				for k, v := range record.Request {
					messageBuffer.WriteString(fmt.Sprintf("%s=%s ", k, v))
				}
				messageBuffer.WriteString("\n")
			}
			if len(record.Limit) > 0 {
				messageBuffer.WriteString("     limits: ")
				for k, v := range record.Limit {
					messageBuffer.WriteString(fmt.Sprintf("%s=%s ", k, v))
				}
				messageBuffer.WriteString("\n")
			}
			messageBuffer.WriteString("\n")
		}
	}

	// 显示更新失败的 Deployment
	if len(failureRecords) > 0 {
		messageBuffer.WriteString("---\n")
		messageBuffer.WriteString(fmt.Sprintf("**❌ 更新失败 (%d 个)**\n\n", len(failureRecords)))

		for i, record := range failureRecords {
			messageBuffer.WriteString(fmt.Sprintf("%d. **%s/%s**\n", i+1, record.Namespace, record.ResourceName))
			if record.ContainerName != "" {
				messageBuffer.WriteString(fmt.Sprintf("   容器: %s\n", record.ContainerName))
			}
			messageBuffer.WriteString("   计划修改资源:\n")
			if len(record.Request) > 0 {
				messageBuffer.WriteString("     requests: ")
				for k, v := range record.Request {
					messageBuffer.WriteString(fmt.Sprintf("%s=%s ", k, v))
				}
				messageBuffer.WriteString("\n")
			}
			if len(record.Limit) > 0 {
				messageBuffer.WriteString("     limits: ")
				for k, v := range record.Limit {
					messageBuffer.WriteString(fmt.Sprintf("%s=%s ", k, v))
				}
				messageBuffer.WriteString("\n")
			}
			if record.Error != "" {
				messageBuffer.WriteString(fmt.Sprintf("   错误原因: %s\n", record.Error))
			}
			messageBuffer.WriteString("\n")
		}
	}

	message := messageBuffer.String()

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

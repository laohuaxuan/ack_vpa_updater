package persistence

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"ack_vpa_updater/pkg/config"
	"ack_vpa_updater/pkg/update"

	_ "github.com/go-sql-driver/mysql"
)

// 处理数据持久化（文件存储和MYSQL存储）
func SaveResults(cfg *config.Config, result *update.UpdateResult) error {
	if cfg.Persistence.Type == "mysql" {
		return SaveResultsToMySQL(cfg.Persistence.MySQL, result)
	} else {
		return SaveResultsToFile(cfg.Persistence, result)
	}
}

func SaveResultsToFile(persistence config.PersistenceConfig, result *update.UpdateResult) error {
	if err := os.MkdirAll(persistence.DataDir, 0755); err != nil {
		return err
	}

	updateLogPath := filepath.Join(persistence.DataDir, persistence.UpdateLogFile)
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}

	if err := os.WriteFile(updateLogPath, data, 0644); err != nil {
		return err
	}

	report := map[string]interface{}{
		"total_count":   result.TotalCount,
		"success_count": result.SuccessCount,
		"failure_count": result.FailureCount,
		"success_rate":  float64(result.SuccessCount) / float64(result.TotalCount) * 100,
		"start_time":    result.StartTime,
		"end_time":      result.EndTime,
	}

	reportPath := filepath.Join(persistence.DataDir, persistence.ReportFile)
	reportData, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}

	if err := os.WriteFile(reportPath, reportData, 0644); err != nil {
		return err
	}

	return nil
}

func SaveResultsToMySQL(mysqlConfig config.MySQLConfig, result *update.UpdateResult) error {
	// 构建DSN
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=%s&parseTime=True&loc=Local",
		mysqlConfig.User, mysqlConfig.Password, mysqlConfig.Host, mysqlConfig.Port, mysqlConfig.Database, mysqlConfig.Charset)

	// 连接数据库
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return err
	}
	defer db.Close()

	// 测试连接
	if err := db.Ping(); err != nil {
		return err
	}

	// 创建表
	if err := createTables(db); err != nil {
		return err
	}

	// 开始事务
	tx, err := db.Begin()
	if err != nil {
		return err
	}

	// 插入更新记录
	for _, record := range result.Records {
		_, err := tx.Exec(
			"INSERT INTO update_records (timestamp, namespace, deployment, container_name, request_cpu, limit_cpu, request_memory, limit_memory, status, error) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
			record.Timestamp, record.Namespace, record.Deployment, record.ContainerName, record.Request["cpu"], record.Limit["cpu"], record.Request["memory"], record.Limit["memory"], record.Status, record.Error,
		)
		if err != nil {
			tx.Rollback()
			return err
		}
	}

	// 插入报告
	successRate := 0.0
	if result.TotalCount > 0 {
		successRate = float64(result.SuccessCount) / float64(result.TotalCount) * 100
	}

	_, err = tx.Exec(
		"INSERT INTO update_reports (total_count, success_count, failure_count, success_rate, start_time, end_time) VALUES (?, ?, ?, ?, ?, ?)",
		result.TotalCount, result.SuccessCount, result.FailureCount, successRate, result.StartTime, result.EndTime,
	)
	if err != nil {
		tx.Rollback()
		return err
	}

	// 提交事务
	if err := tx.Commit(); err != nil {
		return err
	}

	return nil
}

func createTables(db *sql.DB) error {
	// 创建更新记录表
	_, err := db.Exec(`
	CREATE TABLE IF NOT EXISTS update_records (
		id INT AUTO_INCREMENT PRIMARY KEY,
		timestamp VARCHAR(50) NOT NULL,
		namespace VARCHAR(100) NOT NULL,
		deployment VARCHAR(100) NOT NULL,
		container_name VARCHAR(100) NOT NULL,
		request_cpu VARCHAR(50) NOT NULL,
		limit_cpu VARCHAR(50) NOT NULL,
		limit_memory VARCHAR(50) NOT NULL,
		request_memory VARCHAR(50) NOT NULL,
		status VARCHAR(20) NOT NULL,
		error TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
	`)
	if err != nil {
		return err
	}

	// 创建更新报告表
	_, err = db.Exec(`
	CREATE TABLE IF NOT EXISTS update_reports (
		id INT AUTO_INCREMENT PRIMARY KEY,
		total_count INT NOT NULL,
		success_count INT NOT NULL,
		failure_count INT NOT NULL,
		success_rate DOUBLE NOT NULL,
		start_time VARCHAR(50) NOT NULL,
		end_time VARCHAR(50) NOT NULL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
	`)
	if err != nil {
		return err
	}

	return nil
}

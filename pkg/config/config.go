package config

import (
	"os"
	"path/filepath"

	"sigs.k8s.io/yaml"
)

//处理配置文件的加载和解析

type Config struct {
	Kubeconfig   string            `json:"kubeconfig"`
	Filters      Filters           `json:"filters"`
	UpdatePolicy UpdatePolicy      `json:"update_policy"`
	Feishu       FeishuConfig      `json:"feishu"`
	Persistence  PersistenceConfig `json:"persistence"`
}

type Filters struct {
	Namespaces         []string `json:"namespaces"`
	ExcludeNamespaces  []string `json:"exclude_namespaces"`
	Deployments        []string `json:"deployments"`
	ExcludeDeployments []string `json:"exclude_deployments"`
}

type UpdatePolicy struct {
	BatchSize            int     `json:"batch_size"`
	SuccessRateThreshold float64 `json:"success_rate_threshold"`
	PodReadyTimeout      int     `json:"pod_ready_timeout"`
	CheckInterval        int     `json:"check_interval"`
	SafetyRedundancy     float64 `json:"safetyRedundancy"`
}

type FeishuConfig struct {
	Enabled    bool   `json:"enabled"`
	WebhookURL string `json:"webhook_url"`
	Secret     string `json:"secret"`
}

type PersistenceConfig struct {
	Enabled       bool        `json:"enabled"`
	Type          string      `json:"type"` // file or mysql
	DataDir       string      `json:"data_dir"`
	UpdateLogFile string      `json:"update_log_file"`
	ReportFile    string      `json:"report_file"`
	MySQL         MySQLConfig `json:"mysql"`
}

type MySQLConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	Database string `json:"database"`
	Charset  string `json:"charset"`
}

func LoadConfig(configPath string) (*Config, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	// 设置默认值
	if cfg.Kubeconfig == "" || cfg.Kubeconfig == "~" {
		cfg.Kubeconfig = filepath.Join(os.Getenv("HOME"), ".kube", "config")
	}

	if cfg.UpdatePolicy.BatchSize <= 0 {
		cfg.UpdatePolicy.BatchSize = 5
	}

	if cfg.UpdatePolicy.SuccessRateThreshold <= 0 {
		cfg.UpdatePolicy.SuccessRateThreshold = 0.5
	}

	if cfg.UpdatePolicy.PodReadyTimeout <= 0 {
		cfg.UpdatePolicy.PodReadyTimeout = 300
	}

	if cfg.UpdatePolicy.CheckInterval <= 0 {
		cfg.UpdatePolicy.CheckInterval = 10
	}

	if cfg.UpdatePolicy.SafetyRedundancy <= 0 || cfg.UpdatePolicy.SafetyRedundancy > 1 {
		cfg.UpdatePolicy.SafetyRedundancy = 0.3
	}

	if cfg.Persistence.Type == "" {
		cfg.Persistence.Type = "file"
	}

	if cfg.Persistence.DataDir == "" {
		cfg.Persistence.DataDir = "./data"
	}

	if cfg.Persistence.UpdateLogFile == "" {
		cfg.Persistence.UpdateLogFile = "update_log.json"
	}

	if cfg.Persistence.ReportFile == "" {
		cfg.Persistence.ReportFile = "report.json"
	}

	if cfg.Persistence.MySQL.Host == "" {
		cfg.Persistence.MySQL.Host = "localhost"
	}

	if cfg.Persistence.MySQL.Port <= 0 {
		cfg.Persistence.MySQL.Port = 3306
	}

	if cfg.Persistence.MySQL.User == "" {
		cfg.Persistence.MySQL.User = "root"
	}

	if cfg.Persistence.MySQL.Database == "" {
		cfg.Persistence.MySQL.Database = "ack_vpa_updater"
	}

	if cfg.Persistence.MySQL.Charset == "" {
		cfg.Persistence.MySQL.Charset = "utf8mb4"
	}

	return &cfg, nil
}

package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/fsnotify/fsnotify"
	"sigs.k8s.io/yaml"
)

// 全局配置实例
var (
	globalConfig *Config
	configLock   sync.RWMutex
)

//处理配置文件的加载和解析

type Config struct {
	Kubeconfig   string            `json:"kubeconfig"`
	InCluster    bool              `json:"in_cluster"`
	ApiServer    string            `json:"api_server"`
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
		cfg.UpdatePolicy.CheckInterval = 3600
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

// GetConfig 获取当前配置（线程安全）
func GetConfig() *Config {
	configLock.RLock()
	defer configLock.RUnlock()
	//fmt.Println("【step 1】已获取config.yaml配置文件...")
	return globalConfig

}

// SetConfig 设置配置（线程安全）
func SetConfig(cfg *Config) {
	configLock.Lock()
	globalConfig = cfg
	configLock.Unlock()
	//fmt.Println("【step 2】已设置config.yaml配置文件...")
}

// WatchConfig 监控配置文件变化并自动重新加载
func WatchConfig(configPath string, onReload func(*Config)) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("创建文件监控器失败: %v", err)
	}

	// 将相对路径转换为绝对路径
	absConfigPath, err := filepath.Abs(configPath)
	if err != nil {
		watcher.Close()
		return fmt.Errorf("获取配置文件绝对路径失败: %v", err)
	}

	// 获取配置文件所在目录
	configDir := filepath.Dir(absConfigPath)

	err = watcher.Add(configDir)
	if err != nil {
		watcher.Close()
		return fmt.Errorf("添加监控目录失败: %v", err)
	}

	fmt.Printf("开始监控配置文件: %s\n", absConfigPath)

	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				// 只处理配置文件的修改事件（使用绝对路径比较）
				if event.Name == absConfigPath && (event.Op&fsnotify.Write == fsnotify.Write || event.Op&fsnotify.Create == fsnotify.Create) {
					fmt.Printf("检测到配置文件变化: %s\n", event.Name)
					newConfig, err := LoadConfig(absConfigPath)
					if err != nil {
						fmt.Printf("重新加载配置失败: %v\n", err)
						continue
					}
					// 更新全局配置
					SetConfig(newConfig)
					fmt.Println("配置文件已重新加载")
					// 调用回调函数
					if onReload != nil {
						onReload(newConfig)
					}
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				fmt.Printf("配置监控错误: %v\n", err)
			}
		}
	}()

	return nil
}

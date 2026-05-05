package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	ClaudeDir    string          `yaml:"claude_dir"`
	ProjectsDir  string          `yaml:"projects_dir"`
	DatabasePath string          `yaml:"database_path"`
	ReportsDir   string          `yaml:"reports_dir"`
	Redact       bool            `yaml:"redact_by_default"`
	StoreContent bool            `yaml:"store_content"`
	CharsPerTok  int             `yaml:"token_estimation_chars_per_token"`
	Limits       LimitConfig     `yaml:"limit_detection"`
	Comparison   ComparisonCfg   `yaml:"comparison"`
	Server       ServerCfg       `yaml:"server"`
}

type LimitConfig struct {
	Enabled             bool    `yaml:"enabled"`
	ConfidenceThreshold float64 `yaml:"confidence_threshold"`
	EndedAfterLimitMins int     `yaml:"ended_after_limit_window_minutes"`
	PostLimitDropPct    float64 `yaml:"post_limit_drop_threshold"`
}

type ComparisonCfg struct {
	DefaultCurrentDays  int `yaml:"default_current_days"`
	DefaultBaselineDays int `yaml:"default_baseline_days"`
}

type ServerCfg struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

func Default() *Config {
	home, _ := os.UserHomeDir()
	return &Config{
		ClaudeDir:    filepath.Join(home, ".claude"),
		ProjectsDir:  filepath.Join(home, ".claude", "projects"),
		DatabasePath: filepath.Join(home, ".claude-usage-analyser", "usage.sqlite"),
		ReportsDir:   filepath.Join(home, ".claude-usage-analyser", "reports"),
		Redact:       true,
		StoreContent: false,
		CharsPerTok:  4,
		Limits: LimitConfig{
			Enabled:             true,
			ConfidenceThreshold: 0.5,
			EndedAfterLimitMins: 30,
			PostLimitDropPct:    0.2,
		},
		Comparison: ComparisonCfg{
			DefaultCurrentDays:  30,
			DefaultBaselineDays: 90,
		},
		Server: ServerCfg{
			Host: "127.0.0.1",
			Port: 1974,
		},
	}
}

func Load(path string) (*Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func Save(path string, cfg *Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func DefaultPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude-usage-analyser", "config.yml")
}

// Package config 负责加载与校验 config.yaml。
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// KeyConfig 一个调用方密钥。
type KeyConfig struct {
	Name  string `yaml:"name"`  // 调用方标识，写入用量日志
	Key   string `yaml:"key"`   // 密钥本身
	Admin bool   `yaml:"admin"` // 是否可访问管理接口（如 /api/v1/stats）
}

type ServerConfig struct {
	Addr           string `yaml:"addr"`
	TimeoutSeconds int    `yaml:"timeout_seconds"`
}

type AuthConfig struct {
	Keys []KeyConfig `yaml:"keys"`
}

type RateLimitConfig struct {
	PerKeyRPS float64 `yaml:"per_key_rps"`
	Burst     int     `yaml:"burst"`
}

type ScraperConfig struct {
	MaxConcurrency         int    `yaml:"max_concurrency"`
	QueueTimeoutSeconds    int    `yaml:"queue_timeout_seconds"`
	DefaultCacheTTLSeconds int    `yaml:"default_cache_ttl_seconds"`
	UserAgent              string `yaml:"user_agent"`
}

type StoreConfig struct {
	Path string `yaml:"path"`
}

type Config struct {
	Server    ServerConfig    `yaml:"server"`
	Auth      AuthConfig      `yaml:"auth"`
	RateLimit RateLimitConfig `yaml:"ratelimit"`
	Scraper   ScraperConfig   `yaml:"scraper"`
	Store     StoreConfig     `yaml:"store"`
}

// Load 读取并解析配置文件，填充默认值后做基础校验。
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取配置 %s: %w", path, err)
	}
	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("解析配置 %s: %w", path, err)
	}
	cfg.applyDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) applyDefaults() {
	if c.Server.Addr == "" {
		c.Server.Addr = "127.0.0.1:8300"
	}
	if c.Server.TimeoutSeconds <= 0 {
		c.Server.TimeoutSeconds = 20
	}
	if c.RateLimit.PerKeyRPS <= 0 {
		c.RateLimit.PerKeyRPS = 5
	}
	if c.RateLimit.Burst <= 0 {
		c.RateLimit.Burst = 10
	}
	if c.Scraper.MaxConcurrency <= 0 {
		c.Scraper.MaxConcurrency = 4
	}
	if c.Scraper.QueueTimeoutSeconds <= 0 {
		c.Scraper.QueueTimeoutSeconds = 10
	}
	if c.Scraper.DefaultCacheTTLSeconds <= 0 {
		c.Scraper.DefaultCacheTTLSeconds = 300
	}
	if c.Scraper.UserAgent == "" {
		c.Scraper.UserAgent = "api-server/0.1"
	}
	if c.Store.Path == "" {
		c.Store.Path = "./data/api-server.db"
	}
}

func (c *Config) validate() error {
	if len(c.Auth.Keys) == 0 {
		return fmt.Errorf("auth.keys 至少需要配置一个密钥")
	}
	seen := map[string]bool{}
	for i, k := range c.Auth.Keys {
		if k.Name == "" || k.Key == "" {
			return fmt.Errorf("auth.keys[%d] 的 name/key 不能为空", i)
		}
		if seen[k.Key] {
			return fmt.Errorf("auth.keys[%d] 的 key 重复", i)
		}
		seen[k.Key] = true
	}
	return nil
}

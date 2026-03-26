package main

import (
	"os"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Port         int           `yaml:"port"`
	PollInterval time.Duration `yaml:"poll_interval"`
	DataDir      string        `yaml:"data_dir"`

	Docker struct {
		Enabled bool   `yaml:"enabled"`
		Socket  string `yaml:"socket"`
	} `yaml:"docker"`

	Proxmox struct {
		Enabled       bool   `yaml:"enabled"`
		Host          string `yaml:"host"`
		TokenID       string `yaml:"token_id"`
		TokenSecret   string `yaml:"token_secret"`
		SkipTLSVerify bool   `yaml:"skip_tls_verify"`
	} `yaml:"proxmox"`
}

func DefaultConfig() Config {
	var cfg Config
	cfg.Port = 8080
	cfg.PollInterval = 30 * time.Second
	cfg.DataDir = "./data"
	cfg.Docker.Enabled = true
	cfg.Docker.Socket = "/var/run/docker.sock"
	return cfg
}

func LoadConfig(path string) (Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			applyEnv(&cfg)
			return cfg, nil
		}
		return cfg, err
	}

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}

	applyEnv(&cfg)
	return cfg, nil
}

func applyEnv(cfg *Config) {
	if v := os.Getenv("VITALSVG_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			cfg.Port = p
		}
	}
	if v := os.Getenv("VITALSVG_POLL_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.PollInterval = d
		}
	}
	if v := os.Getenv("VITALSVG_DATA_DIR"); v != "" {
		cfg.DataDir = v
	}
	if v := os.Getenv("VITALSVG_DOCKER_SOCKET"); v != "" {
		cfg.Docker.Socket = v
	}
	if v := os.Getenv("VITALSVG_DOCKER_ENABLED"); v != "" {
		cfg.Docker.Enabled = v == "true" || v == "1"
	}
	if v := os.Getenv("VITALSVG_PROXMOX_HOST"); v != "" {
		cfg.Proxmox.Host = v
		cfg.Proxmox.Enabled = true
	}
	if v := os.Getenv("VITALSVG_PROXMOX_TOKEN_ID"); v != "" {
		cfg.Proxmox.TokenID = v
	}
	if v := os.Getenv("VITALSVG_PROXMOX_TOKEN_SECRET"); v != "" {
		cfg.Proxmox.TokenSecret = v
	}
	if v := os.Getenv("VITALSVG_PROXMOX_SKIP_TLS"); v != "" {
		cfg.Proxmox.SkipTLSVerify = v == "true" || v == "1"
	}
}

package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

const (
	DefaultPort     = 8080
	DefaultSubPath  = "/sub"
	DefaultLogLevel = "info"
)

type Config struct {
	Server ServerConfig  `yaml:"server"`
	Panels []PanelConfig `yaml:"panels"`
	Log    LogConfig     `yaml:"log"`
}

type ServerConfig struct {
	Port int `yaml:"port"`
}

type PanelConfig struct {
	Name     string `yaml:"name"`
	Address  string `yaml:"address"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	SubPath  string `yaml:"sub_path"`
}

type LogConfig struct {
	Level string `yaml:"level"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	cfg.setDefaults()

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("validating config: %w", err)
	}

	return &cfg, nil
}

func (c *Config) setDefaults() {
	if c.Server.Port == 0 {
		c.Server.Port = DefaultPort
	}
	if c.Log.Level == "" {
		c.Log.Level = DefaultLogLevel
	}
	for i := range c.Panels {
		if c.Panels[i].SubPath == "" {
			c.Panels[i].SubPath = DefaultSubPath
		}
	}
}

func (c *Config) validate() error {
	if len(c.Panels) < 2 {
		return fmt.Errorf("at least 2 panels are required, got %d", len(c.Panels))
	}

	for i, p := range c.Panels {
		if p.Address == "" {
			return fmt.Errorf("panel %d: address is required", i)
		}
		if p.Username == "" {
			return fmt.Errorf("panel %d: username is required", i)
		}
		if p.Password == "" {
			return fmt.Errorf("panel %d: password is required", i)
		}
	}

	return nil
}

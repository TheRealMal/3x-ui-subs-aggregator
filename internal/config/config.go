package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	DefaultPort     = 8080
	DefaultBasePath = "/panel/"
	DefaultSubPath  = "/json/"
	DefaultLogLevel = "info"

	certBaseDir   = "/root/cert"
	certIPBaseDir = "/root/cert/ip"
)

// Common certificate file name pairs to search for (cert, key).
var certFilePatterns = []struct{ cert, key string }{
	{"fullchain.pem", "privkey.pem"},
	{"cert.pem", "key.pem"},
	{"fullchain.cer", "privkey.key"},
}

type Config struct {
	Server ServerConfig  `yaml:"server"`
	Panels []PanelConfig `yaml:"panels"`
	Log    LogConfig     `yaml:"log"`
}

type ServerConfig struct {
	Port        int       `yaml:"port"`
	AdminSecret string    `yaml:"admin_secret"`
	TLS         TLSConfig `yaml:"tls"`
}

type TLSConfig struct {
	Domain   string `yaml:"domain"`
	CertFile string `yaml:"cert_file"`
	KeyFile  string `yaml:"key_file"`
}

type PanelConfig struct {
	Name     string `yaml:"name"`
	Address  string `yaml:"address"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	BasePath string `yaml:"base_path"`
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
		c.Panels[i].Address = strings.TrimRight(c.Panels[i].Address, "/")
		if c.Panels[i].BasePath == "" {
			c.Panels[i].BasePath = DefaultBasePath
		}
		if c.Panels[i].SubPath == "" {
			c.Panels[i].SubPath = DefaultSubPath
		}
		c.Panels[i].BasePath = normalizePath(c.Panels[i].BasePath)
		c.Panels[i].SubPath = normalizePath(c.Panels[i].SubPath)
	}
}

func (c *Config) validate() error {
	if c.Server.AdminSecret == "" {
		return fmt.Errorf("server.admin_secret is required")
	}
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

	// Validate explicit TLS paths if provided
	tls := c.Server.TLS
	if tls.CertFile != "" || tls.KeyFile != "" {
		if tls.CertFile == "" || tls.KeyFile == "" {
			return fmt.Errorf("both tls.cert_file and tls.key_file must be set together")
		}
		if _, err := os.Stat(tls.CertFile); err != nil {
			return fmt.Errorf("tls.cert_file not found: %s", tls.CertFile)
		}
		if _, err := os.Stat(tls.KeyFile); err != nil {
			return fmt.Errorf("tls.key_file not found: %s", tls.KeyFile)
		}
	}

	return nil
}

// ResolveTLS attempts to find TLS certificate and key files.
// Priority: explicit config > domain-based auto-discovery > IP-based auto-discovery.
// Returns (certFile, keyFile, true) if found, or ("", "", false) if TLS should not be used.
func (c *Config) ResolveTLS(logger *slog.Logger) (certFile, keyFile string, ok bool) {
	tls := c.Server.TLS

	// 1. Explicit paths take highest priority (already validated).
	if tls.CertFile != "" && tls.KeyFile != "" {
		logger.Info("using explicit TLS certificates", "cert", tls.CertFile, "key", tls.KeyFile)
		return tls.CertFile, tls.KeyFile, true
	}

	// 2. Auto-discover by domain: /root/cert/<domain>/
	if tls.Domain != "" {
		dir := filepath.Join(certBaseDir, tls.Domain)
		if cert, key, found := findCertPairInDir(dir); found {
			logger.Info("auto-discovered TLS certificates by domain", "domain", tls.Domain, "cert", cert, "key", key)
			return cert, key, true
		}
		logger.Debug("no TLS certificates found for domain", "domain", tls.Domain, "dir", dir)
	}

	// 3. Auto-discover by IP: /root/cert/ip/<ip>/
	if entries, err := os.ReadDir(certIPBaseDir); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			dir := filepath.Join(certIPBaseDir, entry.Name())
			if cert, key, found := findCertPairInDir(dir); found {
				logger.Info("auto-discovered TLS certificates by IP", "ip", entry.Name(), "cert", cert, "key", key)
				return cert, key, true
			}
		}
	}

	logger.Info("no TLS certificates found, server will use plain HTTP")
	return "", "", false
}

// findCertPairInDir checks a directory for known certificate+key file name patterns.
func findCertPairInDir(dir string) (certFile, keyFile string, found bool) {
	for _, p := range certFilePatterns {
		cert := filepath.Join(dir, p.cert)
		key := filepath.Join(dir, p.key)
		if fileExists(cert) && fileExists(key) {
			return cert, key, true
		}
	}
	return "", "", false
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// normalizePath ensures a path starts with / and ends with /.
func normalizePath(p string) string {
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	if !strings.HasSuffix(p, "/") {
		p = p + "/"
	}
	return p
}

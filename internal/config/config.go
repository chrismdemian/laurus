// Package config handles loading and saving Laurus configuration.
package config

import (
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

// Config holds all user-configurable settings.
type Config struct {
	CanvasURL string            `toml:"canvas_url"`
	SyncDir   string            `toml:"sync_dir"`
	Theme     string            `toml:"theme"`
	Aliases   map[string]string `toml:"aliases,omitempty"`
}

// DefaultPath returns the platform-appropriate config file path.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "laurus", "config.toml"), nil
}

// Dir returns the directory containing the config file.
func Dir() (string, error) {
	p, err := DefaultPath()
	if err != nil {
		return "", err
	}
	return filepath.Dir(p), nil
}

// Load reads config from the default path.
func Load() (*Config, error) {
	p, err := DefaultPath()
	if err != nil {
		return nil, err
	}
	return LoadFrom(p)
}

// LoadFrom reads config from the given path.
// If the file does not exist, it creates a default config and writes it.
func LoadFrom(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		cfg := defaults()
		if saveErr := SaveTo(&cfg, path); saveErr != nil {
			return nil, saveErr
		}
		return &cfg, nil
	}
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	applyDefaults(&cfg)
	return &cfg, nil
}

// Save writes config to the default path.
func Save(cfg *Config) error {
	p, err := DefaultPath()
	if err != nil {
		return err
	}
	return SaveTo(cfg, p)
}

// SaveTo writes config to the given path, creating directories as needed.
func SaveTo(cfg *Config, path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	data, err := toml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func defaults() Config {
	return Config{
		Theme:   "auto",
		SyncDir: "~/School",
	}
}

func applyDefaults(cfg *Config) {
	if cfg.Theme == "" {
		cfg.Theme = "auto"
	}
	if cfg.SyncDir == "" {
		cfg.SyncDir = "~/School"
	}
}

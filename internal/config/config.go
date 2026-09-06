// Package config handles loading and saving Laurus configuration.
package config

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// EnvCanvasURL overrides canvas_url from config.toml when set, so a headless
// setup needs no config file at all (pair it with CANVAS_TOKEN).
const EnvCanvasURL = "CANVAS_URL"

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
// CANVAS_URL in the environment overrides canvas_url from the file; note that
// a later Save persists whatever value is loaded, including the override.
func LoadFrom(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		cfg := defaults()
		if saveErr := SaveTo(&cfg, path); saveErr != nil {
			return nil, saveErr
		}
		applyEnv(&cfg)
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
	applyEnv(&cfg)
	return &cfg, nil
}

func applyEnv(cfg *Config) {
	if v := os.Getenv(EnvCanvasURL); v != "" {
		cfg.CanvasURL = strings.TrimRight(strings.TrimSpace(v), "/")
	}
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

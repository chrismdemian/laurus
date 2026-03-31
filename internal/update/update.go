// Package update provides self-update functionality for the Laurus binary.
package update

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/creativeprojects/go-selfupdate"

	"github.com/chrismdemian/laurus/internal/config"
)

const (
	repo          = "chrismdemian/laurus"
	cacheDuration = 24 * time.Hour
	cacheFile     = "update-check.json"
)

// CachedCheck stores the result of a version check to avoid hitting GitHub on every startup.
type CachedCheck struct {
	LatestVersion  string    `json:"latest_version"`
	CurrentVersion string    `json:"current_version"`
	CheckedAt      time.Time `json:"checked_at"`
}

// CheckResult holds the result of a live version check.
type CheckResult struct {
	CurrentVersion string
	LatestVersion  string
	HasUpdate      bool
	Release        *selfupdate.Release
}

// CheckLatest queries GitHub Releases for the latest version.
func CheckLatest(ctx context.Context, currentVersion string) (*CheckResult, error) {
	latest, found, err := selfupdate.DetectLatest(ctx, selfupdate.ParseSlug(repo))
	if err != nil {
		return nil, fmt.Errorf("detecting latest version: %w", err)
	}
	if !found {
		return nil, fmt.Errorf("no release found for %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	result := &CheckResult{
		CurrentVersion: currentVersion,
		LatestVersion:  latest.Version(),
		HasUpdate:      latest.GreaterThan(currentVersion),
		Release:        latest,
	}
	return result, nil
}

// Apply downloads and replaces the current binary with the release.
func Apply(ctx context.Context, release *selfupdate.Release) error {
	exe, err := selfupdate.ExecutablePath()
	if err != nil {
		return fmt.Errorf("locating executable: %w", err)
	}
	if err := selfupdate.UpdateTo(ctx, release.AssetURL, release.AssetName, exe); err != nil {
		return fmt.Errorf("applying update: %w", err)
	}
	return nil
}

// SaveCachedCheck writes the check result to the cache file.
func SaveCachedCheck(latestVersion, currentVersion string) error {
	dir, err := config.Dir()
	if err != nil {
		return err
	}
	c := CachedCheck{
		LatestVersion:  latestVersion,
		CurrentVersion: currentVersion,
		CheckedAt:      time.Now(),
	}
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, cacheFile), data, 0644)
}

// LoadCachedCheck reads the cached check result. Returns nil if no cache or if stale.
func LoadCachedCheck() *CachedCheck {
	dir, err := config.Dir()
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(dir, cacheFile))
	if err != nil {
		return nil
	}
	var c CachedCheck
	if err := json.Unmarshal(data, &c); err != nil {
		return nil
	}
	return &c
}

// IsCacheStale returns true if the cache is older than 24 hours or doesn't exist.
func IsCacheStale(c *CachedCheck) bool {
	if c == nil {
		return true
	}
	return time.Since(c.CheckedAt) > cacheDuration
}

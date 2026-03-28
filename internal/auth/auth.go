// Package auth handles Canvas API token management and secure storage.
package auth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/99designs/keyring"

	"github.com/chrismdemian/laurus/internal/config"
)

// TokenData holds an API token along with its expiry metadata.
type TokenData struct {
	Token     string    `json:"token"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// DaysRemaining returns the number of days until the token expires.
// Returns -1 if the expiry time is unknown (zero value).
func DaysRemaining(td *TokenData) int {
	if td.ExpiresAt.IsZero() {
		return -1
	}
	days := int(time.Until(td.ExpiresAt).Hours() / 24)
	if days < 0 {
		return 0
	}
	return days
}

// IsExpiringSoon returns true if the token expires within 14 days.
func IsExpiringSoon(td *TokenData) bool {
	d := DaysRemaining(td)
	return d >= 0 && d < 14
}

// Store saves a token to the OS keychain (or file fallback).
func Store(canvasURL, token string, expiresAt time.Time) error {
	return storeWithDir(canvasURL, token, expiresAt, "")
}

func storeWithDir(canvasURL, token string, expiresAt time.Time, dir string) error {
	td := TokenData{
		Token:     token,
		CreatedAt: time.Now().UTC(),
		ExpiresAt: expiresAt.UTC(),
	}

	data, err := json.Marshal(td)
	if err != nil {
		return fmt.Errorf("marshaling token: %w", err)
	}

	kr, err := openKeyring(dir)
	if err != nil {
		return storeToFile(canvasURL, data, dir)
	}

	err = kr.Set(keyring.Item{
		Key:         canvasURL,
		Data:        data,
		Label:       "Laurus Canvas Token",
		Description: "Canvas LMS API token for " + canvasURL,
	})
	if err != nil {
		return storeToFile(canvasURL, data, dir)
	}
	return nil
}

// Load retrieves a token for the given Canvas URL.
// Checks CANVAS_TOKEN env var first, then the keychain, then the fallback file.
func Load(canvasURL string) (*TokenData, error) {
	return loadWithDir(canvasURL, "")
}

func loadWithDir(canvasURL, dir string) (*TokenData, error) {
	// Environment variable takes precedence (CI/headless)
	if envToken := os.Getenv("CANVAS_TOKEN"); envToken != "" {
		return &TokenData{Token: envToken}, nil
	}

	kr, err := openKeyring(dir)
	if err == nil {
		item, err := kr.Get(canvasURL)
		if err == nil {
			var td TokenData
			if err := json.Unmarshal(item.Data, &td); err != nil {
				return nil, fmt.Errorf("corrupted token data: %w", err)
			}
			return &td, nil
		}
	}

	// Try fallback file
	return loadFromFile(canvasURL, dir)
}

// Delete removes a token for the given Canvas URL from all storage backends.
func Delete(canvasURL string) error {
	return deleteWithDir(canvasURL, "")
}

func deleteWithDir(canvasURL, dir string) error {
	kr, err := openKeyring(dir)
	if err == nil {
		_ = kr.Remove(canvasURL)
	}
	_ = deleteFromFile(canvasURL, dir)
	return nil
}

func openKeyring(dir string) (keyring.Keyring, error) {
	if dir == "" {
		var err error
		dir, err = config.Dir()
		if err != nil {
			return nil, err
		}
	}

	backends := []keyring.BackendType{keyring.FileBackend}
	switch runtime.GOOS {
	case "darwin":
		backends = []keyring.BackendType{keyring.KeychainBackend, keyring.FileBackend}
	case "windows":
		backends = []keyring.BackendType{keyring.WinCredBackend, keyring.FileBackend}
	case "linux":
		backends = []keyring.BackendType{keyring.SecretServiceBackend, keyring.FileBackend}
	}

	return keyring.Open(keyring.Config{
		ServiceName:              "laurus",
		AllowedBackends:          backends,
		FileDir:                  dir,
		FilePasswordFunc:         fixedPassword,
		KeychainTrustApplication: true,
		WinCredPrefix:            "laurus_",
	})
}

// fixedPassword provides a deterministic passphrase for the file backend.
// The file itself has 0600 permissions; this adds a layer of obfuscation.
func fixedPassword(_ string) (string, error) {
	return "laurus-file-backend-key", nil
}

// credentialsPath returns the path to the fallback credentials file.
func credentialsPath(dir string) (string, error) {
	if dir == "" {
		var err error
		dir, err = config.Dir()
		if err != nil {
			return "", err
		}
	}
	return filepath.Join(dir, "credentials"), nil
}

func storeToFile(canvasURL string, data []byte, dir string) error {
	path, err := credentialsPath(dir)
	if err != nil {
		return err
	}

	store, err := readCredentialsFile(path)
	if err != nil {
		store = make(map[string]json.RawMessage)
	}
	store[canvasURL] = data

	return writeCredentialsFile(path, store)
}

func loadFromFile(canvasURL, dir string) (*TokenData, error) {
	path, err := credentialsPath(dir)
	if err != nil {
		return nil, err
	}

	store, err := readCredentialsFile(path)
	if err != nil {
		return nil, fmt.Errorf("no token found for %s", canvasURL)
	}

	raw, ok := store[canvasURL]
	if !ok {
		return nil, fmt.Errorf("no token found for %s", canvasURL)
	}

	var td TokenData
	if err := json.Unmarshal(raw, &td); err != nil {
		return nil, fmt.Errorf("corrupted token data: %w", err)
	}
	return &td, nil
}

func deleteFromFile(canvasURL, dir string) error {
	path, err := credentialsPath(dir)
	if err != nil {
		return err
	}

	store, err := readCredentialsFile(path)
	if err != nil {
		return nil
	}

	delete(store, canvasURL)
	return writeCredentialsFile(path, store)
}

func readCredentialsFile(path string) (map[string]json.RawMessage, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var store map[string]json.RawMessage
	if err := json.Unmarshal(data, &store); err != nil {
		return nil, err
	}
	return store, nil
}

func writeCredentialsFile(path string, store map[string]json.RawMessage) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

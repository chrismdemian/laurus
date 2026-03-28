package auth

import (
	"testing"
	"time"
)

func TestDaysRemaining(t *testing.T) {
	tests := []struct {
		name      string
		expiresAt time.Time
		want      int
	}{
		{"30 days out", time.Now().Add(30 * 24 * time.Hour), 30},
		{"1 hour out", time.Now().Add(1 * time.Hour), 0},
		{"zero time", time.Time{}, -1},
		{"already expired", time.Now().Add(-24 * time.Hour), 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			td := &TokenData{ExpiresAt: tt.expiresAt}
			got := DaysRemaining(td)
			if got != tt.want {
				t.Errorf("DaysRemaining() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestIsExpiringSoon(t *testing.T) {
	tests := []struct {
		name      string
		expiresAt time.Time
		want      bool
	}{
		{"13 days", time.Now().Add(13 * 24 * time.Hour), true},
		{"14 days", time.Now().Add(15 * 24 * time.Hour), false},
		{"0 days", time.Now().Add(1 * time.Hour), true},
		{"unknown", time.Time{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			td := &TokenData{ExpiresAt: tt.expiresAt}
			got := IsExpiringSoon(td)
			if got != tt.want {
				t.Errorf("IsExpiringSoon() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStoreAndLoad_FileBackend(t *testing.T) {
	dir := t.TempDir()
	url := "https://canvas.example.com"
	token := "test-token-12345"
	expires := time.Now().Add(90 * 24 * time.Hour).UTC()

	err := storeWithDir(url, token, expires, dir)
	if err != nil {
		t.Fatalf("Store() error: %v", err)
	}

	td, err := loadWithDir(url, dir)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if td.Token != token {
		t.Errorf("Token = %q, want %q", td.Token, token)
	}
	if td.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}
	if td.ExpiresAt.Sub(expires).Abs() > time.Second {
		t.Errorf("ExpiresAt = %v, want ~%v", td.ExpiresAt, expires)
	}
}

func TestLoad_EnvVarOverride(t *testing.T) {
	t.Setenv("CANVAS_TOKEN", "env-token-xyz")

	td, err := loadWithDir("https://anything.example.com", t.TempDir())
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if td.Token != "env-token-xyz" {
		t.Errorf("Token = %q, want 'env-token-xyz'", td.Token)
	}
	if !td.ExpiresAt.IsZero() {
		t.Error("ExpiresAt should be zero for env var tokens")
	}
}

func TestDelete_FileBackend(t *testing.T) {
	dir := t.TempDir()
	url := "https://canvas.example.com"

	err := storeWithDir(url, "token123", time.Now().Add(90*24*time.Hour), dir)
	if err != nil {
		t.Fatalf("Store() error: %v", err)
	}

	err = deleteWithDir(url, dir)
	if err != nil {
		t.Fatalf("Delete() error: %v", err)
	}

	_, err = loadWithDir(url, dir)
	if err == nil {
		t.Error("expected error after delete, got nil")
	}
}

func TestLoad_NotFound(t *testing.T) {
	dir := t.TempDir()
	_, err := loadWithDir("https://nonexistent.example.com", dir)
	if err == nil {
		t.Error("expected error for nonexistent token, got nil")
	}
}

func TestMultipleInstances(t *testing.T) {
	dir := t.TempDir()
	url1 := "https://canvas1.example.com"
	url2 := "https://canvas2.example.com"

	err := storeWithDir(url1, "token-1", time.Now().Add(90*24*time.Hour), dir)
	if err != nil {
		t.Fatalf("Store(url1) error: %v", err)
	}
	err = storeWithDir(url2, "token-2", time.Now().Add(60*24*time.Hour), dir)
	if err != nil {
		t.Fatalf("Store(url2) error: %v", err)
	}

	td1, err := loadWithDir(url1, dir)
	if err != nil {
		t.Fatalf("Load(url1) error: %v", err)
	}
	td2, err := loadWithDir(url2, dir)
	if err != nil {
		t.Fatalf("Load(url2) error: %v", err)
	}

	if td1.Token != "token-1" {
		t.Errorf("url1 Token = %q, want 'token-1'", td1.Token)
	}
	if td2.Token != "token-2" {
		t.Errorf("url2 Token = %q, want 'token-2'", td2.Token)
	}
}

package onboard

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chrismdemian/laurus/internal/config"
)

func noEnv(string) string { return "" }

func envOf(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestResolve_Precedence(t *testing.T) {
	env := envOf(map[string]string{EnvToken: "env-token-1234567", EnvURL: "https://env.example.edu/"})

	t.Run("flags beat env and config", func(t *testing.T) {
		r, err := Resolve(Source{FlagToken: "flag-token-1234567", FlagURL: "https://flag.example.edu", Env: env, ExistingURL: "https://cfg.example.edu"})
		if err != nil {
			t.Fatal(err)
		}
		if r.Token != "flag-token-1234567" || r.TokenFrom != "flag" {
			t.Errorf("token = %q from %q", r.Token, r.TokenFrom)
		}
		if r.URL != "https://flag.example.edu" || r.URLFrom != "flag" {
			t.Errorf("url = %q from %q", r.URL, r.URLFrom)
		}
	})
	t.Run("env beats config, trailing slash dropped", func(t *testing.T) {
		r, err := Resolve(Source{Env: env, ExistingURL: "https://cfg.example.edu"})
		if err != nil {
			t.Fatal(err)
		}
		if r.TokenFrom != "env" || r.URL != "https://env.example.edu" || r.URLFrom != "env" {
			t.Errorf("got %+v", r)
		}
	})
	t.Run("config URL is a fallback, token stays missing", func(t *testing.T) {
		r, err := Resolve(Source{Env: noEnv, ExistingURL: "https://cfg.example.edu"})
		if err != nil {
			t.Fatal(err)
		}
		if r.Token != "" || r.URL != "https://cfg.example.edu" || r.URLFrom != "config" || r.Complete() {
			t.Errorf("got %+v", r)
		}
	})
	t.Run("stdin token, newline trimmed", func(t *testing.T) {
		r, err := Resolve(Source{TokenFromStdin: true, Stdin: strings.NewReader("stdin-token-1234567\n"), Env: noEnv})
		if err != nil {
			t.Fatal(err)
		}
		if r.Token != "stdin-token-1234567" || r.TokenFrom != "stdin" {
			t.Errorf("got %+v", r)
		}
	})
	t.Run("empty stdin is an error", func(t *testing.T) {
		if _, err := Resolve(Source{TokenFromStdin: true, Stdin: strings.NewReader("\n"), Env: noEnv}); err == nil {
			t.Error("expected error for empty stdin")
		}
	})
	t.Run("token and token-stdin are exclusive", func(t *testing.T) {
		if _, err := Resolve(Source{FlagToken: "x", TokenFromStdin: true, Env: noEnv}); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("bad URL is rejected", func(t *testing.T) {
		if _, err := Resolve(Source{FlagURL: "school.edu", Env: noEnv}); err == nil {
			t.Error("expected error for URL without scheme")
		}
	})
}

func TestGuard_FailsFastWithoutTTY(t *testing.T) {
	cases := []struct {
		name    string
		r       Resolved
		tty     bool
		wantErr bool
		missing string
	}{
		{"nothing, no tty", Resolved{}, false, true, "token, url"},
		{"token only, no tty", Resolved{Token: "t"}, false, true, "url"},
		{"url only, no tty", Resolved{URL: "u"}, false, true, "token"},
		{"complete, no tty", Resolved{Token: "t", URL: "u"}, false, false, ""},
		{"nothing, tty", Resolved{}, true, false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Guard(tc.r, tc.tty)
			if tc.wantErr != (err != nil) {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if err != nil {
				if !errors.Is(err, ErrNoTTY) {
					t.Errorf("err = %v, want ErrNoTTY", err)
				}
				if !strings.Contains(err.Error(), "missing: "+tc.missing) {
					t.Errorf("err = %q, want missing %q", err, tc.missing)
				}
				if !strings.Contains(err.Error(), "--token") || !strings.Contains(err.Error(), EnvToken) {
					t.Errorf("error must tell the caller how to fix it: %q", err)
				}
			}
		})
	}
}

func TestNormalizeURL(t *testing.T) {
	good := map[string]string{
		"https://q.utoronto.ca":             "https://q.utoronto.ca",
		"https://q.utoronto.ca/":            "https://q.utoronto.ca",
		" https://canvas.school.edu/login ": "https://canvas.school.edu",
		"http://localhost:3000":             "http://localhost:3000",
	}
	for in, want := range good {
		got, err := NormalizeURL(in)
		if err != nil || got != want {
			t.Errorf("NormalizeURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "q.utoronto.ca", "ftp://x.y", "https://", "https:///path"} {
		if _, err := NormalizeURL(bad); err == nil {
			t.Errorf("NormalizeURL(%q) accepted, want error", bad)
		}
	}
}

func profileServer(t *testing.T, wantToken string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/users/self/profile" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+wantToken {
			w.Header().Set("WWW-Authenticate", "Bearer realm=\"canvas\"")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"errors":[{"message":"Invalid access token."}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"name":"Test Student","time_zone":"America/Toronto"}`))
	}))
}

func TestConfigure_NonInteractive(t *testing.T) {
	srv := profileServer(t, "good-token-1234567")
	defer srv.Close()

	var storedURL, storedToken string
	var storedExp time.Time
	saved := &config.Config{}
	res, err := Configure(context.Background(), Options{
		URL:     srv.URL + "/",
		Token:   "good-token-1234567",
		Version: "test",
		StoreToken: func(u, tok string, exp time.Time) error {
			storedURL, storedToken, storedExp = u, tok, exp
			return nil
		},
		LoadConfig: func() (*config.Config, error) { return &config.Config{Theme: "dark", SyncDir: "~/S"}, nil },
		SaveConfig: func(c *config.Config) error { *saved = *c; return nil },
	})
	if err != nil {
		t.Fatalf("Configure: %v", err)
	}
	if res.Name != "Test Student" || res.TimeZone != "America/Toronto" || res.URL != srv.URL {
		t.Errorf("result = %+v", res)
	}
	if storedURL != srv.URL || storedToken != "good-token-1234567" {
		t.Errorf("stored %q / %q", storedURL, storedToken)
	}
	if d := time.Until(storedExp); d < 119*24*time.Hour || d > 121*24*time.Hour {
		t.Errorf("expiry %v not ~120 days out", d)
	}
	if saved.CanvasURL != srv.URL || saved.Theme != "dark" || saved.SyncDir != "~/S" {
		t.Errorf("saved config = %+v (must set URL and keep other fields)", saved)
	}
}

func TestConfigure_BadTokenStoresNothing(t *testing.T) {
	srv := profileServer(t, "good-token-1234567")
	defer srv.Close()

	stored, savedCfg := false, false
	_, err := Configure(context.Background(), Options{
		URL:        srv.URL,
		Token:      "wrong-token-1234567",
		StoreToken: func(string, string, time.Time) error { stored = true; return nil },
		LoadConfig: func() (*config.Config, error) { return &config.Config{}, nil },
		SaveConfig: func(*config.Config) error { savedCfg = true; return nil },
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "token validation failed") {
		t.Errorf("err = %v", err)
	}
	if stored || savedCfg {
		t.Errorf("stored=%v savedCfg=%v; a rejected token must not be persisted", stored, savedCfg)
	}
}

func TestConfigure_RejectsShortOrMissingToken(t *testing.T) {
	for _, tok := range []string{"", "short"} {
		_, err := Configure(context.Background(), Options{URL: "https://x.edu", Token: tok,
			StoreToken: func(string, string, time.Time) error { t.Fatal("must not store"); return nil }})
		if err == nil {
			t.Errorf("token %q accepted", tok)
		}
	}
}

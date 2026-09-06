package doctor

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chrismdemian/laurus/internal/auth"
	"github.com/chrismdemian/laurus/internal/cache"
	"github.com/chrismdemian/laurus/internal/config"
	"github.com/chrismdemian/laurus/internal/iostreams"
	"github.com/chrismdemian/laurus/pkg/cmdutil"
)

func testFactory(t *testing.T, jsonMode bool) (*cmdutil.Factory, *strings.Builder) {
	t.Helper()
	t.Setenv("HOME", t.TempDir()) // config.DefaultPath and update cache stay in the sandbox
	ios, _, out, _ := iostreams.Test()
	ios.IsJSON = jsonMode
	f := &cmdutil.Factory{
		Version:   "dev",
		IOStreams: func() *iostreams.IOStreams { return ios },
		Cache: func() (*cache.DB, error) {
			return cache.Open(t.TempDir() + "/cache.db")
		},
	}
	sb := &strings.Builder{}
	t.Cleanup(func() { sb.WriteString(out.String()) })
	return f, sb
}

func TestDoctor_ExitsNonZeroOnFail(t *testing.T) {
	f, _ := testFactory(t, true)
	f.Config = func() (*config.Config, error) { return nil, errors.New("boom") }

	err := doctorRun(f)
	if err == nil {
		t.Fatal("doctorRun returned nil with a FAIL check; agents could not gate on the exit code")
	}
	if !strings.Contains(err.Error(), "failed") {
		t.Errorf("err = %v", err)
	}
}

func TestDoctor_UnauthenticatedIsFail(t *testing.T) {
	f, _ := testFactory(t, true)
	f.Config = func() (*config.Config, error) { return &config.Config{CanvasURL: "https://x.example.edu"}, nil }
	f.Auth = func(string) (*auth.TokenData, error) { return nil, errors.New("no token") }

	if err := doctorRun(f); err == nil {
		t.Fatal("expected non-nil error when auth fails")
	}
}

func TestDoctor_PassesWithWarningsOnly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Rate-Limit-Remaining", "700")
		_, _ = w.Write([]byte(`{"id":1,"name":"Test Student"}`))
	}))
	defer srv.Close()

	ios, _, out, _ := iostreams.Test()
	ios.IsJSON = true
	t.Setenv("HOME", t.TempDir())
	f := &cmdutil.Factory{
		Version:   "dev",
		IOStreams: func() *iostreams.IOStreams { return ios },
		Config:    func() (*config.Config, error) { return &config.Config{CanvasURL: srv.URL}, nil },
		Auth: func(string) (*auth.TokenData, error) {
			return &auth.TokenData{Token: "tok", ExpiresAt: time.Now().Add(90 * 24 * time.Hour)}, nil
		},
		Cache: func() (*cache.DB, error) { return nil, errors.New("no cache") }, // WARN, not FAIL
	}

	err := doctorRun(f)
	if err != nil {
		t.Fatalf("doctorRun with only PASS/WARN checks returned %v; warnings must exit zero", err)
	}

	var results []checkResult
	if err := json.Unmarshal([]byte(out.String()), &results); err != nil {
		t.Fatalf("stdout is not clean JSON: %v\n%s", err, out.String())
	}
	var sawWarn bool
	for _, r := range results {
		if r.Status == statusFail {
			t.Errorf("unexpected FAIL: %+v", r)
		}
		if r.Status == statusWarn {
			sawWarn = true
		}
	}
	if !sawWarn {
		t.Error("test setup expected at least one WARN (cache) to prove warnings do not fail")
	}
}

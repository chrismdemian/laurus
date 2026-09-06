// Package onboard implements the non-interactive path shared by `laurus setup`
// and `laurus auth login`: resolve a Canvas URL and token from flags, stdin,
// environment, or existing config; refuse to prompt when there is no terminal;
// then validate the token, store it, and save the URL.
package onboard

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/chrismdemian/laurus/internal/auth"
	"github.com/chrismdemian/laurus/internal/canvas"
	"github.com/chrismdemian/laurus/internal/config"
)

// EnvToken and EnvURL are the environment variables honoured everywhere.
const (
	EnvToken = "CANVAS_TOKEN"
	EnvURL   = "CANVAS_URL"
)

// DefaultExpiryDays is assumed for tokens whose expiry Canvas does not report.
const DefaultExpiryDays = 120

// ErrNoTTY is returned when values are missing and no terminal is attached.
var ErrNoTTY = errors.New("no TTY and no --token; pass --token (or --token-stdin) and --url, or set " + EnvToken + " and " + EnvURL)

// Source describes where a token and URL may come from, in precedence order:
// flag, stdin (token only), environment, existing config (URL only).
type Source struct {
	FlagToken      string
	TokenFromStdin bool
	FlagURL        string

	Stdin       io.Reader
	Env         func(string) string // nil means os.Getenv
	ExistingURL string
}

// Resolved holds whatever could be determined without prompting. Empty fields
// mean "still needed".
type Resolved struct {
	Token     string
	TokenFrom string // "flag", "stdin", "env", or ""
	URL       string
	URLFrom   string // "flag", "env", "config", or ""
}

// Resolve applies the precedence rules. It reads stdin only when
// TokenFromStdin is set.
func Resolve(src Source) (Resolved, error) {
	env := src.Env
	if env == nil {
		env = os.Getenv
	}
	var r Resolved

	switch {
	case src.FlagToken != "" && src.TokenFromStdin:
		return r, errors.New("--token and --token-stdin are mutually exclusive")
	case src.FlagToken != "":
		r.Token, r.TokenFrom = strings.TrimSpace(src.FlagToken), "flag"
	case src.TokenFromStdin:
		if src.Stdin == nil {
			return r, errors.New("--token-stdin: no stdin")
		}
		data, err := io.ReadAll(src.Stdin)
		if err != nil {
			return r, fmt.Errorf("reading token from stdin: %w", err)
		}
		r.Token = strings.TrimSpace(string(data))
		if r.Token == "" {
			return r, errors.New("--token-stdin: stdin was empty")
		}
		r.TokenFrom = "stdin"
	case env(EnvToken) != "":
		r.Token, r.TokenFrom = strings.TrimSpace(env(EnvToken)), "env"
	}

	switch {
	case src.FlagURL != "":
		r.URL, r.URLFrom = src.FlagURL, "flag"
	case env(EnvURL) != "":
		r.URL, r.URLFrom = env(EnvURL), "env"
	case src.ExistingURL != "":
		r.URL, r.URLFrom = src.ExistingURL, "config"
	}
	if r.URL != "" {
		u, err := NormalizeURL(r.URL)
		if err != nil {
			return r, err
		}
		r.URL = u
	}
	return r, nil
}

// Complete reports whether nothing remains to be asked.
func (r Resolved) Complete() bool { return r.Token != "" && r.URL != "" }

// Guard enforces the fail-fast rule: when something is still needed and no
// terminal is attached, return ErrNoTTY instead of letting a prompt hang.
func Guard(r Resolved, interactive bool) error {
	if r.Complete() || interactive {
		return nil
	}
	var missing []string
	if r.Token == "" {
		missing = append(missing, "token")
	}
	if r.URL == "" {
		missing = append(missing, "url")
	}
	return fmt.Errorf("%w (missing: %s)", ErrNoTTY, strings.Join(missing, ", "))
}

// IsInteractive is true only when both stdin and stdout are terminals, which
// is what a huh form needs. Piping a token in (--token-stdin) makes stdin a
// pipe, so that path is non-interactive by construction.
func IsInteractive(stdin, stdout *os.File) bool {
	if stdin == nil || stdout == nil {
		return false
	}
	return term.IsTerminal(int(stdin.Fd())) && term.IsTerminal(int(stdout.Fd()))
}

// NormalizeURL requires an http(s) scheme and a host, and drops any path or
// trailing slash so "https://school.edu/" and "https://school.edu" agree.
func NormalizeURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid Canvas URL %q: %w", raw, err)
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return "", fmt.Errorf("invalid Canvas URL %q: must start with https:// or http://", raw)
	}
	if u.Host == "" {
		return "", fmt.Errorf("invalid Canvas URL %q: missing host", raw)
	}
	return u.Scheme + "://" + u.Host, nil
}

// Options drives Configure. The function fields default to the real
// implementations; tests inject their own so nothing touches a keychain or
// the user's config file.
type Options struct {
	URL        string
	Token      string
	Version    string
	ExpiryDays int

	StoreToken func(canvasURL, token string, expiresAt time.Time) error
	LoadConfig func() (*config.Config, error)
	SaveConfig func(*config.Config) error
}

// Result is what the callers print.
type Result struct {
	Name      string
	TimeZone  string
	URL       string
	ExpiresAt time.Time
	Days      int
}

type userProfile struct {
	Name     string `json:"name"`
	TimeZone string `json:"time_zone"`
}

// Configure validates the token against /users/self/profile, stores it, and
// writes the URL to config. It never prompts.
func Configure(ctx context.Context, opts Options) (*Result, error) {
	if opts.Token == "" {
		return nil, errors.New("no token")
	}
	if len(opts.Token) < 10 {
		return nil, errors.New("token looks too short")
	}
	canvasURL, err := NormalizeURL(opts.URL)
	if err != nil {
		return nil, err
	}
	if opts.ExpiryDays <= 0 {
		opts.ExpiryDays = DefaultExpiryDays
	}
	if opts.StoreToken == nil {
		opts.StoreToken = auth.Store
	}
	if opts.LoadConfig == nil {
		opts.LoadConfig = config.Load
	}
	if opts.SaveConfig == nil {
		opts.SaveConfig = config.Save
	}

	client := canvas.NewClient(canvasURL, opts.Token, opts.Version)
	profile, err := canvas.Get[userProfile](ctx, client, "/api/v1/users/self/profile", nil)
	if err != nil {
		return nil, fmt.Errorf("token validation failed: %w", err)
	}

	expiresAt := time.Now().Add(time.Duration(opts.ExpiryDays) * 24 * time.Hour)
	if err := opts.StoreToken(canvasURL, opts.Token, expiresAt); err != nil {
		return nil, fmt.Errorf("storing token: %w", err)
	}

	cfg, err := opts.LoadConfig()
	if err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}
	cfg.CanvasURL = canvasURL
	if err := opts.SaveConfig(cfg); err != nil {
		return nil, fmt.Errorf("saving config: %w", err)
	}

	return &Result{
		Name:      profile.Name,
		TimeZone:  profile.TimeZone,
		URL:       canvasURL,
		ExpiresAt: expiresAt,
		Days:      auth.DaysRemaining(&auth.TokenData{ExpiresAt: expiresAt}),
	}, nil
}

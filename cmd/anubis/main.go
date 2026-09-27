package main

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// version is reported in the startup log line. Override at build time with
// -ldflags "-X main.version=$(git describe --tags)".
var version = "dev"

// Defaults for the model provider. Any OpenAI-compatible endpoint works; these
// point at OpenCode Zen's free tier so the CLI runs with no configuration.
const (
	defaultLLMBaseURL    = "https://opencode.ai/zen/v1"
	defaultModel         = "big-pickle"
	defaultGitHubBaseURL = "https://api.github.com"
)

// httpTimeout bounds a single GitHub or model API call so a hung connection
// cannot stall a workflow indefinitely.
const httpTimeout = 120 * time.Second

// defaultMaxConcurrency is how many specialist reviewers may run at once. The
// default of 1 runs them one after another. Sequential is the safe default: a
// review costs one provider call per specialist, so fanning them out is the
// fastest way to hit a free tier's rate limit. Raise ANUBIS_MAX_CONCURRENCY
// when the provider can absorb the fan-out and the wall-clock saving is worth
// the spend.
const defaultMaxConcurrency = 1

// maxDiffBytes caps how much of a diff is sent to the model. Very large pull
// requests are truncated with a marker rather than failing the request, because
// most model context windows are far smaller than the API's own limits and a
// hard failure here would be indistinguishable from a provider outage.
const maxDiffBytes = 200_000

// noFindingsComment is published when the review completes cleanly.
const noFindingsComment = "No findings."

func main() {
	Review()
}

func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

// maxConcurrency resolves how many specialist reviewers may run at once from
// ANUBIS_MAX_CONCURRENCY. An unset, unparseable or non-positive value falls back
// to the sequential default rather than failing the run, because a typo in a
// tuning variable should not stop a review from happening.
func maxConcurrency() int {
	raw := strings.TrimSpace(os.Getenv("ANUBIS_MAX_CONCURRENCY"))
	if raw == "" {
		return defaultMaxConcurrency
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		slog.Warn("ignoring invalid ANUBIS_MAX_CONCURRENCY, running sequentially", "value", raw)
		return defaultMaxConcurrency
	}
	return n
}

// sourceRoot is the absolute module root (directory containing the source
// tree). It is derived at init from the compile-time path of this file via
// runtime.Caller, so log source locations survive on any machine/container
// regardless of where the binary was built or runs.
var sourceRoot = func() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(file))) + string(filepath.Separator)
}()

func setupLogger(level string) {
	out := io.Writer(os.Stderr)
	if colorEnabled() {
		out = &colorWriter{w: os.Stderr}
	}
	h := slog.NewTextHandler(out, &slog.HandlerOptions{
		Level:     parseLogLevel(level),
		AddSource: true,
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey && a.Value.Kind() == slog.KindTime {
				a.Value = slog.StringValue(a.Value.Time().UTC().Format(time.RFC3339))
			}
			if a.Key == slog.SourceKey {
				if src, ok := a.Value.Any().(*slog.Source); ok {
					rel := strings.TrimPrefix(src.File, sourceRoot)
					if rel == src.File {
						rel = filepath.Base(src.File)
					}
					a.Value = slog.StringValue(rel + ":" + strconv.Itoa(src.Line))
				}
			}
			return a
		},
	})
	slog.SetDefault(slog.New(h))
}

func parseLogLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func fatal(message string) {
	slog.Error(message)
	os.Exit(2)
}

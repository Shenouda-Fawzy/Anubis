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

/*
TODOs:
  - [DONE] Parse error response
  - [DONE] Write unit tests
  - [DONE] Write integration tests
  - [DONE] Write E2E tests
  - [DONE] Use the standard library log/slog
  - Document env vars
  - Publish as Github action
  - Use structured response
*/
func main() {
	Review()
}

func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
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

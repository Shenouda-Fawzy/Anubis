package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"sync"
)

const (
	ansiReset  = "\x1b[0m"
	ansiBold   = "\x1b[1m"
	ansiDim    = "\x1b[2m"
	ansiCyan   = "\x1b[36m"
	ansiGreen  = "\x1b[32m"
	ansiYellow = "\x1b[33m"
	ansiRed    = "\x1b[31m"
)

var levelColor = map[string]string{
	"DEBUG": ansiCyan,
	"INFO":  ansiGreen,
	"WARN":  ansiYellow,
	"ERROR": ansiRed + ansiBold,
}

// colorEnabled reports whether log output should be ANSI-colored. It defaults
// to on only when stderr is a terminal, and can be forced or disabled via
// ANUBIS_LOG_COLOR (always|auto|never). NO_COLOR wins unless explicitly forced.
func colorEnabled() bool {
	switch strings.ToLower(os.Getenv("ANUBIS_LOG_COLOR")) {
	case "always", "on", "1", "true":
		return true
	case "never", "off", "0", "false":
		return false
	}
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	return isTerminal(os.Stderr)
}

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// colorWriter buffers log records until a newline and colorizes the
// time=, source= and level= tokens before forwarding them. It keeps the
// default slog TextHandler output otherwise untouched.
type colorWriter struct {
	mu  sync.Mutex
	w   io.Writer
	buf []byte
}

func (cw *colorWriter) Write(p []byte) (int, error) {
	cw.mu.Lock()
	defer cw.mu.Unlock()

	cw.buf = append(cw.buf, p...)
	for {
		i := bytes.IndexByte(cw.buf, '\n')
		if i < 0 {
			return len(p), nil
		}
		line := cw.buf[:i+1]
		cw.buf = cw.buf[i+1:]
		if err := cw.writeLine(line); err != nil {
			return len(p), err
		}
	}
}

func (cw *colorWriter) writeLine(line []byte) error {
	s := colorToken(strings.TrimSuffix(string(line), "\n"), "time=", ansiDim)
	s = colorToken(s, "source=", ansiDim)
	s = colorLevel(s)
	_, err := io.WriteString(cw.w, s+"\n")
	return err
}

func colorLevel(s string) string {
	i := strings.Index(s, "level=")
	if i < 0 {
		return s
	}
	j := i + len("level=")
	for j < len(s) && s[j] != ' ' {
		j++
	}
	if j >= len(s) {
		j = len(s)
	}
	lvl := strings.Trim(s[i+len("level="):j], `"`)
	color, ok := levelColor[lvl]
	if !ok {
		return s
	}
	return s[:i] + color + s[i:j] + ansiReset + s[j:]
}

// colorToken wraps the value of the first occurrence of key in color.
func colorToken(s, key, color string) string {
	i := strings.Index(s, key)
	if i < 0 {
		return s
	}
	j := i + len(key)
	for j < len(s) && s[j] != ' ' {
		j++
	}
	if j >= len(s) {
		j = len(s)
	}
	return s[:i] + color + s[i:j] + ansiReset + s[j:]
}

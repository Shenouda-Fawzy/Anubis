package main

import (
	"fmt"
	"os"
)

func main() {
	Review()
}

func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
func fatal(message string) { fmt.Fprintln(os.Stderr, message); os.Exit(2) }

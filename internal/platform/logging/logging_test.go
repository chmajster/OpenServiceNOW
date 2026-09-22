package logging

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestParseLevel(t *testing.T) {
	for input, want := range map[string]slog.Level{
		"debug": slog.LevelDebug,
		"info":  slog.LevelInfo,
		"warn":  slog.LevelWarn,
		"error": slog.LevelError,
	} {
		got, err := ParseLevel(input)
		if err != nil {
			t.Fatalf("ParseLevel(%q): %v", input, err)
		}
		if got != want {
			t.Fatalf("ParseLevel(%q)=%v want %v", input, got, want)
		}
	}
}

func TestNewWritesJSON(t *testing.T) {
	var buf bytes.Buffer
	logger, err := New(&buf, "info")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	logger.Info("started", "module", "test")
	if !strings.Contains(buf.String(), `"msg":"started"`) || !strings.Contains(buf.String(), `"module":"test"`) {
		t.Fatalf("unexpected log output: %s", buf.String())
	}
}

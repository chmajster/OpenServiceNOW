package logging

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
)

func New(out io.Writer, level string) (*slog.Logger, error) {
	parsed, err := ParseLevel(level)
	if err != nil {
		return nil, err
	}
	return slog.New(slog.NewJSONHandler(out, &slog.HandlerOptions{Level: parsed})), nil
}

func ParseLevel(value string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "debug":
		return slog.LevelDebug, nil
	case "", "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("unsupported log level %q", value)
	}
}

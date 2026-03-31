package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

var logger *slog.Logger

// Init sets up the structured logger with console and optional file output.
func Init(debug bool, logDir, env string) {
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}

	var writers []io.Writer
	writers = append(writers, os.Stdout)

	if logDir != "" {
		os.MkdirAll(logDir, 0o755)
		logFile := filepath.Join(logDir, fmt.Sprintf("%s-dreadgoad-%s.log",
			env, time.Now().Format("20060102_150405")))
		if f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
			writers = append(writers, f)
		}
	}

	w := io.MultiWriter(writers...)
	logger = slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)
}

// Get returns the configured logger.
func Get() *slog.Logger {
	if logger == nil {
		logger = slog.Default()
	}
	return logger
}

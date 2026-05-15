package logger

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type contextKey string

const loggerKey contextKey = "logger"

type ReopenWriter struct {
	path string
	file *os.File
	mu   sync.Mutex
}

func NewReopenWriter(path string) (*ReopenWriter, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}
	return &ReopenWriter{path: path, file: f}, nil
}

func (w *ReopenWriter) openFile() (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(w.path), 0755); err != nil {
		return nil, err
	}
	return os.OpenFile(w.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
}

func (w *ReopenWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if _, err := os.Stat(w.path); os.IsNotExist(err) {
		w.file.Close()
		f, err := w.openFile()
		if err != nil {
			return 0, err
		}
		w.file = f
	}

	return w.file.Write(p)
}

func (w *ReopenWriter) Reopen() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.file.Close()
	f, err := w.openFile()
	if err != nil {
		return err
	}
	w.file = f
	return nil
}

func (w *ReopenWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.file.Close()
}

var globalRW *ReopenWriter

func Init(logFile string, logLevel string) (*slog.Logger, error) {
	rw, err := NewReopenWriter(logFile)
	if err != nil {
		return nil, err
	}
	globalRW = rw

	var level slog.Level
	switch strings.ToLower(logLevel) {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	mw := io.MultiWriter(os.Stderr, rw)
	l := slog.New(slog.NewJSONHandler(mw, &slog.HandlerOptions{
		Level: level,
	}))
	return l, nil
}

func ReopenLog() error {
	if globalRW != nil {
		return globalRW.Reopen()
	}
	return nil
}

func NewContext(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey, l)
}

func FromContext(ctx context.Context) *slog.Logger {
	l, ok := ctx.Value(loggerKey).(*slog.Logger)
	if !ok {
		return slog.Default()
	}
	return l
}

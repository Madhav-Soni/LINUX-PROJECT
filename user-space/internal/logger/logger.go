package logger

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

type Logger struct {
	mu     sync.Mutex
	enc    *json.Encoder
	closer io.Closer
}

type entry struct {
	Timestamp string                 `json:"ts"`
	Level     string                 `json:"level"`
	Message   string                 `json:"msg"`
	Fields    map[string]interface{} `json:"fields,omitempty"`
}

func New(path string) (*Logger, error) {
	var writer io.Writer
	var closer io.Closer

	if path == "" {
		writer = os.Stdout
	} else {
		file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return nil, fmt.Errorf("open log file %s: %w", path, err)
		}
		writer = file
		closer = file
	}

	enc := json.NewEncoder(writer)
	enc.SetEscapeHTML(false)

	return &Logger{enc: enc, closer: closer}, nil
}

func (l *Logger) Close() error {
	if l.closer != nil {
		return l.closer.Close()
	}
	return nil
}

func (l *Logger) Info(msg string, fields map[string]interface{}) {
	l.write("info", msg, fields)
}

func (l *Logger) Error(msg string, fields map[string]interface{}) {
	l.write("error", msg, fields)
}

func (l *Logger) write(level, msg string, fields map[string]interface{}) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	_ = l.enc.Encode(entry{
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Level:     level,
		Message:   msg,
		Fields:    fields,
	})
}

package store

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

// AuditWriter appends events as JSONL to an audit log file.
// It is append-only and never modifies or truncates the file.
type AuditWriter struct {
	mu   sync.Mutex
	path string
	file *os.File
}

// NewAuditWriter creates an AuditWriter that appends to the given path.
// The file is created if it does not exist.
func NewAuditWriter(path string) (*AuditWriter, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("open audit log %s: %w", path, err)
	}
	return &AuditWriter{path: path, file: f}, nil
}

// WriteEvent appends a single event as a JSON line.
func (w *AuditWriter) WriteEvent(e Event) error {
	data, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("marshal audit event: %w", err)
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	data = append(data, '\n')
	if _, err := w.file.Write(data); err != nil {
		return fmt.Errorf("write audit event: %w", err)
	}
	return nil
}

// Close closes the underlying file.
func (w *AuditWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file != nil {
		return w.file.Close()
	}
	return nil
}

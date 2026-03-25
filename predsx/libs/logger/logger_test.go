package logger

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

func TestLogger(t *testing.T) {
	// Redirect stdout to capture JSON logs
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	log := NewLogger("info")
	log.Info("test message", "key", "value")

	w.Close()
	var buf bytes.Buffer
	io.Copy(&buf, r)
	os.Stdout = old

	output := buf.String()
	if !strings.Contains(output, "test message") {
		t.Errorf("expected log to contain 'test message', got %s", output)
	}
	if !strings.Contains(output, "\"key\":\"value\"") {
		t.Errorf("expected log to contain 'key':'value', got %s", output)
	}
}

func TestWith(t *testing.T) {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	log := NewLogger("info").With("service", "test-svc")
	log.Info("msg")

	w.Close()
	var buf bytes.Buffer
	io.Copy(&buf, r)
	os.Stdout = old

	output := buf.String()
	if !strings.Contains(output, "\"service\":\"test-svc\"") {
		t.Errorf("expected log to contain service attribute, got %s", output)
	}
}

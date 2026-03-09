package config

import (
	"os"
	"testing"
)

func TestConfig(t *testing.T) {
	cfg := New()

	t.Run("Get with fallback", func(t *testing.T) {
		val := cfg.Get("NON_EXISTENT", "default")
		if val != "default" {
			t.Errorf("expected default, got %s", val)
		}
	})

	t.Run("Get from env", func(t *testing.T) {
		os.Setenv("TEST_KEY", "test-val")
		defer os.Unsetenv("TEST_KEY")
		val := cfg.Get("TEST_KEY", "default")
		if val != "test-val" {
			t.Errorf("expected test-val, got %s", val)
		}
	})

	t.Run("GetInt", func(t *testing.T) {
		os.Setenv("TEST_INT", "123")
		defer os.Unsetenv("TEST_INT")
		val := cfg.GetInt("TEST_INT", 0)
		if val != 123 {
			t.Errorf("expected 123, got %d", val)
		}
	})

	t.Run("GetBool", func(t *testing.T) {
		os.Setenv("TEST_BOOL", "true")
		defer os.Unsetenv("TEST_BOOL")
		val := cfg.GetBool("TEST_BOOL", false)
		if val != true {
			t.Errorf("expected true, got %v", val)
		}
	})

	t.Run("Require success", func(t *testing.T) {
		os.Setenv("REQ_KEY", "val")
		defer os.Unsetenv("REQ_KEY")
		val, err := cfg.Require("REQ_KEY")
		if err != nil || val != "val" {
			t.Errorf("expected val, got %s, err %v", val, err)
		}
	})

	t.Run("Require failure", func(t *testing.T) {
		_, err := cfg.Require("MISSING_KEY")
		if err == nil {
			t.Error("expected error, got nil")
		}
	})
}

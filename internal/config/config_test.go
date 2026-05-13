package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadMissingFileReturnsDefaults(t *testing.T) {
	t.Parallel()

	cfg, err := Load(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	want := Defaults()
	if cfg != want {
		t.Fatalf("Load() = %#v, want %#v", cfg, want)
	}
}

func TestSaveAndLoadPersistsJSON(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "nested", "config.json")
	cfg := Defaults()
	cfg.StateDir = "/tmp/maude-state"
	cfg.DefaultSession = "work"
	cfg.TmuxPrefix = "custom"
	cfg.ClaudeBinary = "/opt/bin/claude"
	cfg.StartupWait = "3s"
	cfg.WaitTimeout = "45s"
	cfg.WaitPollInterval = "750ms"
	cfg.CaptureDelay = "100ms"
	cfg.CaptureLines = 75

	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !strings.Contains(string(data), `"state_dir": "/tmp/maude-state"`) {
		t.Fatalf("saved config did not contain expected state_dir: %s", data)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got != cfg {
		t.Fatalf("Load() = %#v, want %#v", got, cfg)
	}
}

func TestDurationHelpers(t *testing.T) {
	t.Parallel()

	cfg := Defaults()
	got, err := cfg.StartupWaitDuration()
	if err != nil {
		t.Fatalf("StartupWaitDuration() error = %v", err)
	}
	if got != 2*time.Second {
		t.Fatalf("StartupWaitDuration() = %v, want 2s", got)
	}

	cfg.WaitTimeout = "nope"
	if _, err := cfg.WaitTimeoutDuration(); err == nil {
		t.Fatal("WaitTimeoutDuration() error = nil, want parse error")
	}
}

func TestDefaultPath(t *testing.T) {
	t.Parallel()

	if got := DefaultPath(); got != filepath.Join("state", "config.json") {
		t.Fatalf("DefaultPath() = %q", got)
	}
}

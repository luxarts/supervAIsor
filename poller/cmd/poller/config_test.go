package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestResolveBackendURL_DerivesFromHostPort(t *testing.T) {
	got := resolveBackendURL(Config{BackendHost: "10.0.0.5", BackendPort: 9000})
	want := "ws://10.0.0.5:9000/ws/ingest"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestResolveBackendURL_RespectsOverride(t *testing.T) {
	got := resolveBackendURL(Config{BackendURL: "ws://custom/path", BackendHost: "x", BackendPort: 1})
	if got != "ws://custom/path" {
		t.Errorf("override ignored: got %q", got)
	}
}

func TestResolveHostname_ExplicitWins(t *testing.T) {
	got, err := resolveHostname(Config{Hostname: "explicit-name"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "explicit-name" {
		t.Errorf("got %q", got)
	}
}

func TestLoadConfig_FirstRunWritesDefaults(t *testing.T) {
	home := t.TempDir()
	c, err := loadConfig(home)
	if err != nil {
		t.Fatal(err)
	}
	if c.BackendHost != "localhost" || c.BackendPort != 8080 {
		t.Errorf("bad defaults: %#v", c)
	}
	if c.Interval != time.Second {
		t.Errorf("default interval: %v", c.Interval)
	}
	if _, err := os.Stat(settingsPath(home)); err != nil {
		t.Fatalf("expected settings file at %s: %v", settingsPath(home), err)
	}
	if c.StateFile != filepath.Join(home, ".supervaisor", "state.json") {
		t.Errorf("state file path: %q", c.StateFile)
	}
}

func TestLoadConfig_ReadsExistingFile(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".supervaisor"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{
	  "backend_host": "5.6.7.8",
	  "backend_port": 1234,
	  "hostname": "from-file",
	  "interval": "500ms",
	  "backend_url": "ws://file/ws/ingest"
	}`
	if err := os.WriteFile(settingsPath(home), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := loadConfig(home)
	if err != nil {
		t.Fatal(err)
	}
	if c.BackendHost != "5.6.7.8" || c.BackendPort != 1234 || c.Hostname != "from-file" {
		t.Errorf("file not applied: %#v", c)
	}
	if c.Interval != 500*time.Millisecond {
		t.Errorf("interval not parsed: %v", c.Interval)
	}
	if c.BackendURL != "ws://file/ws/ingest" {
		t.Errorf("BackendURL = %q", c.BackendURL)
	}
}

func TestLoadConfig_PartialFileKeepsDefaults(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".supervaisor"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsPath(home), []byte(`{"hostname":"mac-A"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := loadConfig(home)
	if err != nil {
		t.Fatal(err)
	}
	if c.Hostname != "mac-A" {
		t.Errorf("hostname: %q", c.Hostname)
	}
	if c.BackendHost != "localhost" || c.BackendPort != 8080 {
		t.Errorf("defaults clobbered by partial file: %#v", c)
	}
}

func TestLoadConfig_InvalidIntervalInFile(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".supervaisor"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsPath(home), []byte(`{"interval":"not-a-duration"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig(home); err == nil {
		t.Fatal("expected error for invalid interval")
	}
}

func TestPaths_UseHomeDotSupervaisor(t *testing.T) {
	if got := settingsPath("/home/u"); got != "/home/u/.supervaisor/settings.json" {
		t.Errorf("settings path: %q", got)
	}
	if got := statePath("/home/u"); got != "/home/u/.supervaisor/state.json" {
		t.Errorf("state path: %q", got)
	}
}

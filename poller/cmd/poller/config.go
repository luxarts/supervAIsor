package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Settings file location. Hard-coded — this binary runs as a native per-user
// service, so configuration lives in a single well-known JSON file with no
// flag or env var overrides.
const (
	settingsDir  = ".supervaisor"
	settingsFile = "settings.json"
	stateFile    = "state.json"
)

type Config struct {
	ProjectsDir string        `json:"projects_dir"`
	StateFile   string        `json:"state_file"`
	BackendHost string        `json:"backend_host"`
	BackendPort int           `json:"backend_port"`
	BackendURL  string        `json:"backend_url,omitempty"`
	Hostname    string        `json:"hostname,omitempty"`
	Interval    time.Duration `json:"-"`
	IntervalStr string        `json:"interval"`
}

func settingsPath(home string) string { return filepath.Join(home, settingsDir, settingsFile) }
func statePath(home string) string    { return filepath.Join(home, settingsDir, stateFile) }

func defaultConfig(home string) Config {
	return Config{
		ProjectsDir: filepath.Join(home, ".claude", "projects"),
		StateFile:   statePath(home),
		BackendHost: "localhost",
		BackendPort: 8080,
		Interval:    time.Second,
		IntervalStr: "1s",
	}
}

func writeConfig(path string, c Config) error {
	if c.IntervalStr == "" {
		c.IntervalStr = c.Interval.String()
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// readConfig merges values from path into c, leaving fields absent in the
// file at their pre-call defaults.
func readConfig(path string, c *Config) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var raw Config
	if err := json.Unmarshal(b, &raw); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	if raw.ProjectsDir != "" {
		c.ProjectsDir = raw.ProjectsDir
	}
	if raw.StateFile != "" {
		c.StateFile = raw.StateFile
	}
	if raw.BackendHost != "" {
		c.BackendHost = raw.BackendHost
	}
	if raw.BackendPort != 0 {
		c.BackendPort = raw.BackendPort
	}
	if raw.BackendURL != "" {
		c.BackendURL = raw.BackendURL
	}
	if raw.Hostname != "" {
		c.Hostname = raw.Hostname
	}
	if raw.IntervalStr != "" {
		d, err := time.ParseDuration(raw.IntervalStr)
		if err != nil {
			return fmt.Errorf("interval: %w", err)
		}
		c.Interval = d
		c.IntervalStr = raw.IntervalStr
	}
	return nil
}

// loadConfig reads ~/.supervaisor/settings.json, materializing it with
// built-in defaults on first run. The home argument is injected so tests
// can point at a sandbox; production callers pass os.UserHomeDir().
func loadConfig(home string) (Config, error) {
	c := defaultConfig(home)
	path := settingsPath(home)

	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := writeConfig(path, c); err != nil {
			return c, fmt.Errorf("create default settings %s: %w", path, err)
		}
		fmt.Fprintf(os.Stderr, "poller: wrote default settings to %s\n", path)
		return c, nil
	} else if err != nil {
		return c, fmt.Errorf("stat %s: %w", path, err)
	}
	if err := readConfig(path, &c); err != nil {
		return c, err
	}
	return c, nil
}

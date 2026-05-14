package main

import "testing"

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

func TestLoadConfig_Defaults(t *testing.T) {
	c, err := loadConfig(nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.BackendHost != "localhost" || c.BackendPort != 8080 {
		t.Errorf("bad defaults: %#v", c)
	}
}

func TestLoadConfig_FlagsApplied(t *testing.T) {
	c, err := loadConfig([]string{"-backend-host", "1.2.3.4", "-backend-port", "9999", "-hostname", "mac-A"})
	if err != nil {
		t.Fatal(err)
	}
	if c.BackendHost != "1.2.3.4" || c.BackendPort != 9999 || c.Hostname != "mac-A" {
		t.Errorf("flags not applied: %#v", c)
	}
}

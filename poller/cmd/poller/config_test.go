package main

import "testing"

func emptyEnv(string) string { return "" }

func envFromMap(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

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
	c, err := loadConfig(nil, emptyEnv)
	if err != nil {
		t.Fatal(err)
	}
	if c.BackendHost != "localhost" || c.BackendPort != 8080 {
		t.Errorf("bad defaults: %#v", c)
	}
}

func TestLoadConfig_FlagsApplied(t *testing.T) {
	c, err := loadConfig([]string{"-backend-host", "1.2.3.4", "-backend-port", "9999", "-hostname", "mac-A"}, emptyEnv)
	if err != nil {
		t.Fatal(err)
	}
	if c.BackendHost != "1.2.3.4" || c.BackendPort != 9999 || c.Hostname != "mac-A" {
		t.Errorf("flags not applied: %#v", c)
	}
}

func TestLoadConfig_EnvApplied(t *testing.T) {
	env := envFromMap(map[string]string{
		"SUPERVAISOR_BACKEND_HOST": "5.6.7.8",
		"SUPERVAISOR_BACKEND_PORT": "1234",
		"SUPERVAISOR_HOSTNAME":     "from-env",
		"SUPERVAISOR_INTERVAL":     "500ms",
		"SUPERVAISOR_BACKEND_URL":  "ws://env/ws/ingest",
	})
	c, err := loadConfig(nil, env)
	if err != nil {
		t.Fatal(err)
	}
	if c.BackendHost != "5.6.7.8" || c.BackendPort != 1234 || c.Hostname != "from-env" {
		t.Errorf("env not applied: %#v", c)
	}
	if c.Interval != 500_000_000 { // 500ms in ns
		t.Errorf("interval not parsed: %v", c.Interval)
	}
	if c.BackendURL != "ws://env/ws/ingest" {
		t.Errorf("BackendURL = %q", c.BackendURL)
	}
}

func TestLoadConfig_FlagOverridesEnv(t *testing.T) {
	env := envFromMap(map[string]string{
		"SUPERVAISOR_BACKEND_HOST": "from-env",
		"SUPERVAISOR_HOSTNAME":     "host-from-env",
	})
	c, err := loadConfig([]string{"-backend-host", "from-flag", "-hostname", "host-from-flag"}, env)
	if err != nil {
		t.Fatal(err)
	}
	if c.BackendHost != "from-flag" {
		t.Errorf("flag did not override env: BackendHost = %q", c.BackendHost)
	}
	if c.Hostname != "host-from-flag" {
		t.Errorf("flag did not override env: Hostname = %q", c.Hostname)
	}
}

func TestLoadConfig_InvalidPortEnv(t *testing.T) {
	env := envFromMap(map[string]string{"SUPERVAISOR_BACKEND_PORT": "not-a-number"})
	if _, err := loadConfig(nil, env); err == nil {
		t.Fatal("expected error for invalid port")
	}
}

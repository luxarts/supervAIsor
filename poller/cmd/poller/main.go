package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/luxarts/supervaisor-poller/internal/deleter"
	"github.com/luxarts/supervaisor-poller/internal/hostname"
	"github.com/luxarts/supervaisor-poller/internal/offsets"
	"github.com/luxarts/supervaisor-poller/internal/scanner"
	"github.com/luxarts/supervaisor-poller/internal/wsclient"
)

type Config struct {
	ProjectsDir string
	StateFile   string
	BackendHost string
	BackendPort int
	BackendURL  string
	Hostname    string
	Interval    time.Duration
}

// loadConfig resolves configuration in precedence order: flag > env > default.
// Env vars are prefixed with SUPERVAISOR_, e.g. SUPERVAISOR_BACKEND_HOST.
func loadConfig(args []string, getenv func(string) string) (Config, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	home, _ := os.UserHomeDir()
	c := Config{
		ProjectsDir: filepath.Join(home, ".claude", "projects"),
		StateFile:   filepath.Join(home, ".supervAIsor", "poller-state.json"),
		BackendHost: "localhost",
		BackendPort: 8080,
		Interval:    time.Second,
	}

	if v := getenv("SUPERVAISOR_PROJECTS_DIR"); v != "" {
		c.ProjectsDir = v
	}
	if v := getenv("SUPERVAISOR_STATE_FILE"); v != "" {
		c.StateFile = v
	}
	if v := getenv("SUPERVAISOR_BACKEND_HOST"); v != "" {
		c.BackendHost = v
	}
	if v := getenv("SUPERVAISOR_BACKEND_PORT"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return c, fmt.Errorf("SUPERVAISOR_BACKEND_PORT: %w", err)
		}
		c.BackendPort = n
	}
	if v := getenv("SUPERVAISOR_BACKEND_URL"); v != "" {
		c.BackendURL = v
	}
	if v := getenv("SUPERVAISOR_HOSTNAME"); v != "" {
		c.Hostname = v
	}
	if v := getenv("SUPERVAISOR_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return c, fmt.Errorf("SUPERVAISOR_INTERVAL: %w", err)
		}
		c.Interval = d
	}

	fs := flag.NewFlagSet("poller", flag.ContinueOnError)
	fs.StringVar(&c.ProjectsDir, "projects-dir", c.ProjectsDir, "Claude projects dir (env: SUPERVAISOR_PROJECTS_DIR)")
	fs.StringVar(&c.StateFile, "state-file", c.StateFile, "Offset state file (env: SUPERVAISOR_STATE_FILE)")
	fs.StringVar(&c.BackendHost, "backend-host", c.BackendHost, "Backend host (env: SUPERVAISOR_BACKEND_HOST)")
	fs.IntVar(&c.BackendPort, "backend-port", c.BackendPort, "Backend port (env: SUPERVAISOR_BACKEND_PORT)")
	fs.StringVar(&c.BackendURL, "backend", c.BackendURL, "Backend WS URL, overrides host+port (env: SUPERVAISOR_BACKEND_URL)")
	fs.StringVar(&c.Hostname, "hostname", c.Hostname, "Hostname tag, default: OS hostname with .local stripped (env: SUPERVAISOR_HOSTNAME)")
	fs.DurationVar(&c.Interval, "interval", c.Interval, "Poll interval (env: SUPERVAISOR_INTERVAL)")
	if err := fs.Parse(args); err != nil {
		return c, err
	}
	return c, nil
}

func resolveBackendURL(c Config) string {
	if c.BackendURL != "" {
		return c.BackendURL
	}
	return fmt.Sprintf("ws://%s:%d/ws/ingest", c.BackendHost, c.BackendPort)
}

func resolveHostname(c Config) (string, error) {
	if c.Hostname != "" {
		return c.Hostname, nil
	}
	h, err := hostname.Resolve()
	if err != nil {
		return "", fmt.Errorf("resolve hostname: %w", err)
	}
	return h, nil
}

func main() {
	cfg, err := loadConfig(os.Args[1:], os.Getenv)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	host, err := resolveHostname(cfg)
	if err != nil {
		log.Fatalf("%v", err)
	}
	if host == "" {
		log.Fatal(errors.New("hostname is empty"))
	}
	url := resolveBackendURL(cfg)
	log.Printf("poller: host=%s backend=%s projects=%s", host, url, cfg.ProjectsDir)

	off, err := offsets.Load(cfg.StateFile)
	if err != nil {
		log.Fatalf("load offsets: %v", err)
	}
	cli := wsclient.New(url)
	del := deleter.New(cfg.ProjectsDir, cfg.StateFile, off)
	cli.OnCommand = func(cmd wsclient.Command) error {
		switch cmd.Type {
		case "delete":
			return del.Delete(context.Background(), cmd.SessionID, cmd.ProjectDir)
		default:
			return nil
		}
	}

	stop := make(chan struct{})
	// Re-launch Run on every reconnect: ReadMessage exits permanently when
	// the underlying conn drops, so we need to relaunch it after the scanner
	// re-establishes the connection. The loop polls for a live conn cheaply
	// and exits when stop is closed.
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			cli.Run(stop)
			select {
			case <-stop:
				return
			case <-time.After(time.Second):
			}
		}
	}()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig
		log.Println("shutting down")
		close(stop)
		cli.Close()
	}()

	s := &scanner.Scanner{
		Hostname:    host,
		ProjectsDir: cfg.ProjectsDir,
		Offsets:     off,
		OffsetsPath: cfg.StateFile,
		Client:      cli,
		Interval:    cfg.Interval,
	}
	s.Run(stop)
}

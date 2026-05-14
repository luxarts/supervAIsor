package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

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

func loadConfig(args []string) (Config, error) {
	home, _ := os.UserHomeDir()
	c := Config{
		ProjectsDir: filepath.Join(home, ".claude", "projects"),
		StateFile:   filepath.Join(home, ".supervAIsor", "poller-state.json"),
		BackendHost: "localhost",
		BackendPort: 8080,
		Interval:    time.Second,
	}
	fs := flag.NewFlagSet("poller", flag.ContinueOnError)
	fs.StringVar(&c.ProjectsDir, "projects-dir", c.ProjectsDir, "Claude projects dir")
	fs.StringVar(&c.StateFile, "state-file", c.StateFile, "Offset state file")
	fs.StringVar(&c.BackendHost, "backend-host", c.BackendHost, "Backend host")
	fs.IntVar(&c.BackendPort, "backend-port", c.BackendPort, "Backend port")
	fs.StringVar(&c.BackendURL, "backend", "", "Backend WS URL (overrides host+port)")
	fs.StringVar(&c.Hostname, "hostname", "", "Hostname tag (default: OS hostname, .local stripped)")
	fs.DurationVar(&c.Interval, "interval", c.Interval, "Poll interval")
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
	cfg, err := loadConfig(os.Args[1:])
	if err != nil {
		log.Fatalf("flag parse: %v", err)
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

	stop := make(chan struct{})
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

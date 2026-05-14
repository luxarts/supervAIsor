package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/luxarts/supervaisor-poller/internal/offsets"
	"github.com/luxarts/supervaisor-poller/internal/scanner"
	"github.com/luxarts/supervaisor-poller/internal/wsclient"
)

// Config holds runtime configuration for the poller.
type Config struct {
	ProjectsDir string
	StateFile   string
	BackendURL  string
	Interval    time.Duration
}

func loadConfig() Config {
	home, _ := os.UserHomeDir()
	c := Config{
		ProjectsDir: filepath.Join(home, ".claude", "projects"),
		StateFile:   filepath.Join(home, ".supervAIsor", "poller-state.json"),
		BackendURL:  "ws://localhost:8080/ws/ingest",
		Interval:    time.Second,
	}
	flag.StringVar(&c.ProjectsDir, "projects-dir", c.ProjectsDir, "Claude projects dir")
	flag.StringVar(&c.StateFile, "state-file", c.StateFile, "Offset state file")
	flag.StringVar(&c.BackendURL, "backend", c.BackendURL, "Backend WS URL")
	flag.DurationVar(&c.Interval, "interval", c.Interval, "Poll interval")
	flag.Parse()
	return c
}

func main() {
	cfg := loadConfig()
	log.Printf("poller: %+v", cfg)

	off, err := offsets.Load(cfg.StateFile)
	if err != nil {
		log.Fatalf("load offsets: %v", err)
	}
	cli := wsclient.New(cfg.BackendURL)

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
		ProjectsDir: cfg.ProjectsDir,
		Offsets:     off,
		OffsetsPath: cfg.StateFile,
		Client:      cli,
		Interval:    cfg.Interval,
	}
	s.Run(stop)
}

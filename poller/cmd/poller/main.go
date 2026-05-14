package main

import (
	"flag"
	"log"
	"os"
	"path/filepath"
)

type Config struct {
	ProjectsDir string
	StateFile   string
	BackendURL  string
}

func loadConfig() Config {
	home, _ := os.UserHomeDir()
	c := Config{
		ProjectsDir: filepath.Join(home, ".claude", "projects"),
		StateFile:   filepath.Join(home, ".supervAIsor", "poller-state.json"),
		BackendURL:  "ws://localhost:8080/ws/ingest",
	}
	flag.StringVar(&c.ProjectsDir, "projects-dir", c.ProjectsDir, "Claude projects dir")
	flag.StringVar(&c.StateFile, "state-file", c.StateFile, "Offset state file")
	flag.StringVar(&c.BackendURL, "backend", c.BackendURL, "Backend WS URL")
	flag.Parse()
	return c
}

func main() {
	cfg := loadConfig()
	log.Printf("poller config: %+v", cfg)
}

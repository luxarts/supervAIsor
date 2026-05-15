package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/luxarts/supervaisor-poller/internal/deleter"
	"github.com/luxarts/supervaisor-poller/internal/hostname"
	"github.com/luxarts/supervaisor-poller/internal/offsets"
	"github.com/luxarts/supervaisor-poller/internal/scanner"
	"github.com/luxarts/supervaisor-poller/internal/wsclient"
)

// resolveBackendURL turns the user-facing "host:port/path" form into a full
// WS URL. The scheme is fixed (ws://) and "/ws/ingest" is appended if the
// configured value doesn't already end with it, so both "localhost:8080" and
// "dashboard.local/supervaisor" yield the right endpoint.
func resolveBackendURL(c Config) string {
	b := strings.TrimSuffix(c.Backend, "/")
	if !strings.HasSuffix(b, "/ws/ingest") {
		b += "/ws/ingest"
	}
	return "ws://" + b
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
	home, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("resolve home dir: %v", err)
	}
	if dispatch(os.Args[1:], home) {
		return
	}
	cfg, err := loadConfig(home)
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
	// Announce ourselves on every (re)connect so the backend marks the host
	// online without waiting for the first JSONL event to flow.
	cli.OnConnect = func() {
		if err := cli.Send(map[string]any{"type": "hello", "hostname": host}); err != nil {
			log.Printf("hello: %v", err)
			return
		}
		log.Printf("hello sent host=%s", host)
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

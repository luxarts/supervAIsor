package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/luxarts/supervaisor/internal/api"
	"github.com/luxarts/supervaisor/internal/broadcast"
	"github.com/luxarts/supervaisor/internal/ingest"
	"github.com/luxarts/supervaisor/internal/state"
	"github.com/luxarts/supervaisor/internal/store"
)

func main() {
	port := envDefault("PORT", "8080")
	dbPath := envDefault("DB_PATH", "/var/lib/supervaisor/data.db")

	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		log.Fatalf("mkdir db dir: %v", err)
	}

	db, err := store.Open(dbPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	hub := broadcast.NewHub()
	go hub.Run()
	defer hub.Stop()

	registry := ingest.NewRegistry()
	coord := ingest.NewDeleteCoordinator(10 * time.Second)
	defer coord.Close()

	ingestH := &ingest.Handler{Store: db, Hub: hub, Registry: registry, Coordinator: coord}
	clientsH := &broadcast.Handler{Hub: hub, Snapshot: snapshotProvider{db: db, registry: registry}}
	apiH := &api.Handler{Store: db, Sender: registry, Coordinator: coord, Hub: hub}

	r := gin.Default()
	r.Use(corsMiddleware())
	apiH.Register(r)
	r.GET("/ws/ingest", gin.WrapF(ingestH.Serve))
	r.GET("/ws/clients", gin.WrapF(clientsH.Serve))

	go runStatusTicker(db, hub)
	go runPollersBroadcaster(registry, hub)

	log.Printf("supervAIsor backend on :%s, db=%s", port, dbPath)
	if err := r.Run(":" + port); err != nil {
		log.Fatal(err)
	}
}

type snapshotProvider struct {
	db       *store.SQLite
	registry *ingest.Registry
}

func (s snapshotProvider) Snapshot() []*state.Session {
	sess, _ := s.db.ListSessions(context.Background())
	if sess == nil {
		return []*state.Session{}
	}
	return sess
}

func (s snapshotProvider) PollersOnline() map[string]bool {
	if s.registry == nil {
		return map[string]bool{}
	}
	return s.registry.Online()
}

func runStatusTicker(db *store.SQLite, hub *broadcast.Hub) {
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for now := range tick.C {
		sessions, err := db.ListSessions(context.Background())
		if err != nil {
			log.Printf("status tick list: %v", err)
			continue
		}
		for _, s := range sessions {
			prev := s.Status
			state.RecomputeStatus(s, now.UTC())
			if s.Status != prev {
				if err := db.UpsertSession(context.Background(), s); err != nil {
					log.Printf("status tick upsert: %v", err)
					continue
				}
				if b, err := json.Marshal(map[string]any{"kind": "update", "session": s}); err == nil {
					hub.Broadcast(b)
				}
			}
		}
	}
}

// runPollersBroadcaster fans poller liveness transitions out to all
// connected dashboard clients.
func runPollersBroadcaster(registry *ingest.Registry, hub *broadcast.Hub) {
	sub, _ := registry.Subscribe()
	for snap := range sub {
		if b, err := json.Marshal(map[string]any{"kind": "pollers", "online": snap}); err == nil {
			hub.Broadcast(b)
		}
	}
}

func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

func envDefault(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

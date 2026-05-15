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

	ingestH := &ingest.Handler{Store: db, Hub: hub}
	clientsH := &broadcast.Handler{Hub: hub, Snapshot: snapshotProvider{db}}
	apiH := &api.Handler{Store: db}

	r := gin.Default()
	r.Use(corsMiddleware())
	apiH.Register(r)
	r.GET("/ws/ingest", gin.WrapF(ingestH.Serve))
	r.GET("/ws/clients", gin.WrapF(clientsH.Serve))

	// Periodic status recompute (DONE/STALE transitions).
	go runStatusTicker(db, hub)

	log.Printf("supervAIsor backend on :%s, db=%s", port, dbPath)
	if err := r.Run(":" + port); err != nil {
		log.Fatal(err)
	}
}

// snapshotProvider wraps *store.SQLite and satisfies broadcast.SnapshotProvider.
type snapshotProvider struct{ db *store.SQLite }

func (s snapshotProvider) Snapshot() []*state.Session {
	sess, _ := s.db.ListSessions(context.Background())
	if sess == nil {
		return []*state.Session{}
	}
	return sess
}

// runStatusTicker runs every 5 seconds, recomputes time-based session statuses,
// persists any that changed, and broadcasts updates.
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

func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
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

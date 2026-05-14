package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/luxarts/supervaisor/internal/state"
	"github.com/luxarts/supervaisor/internal/store"
)

func TestGetSessions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, _ := store.Open(filepath.Join(t.TempDir(), "a.db"))
	defer db.Close()

	now := time.Now().UTC().Truncate(time.Second)
	_ = db.UpsertSession(context.Background(), &state.Session{
		ID: "s1", Name: "n", Project: "/tmp",
		Status: state.StatusIdle, StartedAt: now, LastEventAt: now,
	})

	h := &Handler{Store: db}
	r := gin.New()
	h.Register(r)

	req := httptest.NewRequest("GET", "/sessions", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, body = %s", w.Code, w.Body.String())
	}
	var out []state.Session
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].ID != "s1" {
		t.Errorf("got %#v", out)
	}
}

func TestGetHealthz(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, _ := store.Open(filepath.Join(t.TempDir(), "b.db"))
	defer db.Close()

	h := &Handler{Store: db}
	r := gin.New()
	h.Register(r)

	req := httptest.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, body = %s", w.Code, w.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out["ok"] != true {
		t.Errorf("expected ok=true, got %v", out)
	}
}

func TestGetSessions_Empty(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, _ := store.Open(filepath.Join(t.TempDir(), "c.db"))
	defer db.Close()

	h := &Handler{Store: db}
	r := gin.New()
	h.Register(r)

	req := httptest.NewRequest("GET", "/sessions", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, body = %s", w.Code, w.Body.String())
	}
	// Must return JSON array, not null.
	if w.Body.String() != "[]\n" && w.Body.String() != "[]" {
		t.Errorf("expected empty array, got %q", w.Body.String())
	}
}

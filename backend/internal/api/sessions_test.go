package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
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
		ID: "s1", Hostname: "host-test", Name: "n", Project: "/tmp",
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

func TestGetSessionEvents(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	sess := &state.Session{
		ID:            "abc",
		Hostname:      "host-test",
		Name:          "s",
		Project:       "/tmp/p",
		Status:        "idle",
		StartedAt:     now,
		LastEventAt:   now,
		CurrentAction: "",
	}
	if err := st.UpsertSession(ctx, sess); err != nil {
		t.Fatal(err)
	}
	if err := st.AppendEvent(ctx, "host-test", "abc", now, "user", []byte(`{"text":"hi"}`)); err != nil {
		t.Fatal(err)
	}

	r := gin.New()
	(&Handler{Store: st}).Register(r)

	req := httptest.NewRequest("GET", "/sessions/host-test/abc/events", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"hi"`) {
		t.Fatalf("body missing event payload: %s", w.Body.String())
	}

	req2 := httptest.NewRequest("GET", "/sessions/host-test/does-not-exist/events", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w2.Code)
	}
}

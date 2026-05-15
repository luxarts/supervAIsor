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

	"github.com/luxarts/supervaisor/internal/broadcast"
	"github.com/luxarts/supervaisor/internal/ingest"
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
		Status: state.StatusDone, StartedAt: now, LastEventAt: now,
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
		Status:        state.StatusDone,
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

func TestGetSessionStats(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "stats.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ctx := context.Background()
	t0 := time.Now().UTC().Truncate(time.Second)
	sess := &state.Session{
		ID:          "abc",
		Hostname:    "host-test",
		Name:        "n",
		Project:     "/tmp/p",
		Status:      state.StatusDone,
		StartedAt:   t0,
		LastEventAt: t0.Add(60 * time.Second),
	}
	if err := st.UpsertSession(ctx, sess); err != nil {
		t.Fatal(err)
	}
	assistantPayload := []byte(`{
		"type": "assistant",
		"message": {
			"role": "assistant",
			"model": "claude-opus-4-7",
			"usage": {"input_tokens": 5, "output_tokens": 7},
			"content": [{"type": "tool_use", "id": "x", "name": "Bash"}]
		}
	}`)
	if err := st.AppendEvent(ctx, "host-test", "abc", t0, "assistant", assistantPayload); err != nil {
		t.Fatal(err)
	}

	r := gin.New()
	(&Handler{Store: st}).Register(r)

	req := httptest.NewRequest("GET", "/sessions/host-test/abc/stats", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var out state.Stats
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Model != "claude-opus-4-7" {
		t.Errorf("Model = %q", out.Model)
	}
	if out.Tokens.Input != 5 || out.Tokens.Output != 7 {
		t.Errorf("Tokens = %+v", out.Tokens)
	}
	if out.Counts.AssistantTurns != 1 || out.Counts.ToolCalls != 1 {
		t.Errorf("Counts = %+v", out.Counts)
	}
	if out.ToolBreakdown["Bash"] != 1 {
		t.Errorf("ToolBreakdown = %+v", out.ToolBreakdown)
	}
	if out.WallClockSeconds != 60 {
		t.Errorf("WallClockSeconds = %d, want 60", out.WallClockSeconds)
	}
}

func TestGetSessionStats_NotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	st, _ := store.Open(filepath.Join(t.TempDir(), "x.db"))
	defer st.Close()
	r := gin.New()
	(&Handler{Store: st}).Register(r)

	req := httptest.NewRequest("GET", "/sessions/h/missing/stats", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

type fakeSender struct {
	online   map[string]bool
	last     []byte
	lastHost string
}

func (f *fakeSender) IsOnline(h string) bool { return f.online[h] }
func (f *fakeSender) Send(h string, b []byte) error {
	if !f.online[h] {
		return ingest.ErrNoPoller
	}
	f.lastHost = h
	f.last = b
	return nil
}

func TestDeleteSession_NotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	st, _ := store.Open(filepath.Join(t.TempDir(), "del.db"))
	defer st.Close()
	r := gin.New()
	(&Handler{
		Store:       st,
		Sender:      &fakeSender{online: map[string]bool{}},
		Coordinator: ingest.NewDeleteCoordinator(time.Second),
	}).Register(r)

	req := httptest.NewRequest("DELETE", "/sessions/h/missing", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", w.Code)
	}
}

func TestDeleteSession_PollerOffline(t *testing.T) {
	gin.SetMode(gin.TestMode)
	st, _ := store.Open(filepath.Join(t.TempDir(), "off.db"))
	defer st.Close()
	ctx := context.Background()
	t0 := time.Now().UTC().Truncate(time.Second)
	_ = st.UpsertSession(ctx, &state.Session{
		ID: "abc", Hostname: "h", Name: "n", Project: "/p",
		Status: state.StatusDone, StartedAt: t0, LastEventAt: t0,
		ProjectDirEncoded: "-x",
	})

	r := gin.New()
	(&Handler{
		Store:       st,
		Sender:      &fakeSender{online: map[string]bool{}},
		Coordinator: ingest.NewDeleteCoordinator(time.Second),
	}).Register(r)

	req := httptest.NewRequest("DELETE", "/sessions/h/abc", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("want 409, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestDeleteSession_HappyPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	st, _ := store.Open(filepath.Join(t.TempDir(), "ok.db"))
	defer st.Close()
	ctx := context.Background()
	t0 := time.Now().UTC().Truncate(time.Second)
	_ = st.UpsertSession(ctx, &state.Session{
		ID: "abc", Hostname: "h", Name: "n", Project: "/p",
		Status: state.StatusDone, StartedAt: t0, LastEventAt: t0,
		ProjectDirEncoded: "-x",
	})

	coord := ingest.NewDeleteCoordinator(2 * time.Second)
	defer coord.Close()
	hub := broadcast.NewHub()
	go hub.Run()
	defer hub.Stop()

	sender := &fakeSender{online: map[string]bool{"h": true}}

	r := gin.New()
	(&Handler{Store: st, Sender: sender, Coordinator: coord, Hub: hub}).Register(r)

	respCh := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		req := httptest.NewRequest("DELETE", "/sessions/h/abc", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		respCh <- w
	}()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if sender.last != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if sender.last == nil {
		t.Fatal("handler never dispatched delete command")
	}

	var sent map[string]any
	_ = json.Unmarshal(sender.last, &sent)
	reqID, _ := sent["request_id"].(string)
	if reqID == "" {
		t.Fatal("dispatched command missing request_id")
	}
	coord.Resolve(reqID, true, "")

	w := <-respCh
	if w.Code != http.StatusNoContent {
		t.Fatalf("want 204, got %d body=%s", w.Code, w.Body.String())
	}

	got, _ := st.GetSession(ctx, "h", "abc")
	if got != nil {
		t.Errorf("session not purged: %+v", got)
	}
}

func TestDeleteSession_PollerErrorAck(t *testing.T) {
	gin.SetMode(gin.TestMode)
	st, _ := store.Open(filepath.Join(t.TempDir(), "ack.db"))
	defer st.Close()
	ctx := context.Background()
	t0 := time.Now().UTC().Truncate(time.Second)
	_ = st.UpsertSession(ctx, &state.Session{
		ID: "abc", Hostname: "h", Name: "n", Project: "/p",
		Status: state.StatusDone, StartedAt: t0, LastEventAt: t0,
		ProjectDirEncoded: "-x",
	})

	coord := ingest.NewDeleteCoordinator(2 * time.Second)
	defer coord.Close()
	hub := broadcast.NewHub()
	go hub.Run()
	defer hub.Stop()

	sender := &fakeSender{online: map[string]bool{"h": true}}
	r := gin.New()
	(&Handler{Store: st, Sender: sender, Coordinator: coord, Hub: hub}).Register(r)

	respCh := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		req := httptest.NewRequest("DELETE", "/sessions/h/abc", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		respCh <- w
	}()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if sender.last != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	var sent map[string]any
	_ = json.Unmarshal(sender.last, &sent)
	reqID, _ := sent["request_id"].(string)
	coord.Resolve(reqID, false, "boom")
	w := <-respCh
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d", w.Code)
	}
	got, _ := st.GetSession(ctx, "h", "abc")
	if got == nil {
		t.Fatal("session should still exist after error ack")
	}
}

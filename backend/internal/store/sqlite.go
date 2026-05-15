package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/luxarts/supervaisor/internal/events"
	"github.com/luxarts/supervaisor/internal/state"
)

// SQLite is a SQLite-backed repository for sessions and events.
type SQLite struct {
	db *sql.DB
}

// Open opens (or creates) a SQLite database at path and applies the schema.
func Open(path string) (*SQLite, error) {
	// Enable WAL and a busy timeout so concurrent writers (multiple pollers)
	// do not immediately fail with SQLITE_BUSY.
	dsn := path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// Serialize writes through a single connection — modernc.org/sqlite uses
	// per-connection handles, and SQLite only allows one writer at a time.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("init schema: %w", err)
	}
	for _, m := range migrations {
		if _, err := db.Exec(m); err != nil {
			if !strings.Contains(err.Error(), "duplicate column name") {
				return nil, fmt.Errorf("migrate: %w", err)
			}
		}
	}
	return &SQLite{db: db}, nil
}

// Close closes the underlying database connection.
func (s *SQLite) Close() error { return s.db.Close() }

// CountEvents returns the total number of rows in the events table.
// Intended for tests and diagnostics.
func (s *SQLite) CountEvents(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM events").Scan(&n)
	return n, err
}

const schema = `
CREATE TABLE IF NOT EXISTS sessions (
  hostname        TEXT NOT NULL,
  id              TEXT NOT NULL,
  name            TEXT NOT NULL,
  project         TEXT NOT NULL,
  status          TEXT NOT NULL,
  started_at      TIMESTAMP NOT NULL,
  last_prompt_at  TIMESTAMP,
  current_action  TEXT,
  last_event_at   TIMESTAMP NOT NULL,
  last_error_at   TIMESTAMP,
  project_dir_encoded TEXT,
  PRIMARY KEY (hostname, id)
);
CREATE TABLE IF NOT EXISTS events (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  hostname    TEXT NOT NULL DEFAULT '',
  session_id  TEXT NOT NULL,
  ts          TIMESTAMP NOT NULL,
  type        TEXT NOT NULL,
  payload     TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_events_session_ts ON events(hostname, session_id, ts);
`

// migrations contains idempotent ALTER TABLE statements that bring older
// databases up to the current schema. SQLite errors on duplicate columns,
// so we tolerate the failure for the additive case.
var migrations = []string{
	`ALTER TABLE sessions ADD COLUMN last_error_at TIMESTAMP`,
	`ALTER TABLE sessions ADD COLUMN project_dir_encoded TEXT`,
}

// UpsertSession inserts or updates a session record by primary key.
func (s *SQLite) UpsertSession(ctx context.Context, sess *state.Session) error {
	var lastPrompt any
	if sess.LastPromptAt != nil {
		lastPrompt = *sess.LastPromptAt
	}
	var lastError any
	if !sess.LastErrorAt.IsZero() {
		lastError = sess.LastErrorAt
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO sessions (hostname, id, name, project, status, started_at, last_prompt_at, current_action, last_event_at, last_error_at, project_dir_encoded)
VALUES (?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(hostname, id) DO UPDATE SET
  name=excluded.name,
  project=excluded.project,
  status=excluded.status,
  started_at=excluded.started_at,
  last_prompt_at=excluded.last_prompt_at,
  current_action=excluded.current_action,
  last_event_at=excluded.last_event_at,
  last_error_at=excluded.last_error_at,
  project_dir_encoded=excluded.project_dir_encoded
`,
		sess.Hostname, sess.ID, sess.Name, sess.Project, string(sess.Status),
		sess.StartedAt, lastPrompt, sess.CurrentAction, sess.LastEventAt, lastError,
		sess.ProjectDirEncoded,
	)
	return err
}

// ListSessions returns all sessions ordered by last_event_at descending.
func (s *SQLite) ListSessions(ctx context.Context) ([]*state.Session, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT hostname, id, name, project, status, started_at, last_prompt_at, current_action, last_event_at, last_error_at, project_dir_encoded
FROM sessions ORDER BY last_event_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*state.Session
	for rows.Next() {
		var (
			sess   state.Session
			status string
			lp     sql.NullTime
			le     sql.NullTime
			pde    sql.NullString
		)
		if err := rows.Scan(
			&sess.Hostname, &sess.ID, &sess.Name, &sess.Project, &status,
			&sess.StartedAt, &lp, &sess.CurrentAction, &sess.LastEventAt, &le, &pde,
		); err != nil {
			return nil, err
		}
		sess.Status = state.Status(status)
		if lp.Valid {
			t := lp.Time
			sess.LastPromptAt = &t
		}
		if le.Valid {
			sess.LastErrorAt = le.Time
		}
		if pde.Valid {
			sess.ProjectDirEncoded = pde.String
		}
		out = append(out, &sess)
	}
	return out, rows.Err()
}

// GetSession returns a session by (hostname, id), or nil if not found.
func (s *SQLite) GetSession(ctx context.Context, hostname, id string) (*state.Session, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT hostname, id, name, project, status, started_at, last_prompt_at, current_action, last_event_at, last_error_at, project_dir_encoded
FROM sessions WHERE hostname = ? AND id = ?`, hostname, id)
	var (
		sess   state.Session
		status string
		lp     sql.NullTime
		le     sql.NullTime
		pde    sql.NullString
	)
	err := row.Scan(&sess.Hostname, &sess.ID, &sess.Name, &sess.Project, &status,
		&sess.StartedAt, &lp, &sess.CurrentAction, &sess.LastEventAt, &le, &pde)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	sess.Status = state.Status(status)
	if lp.Valid {
		t := lp.Time
		sess.LastPromptAt = &t
	}
	if le.Valid {
		sess.LastErrorAt = le.Time
	}
	if pde.Valid {
		sess.ProjectDirEncoded = pde.String
	}
	return &sess, nil
}

// AppendEvent stores a raw JSONL event line for a session.
func (s *SQLite) AppendEvent(ctx context.Context, hostname, sessionID string, ts time.Time, eventType string, payload []byte) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO events (hostname, session_id, ts, type, payload) VALUES (?,?,?,?,?)`,
		hostname, sessionID, ts, eventType, string(payload),
	)
	return err
}

// Event is a type alias for events.Event, kept for backward compatibility.
// New code should use events.Event directly.
type Event = events.Event

// DeleteSession removes a session row and all its events in a single
// transaction. No-op when the session does not exist.
func (s *SQLite) DeleteSession(ctx context.Context, hostname, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM events WHERE hostname = ? AND session_id = ?`, hostname, id); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE hostname = ? AND id = ?`, hostname, id); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// ListEvents returns the most recent events for a session, in chronological
// (ts ASC) order, capped at limit. When the session has more events than
// limit, the older ones are dropped so the modal always shows the tail of
// the conversation.
func (s *SQLite) ListEvents(ctx context.Context, hostname, sessionID string, limit int) ([]events.Event, error) {
	// limit == 0 → use sane default; limit < 0 → unlimited (SQLite LIMIT -1).
	if limit == 0 {
		limit = 500
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT ts, type, payload FROM events WHERE hostname = ? AND session_id = ? ORDER BY ts DESC LIMIT ?`,
		hostname, sessionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []events.Event
	for rows.Next() {
		var (
			ts      time.Time
			typ     string
			payload string
		)
		if err := rows.Scan(&ts, &typ, &payload); err != nil {
			return nil, err
		}
		out = append(out, events.Event{TS: ts, Type: typ, Payload: json.RawMessage(payload)})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Reverse to ASC for the caller.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

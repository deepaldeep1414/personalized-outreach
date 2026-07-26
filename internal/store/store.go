package store

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

const schema = `
PRAGMA journal_mode=WAL;
PRAGMA foreign_keys=ON;

CREATE TABLE IF NOT EXISTS runs (
    id            TEXT    PRIMARY KEY,
    name          TEXT    NOT NULL,
    title         TEXT    NOT NULL,
    company       TEXT    NOT NULL,
    linkedin_url  TEXT    NOT NULL DEFAULT '',
    notes         TEXT    NOT NULL DEFAULT '',
    status        TEXT    NOT NULL DEFAULT 'running',
    hook_type     TEXT    NOT NULL DEFAULT '',
    hook_title    TEXT    NOT NULL DEFAULT '',
    draft_subject TEXT    NOT NULL DEFAULT '',
    draft_body    TEXT    NOT NULL DEFAULT '',
    review_status TEXT    NOT NULL DEFAULT 'pending',
    started_at    INTEGER NOT NULL,
    finished_at   INTEGER
);

CREATE TABLE IF NOT EXISTS run_stages (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id       TEXT    NOT NULL REFERENCES runs(id),
    stage_name   TEXT    NOT NULL,
    stage_index  INTEGER NOT NULL,
    status       TEXT    NOT NULL,
    output_json  TEXT    NOT NULL DEFAULT '{}',
    reasoning    TEXT    NOT NULL DEFAULT '',
    duration_ms  INTEGER NOT NULL DEFAULT 0,
    error_msg    TEXT    NOT NULL DEFAULT '',
    completed_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_run_stages_run_id ON run_stages(run_id);
`

// Store is the SQLite-backed persistence layer.
type Store struct {
	db *sql.DB
}

// Open opens (or creates) the SQLite database at path and applies the schema.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}
	db.SetMaxOpenConns(1) // SQLite is single-writer

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("applying schema: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// ── Domain types ─────────────────────────────────────────────────────────────

// Run represents one complete pipeline execution.
type Run struct {
	ID           string
	Name         string
	Title        string
	Company      string
	LinkedInURL  string
	Notes        string
	Status       string     // running | completed | failed
	HookType     string
	HookTitle    string
	DraftSubject string
	DraftBody    string
	ReviewStatus string     // pending | approved | discarded
	StartedAt    time.Time
	FinishedAt   *time.Time
}

// RunStage is one named stage within a run.
type RunStage struct {
	ID          int64
	RunID       string
	StageName   string
	StageIndex  int
	Status      string
	OutputJSON  string
	Reasoning   string
	DurationMs  int64
	ErrorMsg    string
	CompletedAt time.Time
}

// ── Write operations ─────────────────────────────────────────────────────────

func (s *Store) CreateRun(r Run) error {
	_, err := s.db.Exec(
		`INSERT INTO runs (id, name, title, company, linkedin_url, notes, status, started_at)
         VALUES (?, ?, ?, ?, ?, ?, 'running', ?)`,
		r.ID, r.Name, r.Title, r.Company, r.LinkedInURL, r.Notes,
		r.StartedAt.UnixMilli(),
	)
	return err
}

func (s *Store) InsertStage(st RunStage) error {
	_, err := s.db.Exec(
		`INSERT INTO run_stages
         (run_id, stage_name, stage_index, status, output_json, reasoning, duration_ms, error_msg, completed_at)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		st.RunID, st.StageName, st.StageIndex, st.Status,
		st.OutputJSON, st.Reasoning, st.DurationMs, st.ErrorMsg,
		st.CompletedAt.UnixMilli(),
	)
	return err
}

func (s *Store) CompleteRun(id, status, hookType, hookTitle, draftSubject, draftBody string) error {
	_, err := s.db.Exec(
		`UPDATE runs
         SET status=?, hook_type=?, hook_title=?, draft_subject=?, draft_body=?, finished_at=?
         WHERE id=?`,
		status, hookType, hookTitle, draftSubject, draftBody,
		time.Now().UnixMilli(), id,
	)
	return err
}

func (s *Store) UpdateReview(id, reviewStatus string) error {
	_, err := s.db.Exec(`UPDATE runs SET review_status=? WHERE id=?`, reviewStatus, id)
	return err
}

// ── Read operations ───────────────────────────────────────────────────────────

func (s *Store) GetRun(id string) (*Run, error) {
	return scanRun(s.db.QueryRow(
		`SELECT id, name, title, company, linkedin_url, notes, status,
                hook_type, hook_title, draft_subject, draft_body, review_status,
                started_at, finished_at
         FROM runs WHERE id=?`, id,
	))
}

func (s *Store) ListRuns() ([]Run, error) {
	rows, err := s.db.Query(
		`SELECT id, name, title, company, linkedin_url, notes, status,
                hook_type, hook_title, draft_subject, draft_body, review_status,
                started_at, finished_at
         FROM runs ORDER BY started_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Run
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

func (s *Store) GetRunStages(runID string) ([]RunStage, error) {
	rows, err := s.db.Query(
		`SELECT id, run_id, stage_name, stage_index, status, output_json,
                reasoning, duration_ms, error_msg, completed_at
         FROM run_stages WHERE run_id=? ORDER BY stage_index`,
		runID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []RunStage
	for rows.Next() {
		var st RunStage
		var completedAtMs int64
		if err := rows.Scan(
			&st.ID, &st.RunID, &st.StageName, &st.StageIndex,
			&st.Status, &st.OutputJSON, &st.Reasoning,
			&st.DurationMs, &st.ErrorMsg, &completedAtMs,
		); err != nil {
			return nil, err
		}
		st.CompletedAt = time.UnixMilli(completedAtMs)
		out = append(out, st)
	}
	return out, rows.Err()
}

// ── Scanner helper ────────────────────────────────────────────────────────────

type dbScanner interface {
	Scan(dest ...any) error
}

func scanRun(sc dbScanner) (*Run, error) {
	var r Run
	var startedAtMs int64
	var finishedAtMs sql.NullInt64

	err := sc.Scan(
		&r.ID, &r.Name, &r.Title, &r.Company, &r.LinkedInURL, &r.Notes,
		&r.Status, &r.HookType, &r.HookTitle, &r.DraftSubject, &r.DraftBody,
		&r.ReviewStatus, &startedAtMs, &finishedAtMs,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r.StartedAt = time.UnixMilli(startedAtMs)
	if finishedAtMs.Valid {
		t := time.UnixMilli(finishedAtMs.Int64)
		r.FinishedAt = &t
	}
	return &r, nil
}

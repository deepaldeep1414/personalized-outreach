package store_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/user/personalized-outreach/internal/store"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test_outreach.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open test store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestStore_CreateAndListRuns(t *testing.T) {
	st := newTestStore(t)

	run := store.Run{
		ID:        "run_1",
		Name:      "Alice Smith",
		Title:     "CEO",
		Company:   "Acme Corp",
		StartedAt: time.Now(),
	}

	if err := st.CreateRun(run); err != nil {
		t.Fatalf("CreateRun failed: %v", err)
	}

	got, err := st.GetRun("run_1")
	if err != nil {
		t.Fatalf("GetRun failed: %v", err)
	}
	if got == nil || got.Name != "Alice Smith" {
		t.Errorf("expected run_1 with name Alice Smith, got %+v", got)
	}

	runs, err := st.ListRuns()
	if err != nil {
		t.Fatalf("ListRuns failed: %v", err)
	}
	if len(runs) != 1 {
		t.Errorf("expected 1 run, got %d", len(runs))
	}
}

func TestStore_DeleteRun(t *testing.T) {
	st := newTestStore(t)

	run := store.Run{
		ID:        "run_to_delete",
		Name:      "Bob Jones",
		Title:     "CTO",
		Company:   "Tech Inc",
		StartedAt: time.Now(),
	}
	if err := st.CreateRun(run); err != nil {
		t.Fatalf("CreateRun failed: %v", err)
	}

	stage := store.RunStage{
		RunID:       "run_to_delete",
		StageName:   "Research",
		StageIndex:  1,
		Status:      "ok",
		OutputJSON:  "{}",
		CompletedAt: time.Now(),
	}
	if err := st.InsertStage(stage); err != nil {
		t.Fatalf("InsertStage failed: %v", err)
	}

	// Delete run
	if err := st.DeleteRun("run_to_delete"); err != nil {
		t.Fatalf("DeleteRun failed: %v", err)
	}

	// Verify run is gone
	gotRun, err := st.GetRun("run_to_delete")
	if err != nil {
		t.Fatalf("GetRun failed: %v", err)
	}
	if gotRun != nil {
		t.Errorf("expected run to be deleted, but found: %+v", gotRun)
	}

	// Verify stages are gone
	stages, err := st.GetRunStages("run_to_delete")
	if err != nil {
		t.Fatalf("GetRunStages failed: %v", err)
	}
	if len(stages) != 0 {
		t.Errorf("expected 0 stages, got %d", len(stages))
	}
}

func TestStore_DeleteAllRuns(t *testing.T) {
	st := newTestStore(t)

	for i := 1; i <= 3; i++ {
		id := string(rune('a' + i - 1))
		if err := st.CreateRun(store.Run{
			ID:        id,
			Name:      "User " + id,
			Title:     "Dev",
			Company:   "Corp",
			StartedAt: time.Now(),
		}); err != nil {
			t.Fatalf("CreateRun failed: %v", err)
		}
		if err := st.InsertStage(store.RunStage{
			RunID:       id,
			StageName:   "Research",
			StageIndex:  1,
			Status:      "ok",
			CompletedAt: time.Now(),
		}); err != nil {
			t.Fatalf("InsertStage failed: %v", err)
		}
	}

	runs, err := st.ListRuns()
	if err != nil || len(runs) != 3 {
		t.Fatalf("expected 3 runs before clear, got %d (err: %v)", len(runs), err)
	}

	if err := st.DeleteAllRuns(); err != nil {
		t.Fatalf("DeleteAllRuns failed: %v", err)
	}

	runsAfter, err := st.ListRuns()
	if err != nil {
		t.Fatalf("ListRuns after clear failed: %v", err)
	}
	if len(runsAfter) != 0 {
		t.Errorf("expected 0 runs after clear, got %d", len(runsAfter))
	}
}

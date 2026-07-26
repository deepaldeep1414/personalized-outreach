package pipeline

import (
	"context"

	"github.com/user/personalized-outreach/internal/models"
)

// StageEvent is emitted by the pipeline for each stage as it completes.
// The UI/CLI can range over Events() to print results incrementally.
type StageEvent struct {
	Index int    // 1-based stage index
	Total int    // total number of stages
	Name  string // stage name

	// Exactly one of the following is non-nil:
	Research  *models.StageResult[models.ResearchOutput]
	Signals   *models.StageResult[models.SignalsOutput]
	Hook      *models.StageResult[models.HookOutput]
	Draft     *models.StageResult[models.DraftOutput]
	Review    *models.StageResult[models.ReviewOutput]
}

// Config holds all provider implementations the pipeline will use.
type Config struct {
	Researcher     Researcher
	SignalExtractor SignalExtractor
	HookSelector   HookSelector
	Drafter        Drafter
}

// Pipeline orchestrates the five named stages in order.
type Pipeline struct {
	cfg Config
}

// New creates a Pipeline from a Config.
func New(cfg Config) *Pipeline {
	return &Pipeline{cfg: cfg}
}

// Run executes all stages sequentially for the given prospect.
// It sends a StageEvent to the returned channel after each stage completes,
// so callers can display results incrementally. The channel is closed when the
// pipeline finishes (successfully or after a hard failure).
//
// A stage with StatusFailed stops the pipeline; all subsequent stages are skipped.
func (p *Pipeline) Run(ctx context.Context, prospect models.Prospect) <-chan StageEvent {
	ch := make(chan StageEvent, 5)

	go func() {
		defer close(ch)

		const total = 5

		// ── Stage 1: Research ──────────────────────────────────────────────────
		res := runResearch(ctx, prospect, p.cfg.Researcher)
		ch <- StageEvent{Index: 1, Total: total, Name: "Research", Research: &res}
		if res.Status == models.StatusFailed {
			return
		}

		// ── Stage 2: ExtractSignals ────────────────────────────────────────────
		sig := runExtractSignals(ctx, prospect, res.Output.Snippets, p.cfg.SignalExtractor)
		ch <- StageEvent{Index: 2, Total: total, Name: "ExtractSignals", Signals: &sig}
		if sig.Status == models.StatusFailed {
			return
		}

		// ── Stage 3: SelectHook ────────────────────────────────────────────────
		hookResult := runSelectHook(ctx, prospect, sig.Output.Signals, p.cfg.HookSelector)
		ch <- StageEvent{Index: 3, Total: total, Name: "SelectHook", Hook: &hookResult}
		if hookResult.Status == models.StatusFailed {
			return
		}

		// ── Stage 4: DraftMessage ──────────────────────────────────────────────
		draftResult := runDraftMessage(ctx, prospect, hookResult.Output.Hook, p.cfg.Drafter)
		ch <- StageEvent{Index: 4, Total: total, Name: "DraftMessage", Draft: &draftResult}
		if draftResult.Status == models.StatusFailed {
			return
		}

		// ── Stage 5: AwaitReview (no-op) ──────────────────────────────────────
		reviewResult := runAwaitReview(draftResult.Output.Draft)
		ch <- StageEvent{Index: 5, Total: total, Name: "AwaitReview", Review: &reviewResult}
	}()

	return ch
}

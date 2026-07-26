package models

import "time"

// StageStatus describes the outcome of a pipeline stage.
type StageStatus string

const (
	StatusOK       StageStatus = "ok"
	StatusDegraded StageStatus = "degraded" // completed with fallback / partial data
	StatusFailed   StageStatus = "failed"   // hard error, pipeline should stop
)

// StageResult wraps the typed output of any pipeline stage with metadata.
// The generic parameter T holds the stage-specific output type.
type StageResult[T any] struct {
	Stage     string        `json:"stage"`
	Status    StageStatus   `json:"status"`
	Input     any           `json:"input"`
	Output    T             `json:"output"`
	Reasoning string        `json:"reasoning"`
	Duration  time.Duration `json:"duration_ms"` // stored as nanoseconds, format for display
	Error     string        `json:"error,omitempty"`
}

// ResearchOutput is the typed output for the Research stage.
type ResearchOutput struct {
	Snippets []string `json:"snippets"`
	Count    int      `json:"count"`
}

// EdgeCaseFlags records which edge-case detection paths fired during extraction.
// Each set field carries a short human-readable explanation for the reasoning trail.
type EdgeCaseFlags struct {
	NoFootprint         string `json:"no_footprint,omitempty"`          // empty search / no usable signals
	StaleSignalsRemoved string `json:"stale_signals_removed,omitempty"` // signals older than 24 months dropped
	CompetingSignals    string `json:"competing_signals,omitempty"`     // near-tie between two top signals
	ConflictingCompany  string `json:"conflicting_company,omitempty"`   // acquisition / rebrand detected
}

// SignalsOutput is the typed output for the ExtractSignals stage.
type SignalsOutput struct {
	Signals    []Signal      `json:"signals"`
	EdgeCases  EdgeCaseFlags `json:"edge_cases,omitempty"`
}

// HookOutput is the typed output for the SelectHook stage.
type HookOutput struct {
	Hook     Hook    `json:"hook"`
	// RunnerUp holds the second-best signal when two signals were closely matched.
	// Populated by the hook selector when the score gap is ≤ 0.15.
	// Nil when there was only one candidate or a clear winner.
	RunnerUp *Signal `json:"runner_up,omitempty"`
}

// DraftOutput is the typed output for the DraftMessage stage.
type DraftOutput struct {
	Draft OutreachDraft `json:"draft"`
}

// ReviewOutput is the typed output for the AwaitReview stage.
type ReviewOutput struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

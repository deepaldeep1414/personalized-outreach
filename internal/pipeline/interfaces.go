package pipeline

import (
	"context"

	"github.com/user/personalized-outreach/internal/models"
)

// Researcher performs web research for a prospect and returns raw text snippets.
// Each snippet should be a self-contained paragraph that can be fed to an LLM.
type Researcher interface {
	Research(ctx context.Context, prospect models.Prospect) ([]string, error)
}

// SignalExtractor analyses raw research text and extracts structured signals.
// Implementations may also surface edge-case flags (no-footprint, stale, etc.)
// via ExtractSignalsWithFlags; stages.go calls that method when available.
type SignalExtractor interface {
	ExtractSignals(ctx context.Context, prospect models.Prospect, rawResearch []string) ([]models.Signal, error)
}

// SignalExtractorFull is an optional extension of SignalExtractor that returns
// EdgeCaseFlags alongside signals. stages.go type-asserts to this when possible.
type SignalExtractorFull interface {
	SignalExtractor
	ExtractSignalsWithFlags(ctx context.Context, prospect models.Prospect, rawResearch []string) ([]models.Signal, models.EdgeCaseFlags, error)
}

// HookSelector picks the single best signal to anchor the outreach message.
type HookSelector interface {
	SelectHook(ctx context.Context, prospect models.Prospect, signals []models.Signal) (models.Hook, error)
}

// HookSelectorFull is an optional extension of HookSelector that also returns
// the runner-up signal. stages.go type-asserts to this when possible.
type HookSelectorFull interface {
	HookSelector
	SelectHookWithRunnerUp(ctx context.Context, prospect models.Prospect, signals []models.Signal) (models.Hook, *models.Signal, error)
}

// Drafter generates the final personalised cold-outreach draft from the chosen hook.
type Drafter interface {
	Draft(ctx context.Context, prospect models.Prospect, hook models.Hook) (models.OutreachDraft, error)
}

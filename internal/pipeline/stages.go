package pipeline

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/user/personalized-outreach/internal/models"
)

// ── Research ──────────────────────────────────────────────────────────────────

func runResearch(
	ctx context.Context,
	prospect models.Prospect,
	researcher Researcher,
) models.StageResult[models.ResearchOutput] {
	start := time.Now()
	result := models.StageResult[models.ResearchOutput]{
		Stage: "Research",
		Input: prospect,
	}

	snippets, err := researcher.Research(ctx, prospect)
	result.Duration = time.Since(start)

	if err != nil {
		result.Status = models.StatusFailed
		result.Error = err.Error()
		result.Reasoning = "Research stage failed with a hard error; cannot continue pipeline without raw data."
		return result
	}

	if len(snippets) == 0 {
		// EDGE CASE 1: No public footprint — record it explicitly so the
		// reasoning trail shows exactly why we fall back downstream.
		result.Status = models.StatusDegraded
		result.Reasoning = "EDGE CASE — No public footprint: search returned 0 usable results for this prospect/company. " +
			"Downstream stages will use a role/industry fallback signal rather than a timely hook."
		result.Output = models.ResearchOutput{Snippets: []string{}, Count: 0}
		return result
	}

	result.Status = models.StatusOK
	result.Reasoning = fmt.Sprintf("Retrieved %d research snippet(s).", len(snippets))
	result.Output = models.ResearchOutput{Snippets: snippets, Count: len(snippets)}
	return result
}

// ── ExtractSignals ────────────────────────────────────────────────────────────

func runExtractSignals(
	ctx context.Context,
	prospect models.Prospect,
	snippets []string,
	extractor SignalExtractor,
) models.StageResult[models.SignalsOutput] {
	start := time.Now()
	result := models.StageResult[models.SignalsOutput]{
		Stage: "ExtractSignals",
		Input: map[string]any{
			"prospect":      prospect,
			"snippet_count": len(snippets),
		},
	}

	// EDGE CASE 1 short-circuit: no research data at all.
	if len(snippets) == 0 {
		result.Duration = time.Since(start)
		result.Status = models.StatusDegraded
		fb := fallbackSignal(prospect)
		result.Reasoning = fmt.Sprintf(
			"EDGE CASE — No public footprint: no research data to analyse. "+
				"Injecting generic role/industry signal (%q). "+
				"Strategy: outreach will reference the prospect's seniority and company growth trajectory "+
				"rather than a specific recent event.",
			fb.Title,
		)
		result.Output = models.SignalsOutput{
			Signals: []models.Signal{fb},
			EdgeCases: models.EdgeCaseFlags{
				NoFootprint: fmt.Sprintf(
					"No meaningful public signal found for %s. Falling back to role/industry angle.",
					prospect.Company,
				),
			},
		}
		return result
	}

	// Try the richer interface first (Claude provider); fall back to base.
	var (
		signals   []models.Signal
		ecFlags   models.EdgeCaseFlags
		err       error
	)
	if full, ok := extractor.(SignalExtractorFull); ok {
		signals, ecFlags, err = full.ExtractSignalsWithFlags(ctx, prospect, snippets)
	} else {
		signals, err = extractor.ExtractSignals(ctx, prospect, snippets)
	}
	result.Duration = time.Since(start)

	if err != nil {
		result.Status = models.StatusDegraded
		result.Error = err.Error()
		result.Reasoning = fmt.Sprintf(
			"Signal extraction LLM call failed (%s). "+
				"Falling back to generic role/company signal to keep the pipeline running.",
			err.Error(),
		)
		result.Output = models.SignalsOutput{Signals: []models.Signal{fallbackSignal(prospect)}}
		return result
	}

	// Post-process: if LLM returned no signals, inject fallback.
	if len(signals) == 0 {
		fb := fallbackSignal(prospect)
		// Surface the LLM-reported no-footprint flag if present.
		noFpNote := ecFlags.NoFootprint
		if noFpNote == "" {
			noFpNote = fmt.Sprintf("No meaningful public signal found for %s. Falling back to role/industry angle.", prospect.Company)
		}
		result.Status = models.StatusDegraded
		result.Reasoning = fmt.Sprintf(
			"EDGE CASE — No public footprint: LLM found no extractable signals from %d snippet(s). "+
				"Injecting generic fallback signal (%q). "+
				"Outreach will lead with the prospect's role context rather than a specific trigger.",
			len(snippets), fb.Title,
		)
		result.Output = models.SignalsOutput{
			Signals:   []models.Signal{fb},
			EdgeCases: models.EdgeCaseFlags{NoFootprint: noFpNote},
		}
		return result
	}

	// Build reasoning, calling out every edge-case that fired.
	var parts []string
	parts = append(parts, fmt.Sprintf("Extracted %d signal(s).", len(signals)))

	if ecFlags.StaleSignalsRemoved != "" {
		parts = append(parts, fmt.Sprintf(
			"EDGE CASE — Stale signals detected: %s "+
				"Relevance scores penalised by 0.4 (min 0.1) and summaries flagged. "+
				"Recency check: signals older than 24 months are deprioritised.",
			ecFlags.StaleSignalsRemoved,
		))
	}
	if ecFlags.NoFootprint != "" {
		parts = append(parts, "EDGE CASE — No footprint: "+ecFlags.NoFootprint)
	}
	if ecFlags.ConflictingCompany != "" {
		parts = append(parts, "EDGE CASE — Conflicting company info: "+ecFlags.ConflictingCompany)
	}

	result.Status = models.StatusOK
	result.Reasoning = strings.Join(parts, " ")
	result.Output = models.SignalsOutput{Signals: signals, EdgeCases: ecFlags}
	return result
}

// ── SelectHook ────────────────────────────────────────────────────────────────

func runSelectHook(
	ctx context.Context,
	prospect models.Prospect,
	signals []models.Signal,
	selector HookSelector,
) models.StageResult[models.HookOutput] {
	start := time.Now()
	result := models.StageResult[models.HookOutput]{
		Stage: "SelectHook",
		Input: map[string]any{
			"prospect":     prospect,
			"signal_count": len(signals),
		},
	}

	var (
		hook     models.Hook
		runnerUp *models.Signal
		err      error
	)

	// Use richer interface if available (Claude provider).
	if full, ok := selector.(HookSelectorFull); ok {
		hook, runnerUp, err = full.SelectHookWithRunnerUp(ctx, prospect, signals)
	} else {
		hook, err = selector.SelectHook(ctx, prospect, signals)
	}
	result.Duration = time.Since(start)

	if err != nil {
		// LLM failed: pick the highest-relevance signal automatically.
		if len(signals) > 0 {
			best := signals[0]
			for _, s := range signals[1:] {
				if s.Relevance > best.Relevance {
					best = s
				}
			}
			result.Status = models.StatusDegraded
			result.Error = err.Error()
			result.Reasoning = fmt.Sprintf(
				"Hook selection LLM call failed (%s). "+
					"Fell back to highest-relevance signal automatically: %q (score %.2f).",
				err.Error(), best.Title, best.Relevance,
			)
			result.Output = models.HookOutput{
				Hook: models.Hook{
					Signal:    best,
					Reasoning: "Fallback: highest relevance score selected automatically.",
				},
			}
			return result
		}
		result.Status = models.StatusFailed
		result.Error = err.Error()
		result.Reasoning = "Hook selection failed and no signals are available as a fallback."
		return result
	}

	// Detect competing signals: check if the gap to the runner-up is narrow.
	var competingNote string
	if runnerUp != nil {
		gap := hook.Signal.Relevance - runnerUp.Relevance
		if gap < 0 {
			gap = -gap
		}
		if gap <= 0.15 {
			competingNote = fmt.Sprintf(
				" EDGE CASE — Competing signals: %q (%.2f) vs %q (%.2f), gap=%.2f. "+
					"See runner_up in output for the trade-off.",
				hook.Signal.Title, hook.Signal.Relevance,
				runnerUp.Title, runnerUp.Relevance,
				gap,
			)
		}
	}

	result.Status = models.StatusOK
	result.Reasoning = hook.Reasoning + competingNote
	result.Output = models.HookOutput{Hook: hook, RunnerUp: runnerUp}
	return result
}

// ── DraftMessage ──────────────────────────────────────────────────────────────

func runDraftMessage(
	ctx context.Context,
	prospect models.Prospect,
	hook models.Hook,
	drafter Drafter,
) models.StageResult[models.DraftOutput] {
	start := time.Now()
	result := models.StageResult[models.DraftOutput]{
		Stage: "DraftMessage",
		Input: map[string]any{
			"prospect": prospect,
			"hook":     hook,
		},
	}

	draft, err := drafter.Draft(ctx, prospect, hook)
	result.Duration = time.Since(start)

	if err != nil {
		placeholder := models.NewPendingDraft(
			fmt.Sprintf("Quick note — %s at %s", prospect.Name, prospect.Company),
			fmt.Sprintf(
				"Hi %s, I came across your work as %s at %s and wanted to reach out. "+
					"Would love to connect briefly to share something relevant to your team.",
				prospect.Name, prospect.Title, prospect.Company,
			),
			string(hook.Signal.Type),
		)
		result.Status = models.StatusDegraded
		result.Error = err.Error()
		result.Reasoning = fmt.Sprintf(
			"Draft generation LLM call failed (%s). "+
				"Using template placeholder draft — a human reviewer should rewrite before sending.",
			err.Error(),
		)
		result.Output = models.DraftOutput{Draft: placeholder}
		return result
	}

	hookNote := ""
	if hook.Signal.Type == models.SignalGeneral {
		hookNote = " Note: this draft uses a generic role/industry angle because no specific recent signal was found."
	}
	result.Status = models.StatusOK
	result.Reasoning = fmt.Sprintf(
		"Draft generated using hook: %s — %q.%s",
		hook.Signal.Type, hook.Signal.Title, hookNote,
	)
	result.Output = models.DraftOutput{Draft: draft}
	return result
}

// ── AwaitReview ───────────────────────────────────────────────────────────────

func runAwaitReview(draft models.OutreachDraft) models.StageResult[models.ReviewOutput] {
	return models.StageResult[models.ReviewOutput]{
		Stage:     "AwaitReview",
		Status:    models.StatusOK,
		Input:     draft,
		Reasoning: "Pipeline complete. Draft requires human review and explicit approval before any outreach is sent.",
		Output: models.ReviewOutput{
			Status:  "pending_human_review",
			Message: "⚠️  This draft has NOT been sent. A human must review and approve it before any outreach is made.",
		},
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func fallbackSignal(prospect models.Prospect) models.Signal {
	return models.Signal{
		Type:      models.SignalGeneral,
		Title:     fmt.Sprintf("%s at %s", prospect.Title, prospect.Company),
		Summary:   fmt.Sprintf("No specific recent signal found. Outreach will reference %s's seniority as %s at %s.", prospect.Name, prospect.Title, prospect.Company),
		Relevance: 0.3,
	}
}

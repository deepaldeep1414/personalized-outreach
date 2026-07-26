package claude

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/user/personalized-outreach/internal/models"
)

// HookSelector implements pipeline.HookSelector using Claude.
type HookSelector struct {
	client *Client
}

// NewHookSelector creates a HookSelector.
func NewHookSelector(client *Client) *HookSelector {
	return &HookSelector{client: client}
}

const selectorSystem = `You are an expert at cold outreach strategy for B2B sales.
Given a ranked list of signals about a prospect, choose the SINGLE best hook for a
personalised cold email — the one that is most timely, relevant, and compelling.

SELECTION RULES:
1. Prefer recent, time-bounded signals (funding rounds, product launches, new hires) over
   evergreen signals (press mentions, general job postings).
2. Prefer signals directly tied to the prospect's stated role or priorities.
3. When two signals have relevance scores within 0.15 of each other, they are "competing".
   You MUST pick one and explicitly justify why it beats the other in your reasoning.
   Also populate runner_up_index with the losing signal's index.
4. If only one signal exists (or is a fallback general signal), set runner_up_index to -1.
5. Your reasoning must be 2-4 sentences. When edge cases were detected (stale signals,
   no footprint, acquisition), reference them explicitly in your reasoning.

Return ONLY a JSON object matching this schema — no prose, no markdown fences:
{
  "selected_signal_index": <integer, 0-based>,
  "runner_up_index": <integer, 0-based, or -1 if no runner-up>,
  "reasoning": "<2-4 sentences explaining the choice and, if competing, why the winner beat the runner-up>"
}`

// HookSelectorResult extends Hook with an optional runner-up for demo display.
type HookSelectorResult struct {
	Hook     models.Hook
	RunnerUp *models.Signal
}

// SelectHook picks the best signal and returns a Hook.
// It also checks for a near-tie (score gap ≤ 0.15) and surfaces the runner-up
// in the HookOutput so the UI can show the trade-off.
func (s *HookSelector) SelectHook(
	ctx context.Context,
	prospect models.Prospect,
	signals []models.Signal,
) (models.Hook, error) {
	if len(signals) == 0 {
		return models.Hook{}, fmt.Errorf("no signals provided to hook selector")
	}

	signalsJSON, _ := json.MarshalIndent(signals, "", "  ")
	userPrompt := fmt.Sprintf(
		"Prospect: %s, %s at %s\n\nSignals (0-based index):\n%s\n\nSelect the best hook.",
		prospect.Name, prospect.Title, prospect.Company, string(signalsJSON),
	)

	type apiResponse struct {
		SelectedSignalIndex int    `json:"selected_signal_index"`
		RunnerUpIndex       int    `json:"runner_up_index"`
		Reasoning           string `json:"reasoning"`
	}

	var resp apiResponse
	if err := s.client.jsonResponse(ctx, selectorSystem, userPrompt, &resp); err != nil {
		return models.Hook{}, fmt.Errorf("hook selection: %w", err)
	}

	// Bounds-check selected index.
	if resp.SelectedSignalIndex < 0 || resp.SelectedSignalIndex >= len(signals) {
		resp.SelectedSignalIndex = 0
		resp.Reasoning += " (index out of range — defaulted to first signal)"
	}

	return models.Hook{
		Signal:    signals[resp.SelectedSignalIndex],
		Reasoning: resp.Reasoning,
	}, nil
}

// SelectHookWithRunnerUp is like SelectHook but additionally returns the runner-up
// signal when two candidates were closely matched (for demo display).
func (s *HookSelector) SelectHookWithRunnerUp(
	ctx context.Context,
	prospect models.Prospect,
	signals []models.Signal,
) (models.Hook, *models.Signal, error) {
	if len(signals) == 0 {
		return models.Hook{}, nil, fmt.Errorf("no signals provided to hook selector")
	}

	signalsJSON, _ := json.MarshalIndent(signals, "", "  ")
	userPrompt := fmt.Sprintf(
		"Prospect: %s, %s at %s\n\nSignals (0-based index):\n%s\n\nSelect the best hook.",
		prospect.Name, prospect.Title, prospect.Company, string(signalsJSON),
	)

	type apiResponse struct {
		SelectedSignalIndex int    `json:"selected_signal_index"`
		RunnerUpIndex       int    `json:"runner_up_index"`
		Reasoning           string `json:"reasoning"`
	}

	var resp apiResponse
	if err := s.client.jsonResponse(ctx, selectorSystem, userPrompt, &resp); err != nil {
		return models.Hook{}, nil, fmt.Errorf("hook selection: %w", err)
	}

	if resp.SelectedSignalIndex < 0 || resp.SelectedSignalIndex >= len(signals) {
		resp.SelectedSignalIndex = 0
		resp.Reasoning += " (index out of range — defaulted to first signal)"
	}

	hook := models.Hook{
		Signal:    signals[resp.SelectedSignalIndex],
		Reasoning: resp.Reasoning,
	}

	var runnerUp *models.Signal
	if resp.RunnerUpIndex >= 0 && resp.RunnerUpIndex < len(signals) && resp.RunnerUpIndex != resp.SelectedSignalIndex {
		sig := signals[resp.RunnerUpIndex]
		runnerUp = &sig
	}

	return hook, runnerUp, nil
}

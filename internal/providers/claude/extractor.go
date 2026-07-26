package claude

import (
	"context"
	"fmt"
	"strings"

	"github.com/user/personalized-outreach/internal/models"
)

// SignalExtractor implements pipeline.SignalExtractor using Claude.
type SignalExtractor struct {
	client *Client
}

// NewSignalExtractor creates a SignalExtractor.
func NewSignalExtractor(client *Client) *SignalExtractor {
	return &SignalExtractor{client: client}
}

const extractorSystem = `You are a business intelligence analyst specialising in B2B sales research.
Read the research snippets about a prospect and extract structured outreach signals.

Signal types:
  recent_hire        — a new executive or key role joined the company
  funding_round      — the company raised money recently
  job_posting        — active hiring in a relevant area
  exec_interview     — the prospect or another exec gave a public interview or keynote
  press_mention      — notable media coverage
  product_launch     — new product, feature, or major announcement
  acquisition        — the company was acquired, merged, or rebranded (use as a positive signal)

EDGE CASE RULES — you must follow these explicitly and surface them in the output:

RULE 1 — NO FOOTPRINT: If the research contains nothing usable about the prospect or their company
(generic homepage copy, irrelevant results, or nothing at all), return an empty signals array and set
edge_cases.no_footprint to: "No meaningful public signal found for [company]. Falling back to
role/industry angle."

RULE 2 — STALE SIGNALS: Any signal whose published_at date is more than 24 months before today
must have its relevance_score reduced by 0.4 (min 0.1) and its summary prefixed with
"[STALE — recency check failed: published >24 months ago] ". Also set edge_cases.stale_signals_removed
to a short note like "N signal(s) penalised for age: [titles]".

RULE 3 — CONFLICTING COMPANY INFO: If the snippets mention an acquisition, rebrand, merger, or
contradictory company names (e.g. company was acquired by another), flag this in
edge_cases.conflicting_company with a note like: "Company appears to have been acquired by [acquirer].
Using the acquisition itself as a primary signal." and add an acquisition-type signal.

RULE 4 — SCORE SIGNALS on a strict scale:
  0.8–1.0  Very recent (< 3 months), directly relevant to prospect's role
  0.5–0.79 Recent (3–12 months) or moderately relevant
  0.2–0.49 Older (12–24 months) or tangential
  0.1–0.19 Stale (penalised by Rule 2)

Return ONLY a JSON object matching this schema — no prose, no markdown fences:
{
  "signals": [
    {
      "type": "<signal_type>",
      "title": "<short descriptive title>",
      "summary": "<1-2 sentence summary>",
      "source_url": "<url or empty string>",
      "published_at": "<date string or empty string>",
      "relevance_score": <float 0.0-1.0>
    }
  ],
  "edge_cases": {
    "no_footprint": "<string or omit>",
    "stale_signals_removed": "<string or omit>",
    "conflicting_company": "<string or omit>"
  }
}`

func extractorUserPrompt(prospect models.Prospect, snippets []string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Prospect: %s, %s at %s\n", prospect.Name, prospect.Title, prospect.Company))
	if prospect.Notes != "" {
		sb.WriteString(fmt.Sprintf("Additional context: %s\n", prospect.Notes))
	}
	sb.WriteString(fmt.Sprintf("\nNumber of research snippets: %d\n", len(snippets)))
	sb.WriteString("\nResearch snippets:\n")
	for i, s := range snippets {
		sb.WriteString(fmt.Sprintf("[%d] %s\n", i+1, s))
	}
	sb.WriteString("\nApply all edge case rules, then extract every signal you can identify.")
	return sb.String()
}

// ExtractSignals parses research snippets into structured signals using Claude.
// The returned SignalsOutput includes edge_cases flags so the UI and reasoning
// trail can show exactly which edge-case path fired.
func (e *SignalExtractor) ExtractSignals(
	ctx context.Context,
	prospect models.Prospect,
	rawResearch []string,
) ([]models.Signal, error) {
	type apiResponse struct {
		Signals   []models.Signal    `json:"signals"`
		EdgeCases models.EdgeCaseFlags `json:"edge_cases"`
	}

	var resp apiResponse
	err := e.client.jsonResponse(
		ctx,
		extractorSystem,
		extractorUserPrompt(prospect, rawResearch),
		&resp,
	)
	if err != nil {
		return nil, fmt.Errorf("signal extraction: %w", err)
	}
	return resp.Signals, nil
}

// ExtractSignalsWithFlags is like ExtractSignals but also returns EdgeCaseFlags
// so stages.go can include them in the SignalsOutput for display in the UI.
func (e *SignalExtractor) ExtractSignalsWithFlags(
	ctx context.Context,
	prospect models.Prospect,
	rawResearch []string,
) ([]models.Signal, models.EdgeCaseFlags, error) {
	type apiResponse struct {
		Signals   []models.Signal      `json:"signals"`
		EdgeCases models.EdgeCaseFlags `json:"edge_cases"`
	}

	var resp apiResponse
	err := e.client.jsonResponse(
		ctx,
		extractorSystem,
		extractorUserPrompt(prospect, rawResearch),
		&resp,
	)
	if err != nil {
		return nil, models.EdgeCaseFlags{}, fmt.Errorf("signal extraction: %w", err)
	}
	return resp.Signals, resp.EdgeCases, nil
}

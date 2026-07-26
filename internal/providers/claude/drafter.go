package claude

import (
	"context"
	"fmt"

	"github.com/user/personalized-outreach/internal/models"
)

// Drafter implements pipeline.Drafter using Claude.
type Drafter struct {
	client *Client
}

// NewDrafter creates a Drafter.
func NewDrafter(client *Client) *Drafter {
	return &Drafter{client: client}
}

const drafterSystem = `You are an expert cold outreach copywriter for B2B sales.
Your job is to write a highly personalised, genuine-sounding cold email (2–4 sentences)
grounded in a specific signal about the prospect or their company.

Rules:
1. Open with the specific hook — do not start with "I hope this email finds you well" or similar clichés.
2. The message should feel relevant and researched, not templated.
3. End with a soft, low-friction call to action (a question or an offer, not "let's book a call ASAP").
4. Keep the total body under 80 words.
5. Do NOT invent facts not present in the hook or prospect info.
6. Return ONLY a JSON object matching the schema below — no prose, no markdown fences.

Schema:
{
  "subject": "<concise, personalised email subject line>",
  "body": "<the 2-4 sentence outreach message>"
}`

// Draft generates a personalised outreach draft from the chosen hook.
func (d *Drafter) Draft(
	ctx context.Context,
	prospect models.Prospect,
	hook models.Hook,
) (models.OutreachDraft, error) {
	userPrompt := fmt.Sprintf(
		`Prospect: %s, %s at %s
Hook signal type: %s
Hook title: %s
Hook summary: %s
Hook selection reasoning: %s
%s

Write a personalised cold outreach email using this hook.`,
		prospect.Name, prospect.Title, prospect.Company,
		hook.Signal.Type,
		hook.Signal.Title,
		hook.Signal.Summary,
		hook.Reasoning,
		func() string {
			if prospect.Notes != "" {
				return "Additional context: " + prospect.Notes
			}
			return ""
		}(),
	)

	type apiResponse struct {
		Subject string `json:"subject"`
		Body    string `json:"body"`
	}

	var resp apiResponse
	if err := d.client.jsonResponse(ctx, drafterSystem, userPrompt, &resp); err != nil {
		return models.OutreachDraft{}, fmt.Errorf("drafting: %w", err)
	}

	return models.NewPendingDraft(resp.Subject, resp.Body, string(hook.Signal.Type)), nil
}

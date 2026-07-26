package models

// OutreachDraft is the final generated cold-outreach message.
// It is always in "pending_human_review" status — never auto-sent.
type OutreachDraft struct {
	Subject     string `json:"subject"`      // suggested email subject line
	Body        string `json:"body"`         // 2-4 sentence outreach message
	HookUsed    string `json:"hook_used"`    // brief label of which signal was used
	Status      string `json:"status"`       // always "pending_human_review"
	Disclaimer  string `json:"disclaimer"`   // reminder that human must approve
}

// NewPendingDraft constructs a draft with the mandatory review status fields set.
func NewPendingDraft(subject, body, hookUsed string) OutreachDraft {
	return OutreachDraft{
		Subject:    subject,
		Body:       body,
		HookUsed:   hookUsed,
		Status:     "pending_human_review",
		Disclaimer: "This draft has NOT been sent. A human must review and approve it before any outreach is made.",
	}
}

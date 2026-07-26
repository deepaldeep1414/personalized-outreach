package models

// SignalType categorises the kind of intel found about a prospect / company.
type SignalType string

const (
	SignalRecentHire    SignalType = "recent_hire"
	SignalFundingRound  SignalType = "funding_round"
	SignalJobPosting    SignalType = "job_posting"
	SignalExecInterview SignalType = "exec_interview"
	SignalPressMention  SignalType = "press_mention"
	SignalProductLaunch SignalType = "product_launch"
	SignalGeneral       SignalType = "general" // fallback when no strong signal found
)

// Signal is a single piece of intelligence extracted from research.
type Signal struct {
	Type        SignalType `json:"type"`
	Title       string     `json:"title"`
	Summary     string     `json:"summary"`
	Source      string     `json:"source_url,omitempty"`
	PublishedAt string     `json:"published_at,omitempty"` // ISO date or relative e.g. "2 days ago"
	Relevance   float64    `json:"relevance_score"`         // 0.0–1.0 assigned by LLM
}

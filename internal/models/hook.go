package models

// Hook is the single selected signal chosen as the best outreach angle.
type Hook struct {
	Signal    Signal `json:"signal"`
	Reasoning string `json:"reasoning"` // Why this signal was chosen over others
}

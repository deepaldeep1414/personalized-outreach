package models

// Prospect holds the input data for a single outreach target.
type Prospect struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Company     string `json:"company"`
	LinkedInURL string `json:"linkedin_url,omitempty"`
	Notes       string `json:"notes,omitempty"`
}

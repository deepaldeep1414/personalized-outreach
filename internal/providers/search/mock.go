package search

import (
	"context"

	"github.com/user/personalized-outreach/internal/models"
)

// MockSearcher returns fixed snippets for offline testing.
// Set Snippets to override; leave nil to use the built-in demo data.
type MockSearcher struct {
	Snippets []string
	Err      error // if non-nil, Research returns this error
}

// Research implements pipeline.Researcher.
func (m *MockSearcher) Research(_ context.Context, prospect models.Prospect) ([]string, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	if m.Snippets != nil {
		return m.Snippets, nil
	}
	// Default demo data — useful for `--dry-run` mode.
	return []string{
		"[https://techcrunch.com/example] " + prospect.Company + " just raised a $50M Series B to expand their AI platform, according to a TechCrunch report published last week.",
		"[https://businesswire.com/example] " + prospect.Name + ", " + prospect.Title + " at " + prospect.Company + ", gave a keynote at the SaaS Summit on scaling enterprise AI adoption.",
		"[https://linkedin.com/posts/example] " + prospect.Company + " is hiring 30 engineers across ML and platform teams — a signal of significant growth.",
	}, nil
}

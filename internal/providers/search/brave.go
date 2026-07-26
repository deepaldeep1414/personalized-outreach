package search

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/user/personalized-outreach/internal/models"
)

const (
	braveSearchURL = "https://api.search.brave.com/res/v1/web/search"
	defaultCount   = 10 // results per query
)

// BraveSearcher implements pipeline.Researcher using the Brave Search API.
type BraveSearcher struct {
	apiKey  string
	client  *http.Client
	count   int
}

// NewBraveSearcher creates a BraveSearcher.
// apiKey must be a valid Brave Search API subscription key.
func NewBraveSearcher(apiKey string) *BraveSearcher {
	return &BraveSearcher{
		apiKey: apiKey,
		client: &http.Client{Timeout: 15 * time.Second},
		count:  defaultCount,
	}
}

// Research runs two searches: one prospect-focused and one company-focused,
// then merges the snippets deduplicated by URL.
func (b *BraveSearcher) Research(ctx context.Context, prospect models.Prospect) ([]string, error) {
	queries := buildQueries(prospect)

	seen := map[string]bool{}
	var snippets []string

	for _, q := range queries {
		results, err := b.search(ctx, q)
		if err != nil {
			// One query failing shouldn't abort the whole thing — log and continue.
			continue
		}
		for _, r := range results {
			key := r.url
			if seen[key] {
				continue
			}
			seen[key] = true
			// Combine title + description into a single useful text snippet.
			snippet := strings.TrimSpace(r.title + ". " + r.description)
			if snippet != ". " && snippet != "" {
				snippets = append(snippets, fmt.Sprintf("[%s] %s", r.url, snippet))
			}
		}
	}

	return snippets, nil
}

// buildQueries returns 2-3 targeted search queries for the prospect.
func buildQueries(p models.Prospect) []string {
	name := p.Name
	company := p.Company

	queries := []string{
		fmt.Sprintf(`"%s" "%s" news OR announcement OR launch OR interview`, name, company),
		fmt.Sprintf(`"%s" funding OR expansion OR product OR hire`, company),
	}
	if p.LinkedInURL != "" {
		// Use LinkedIn URL as an additional search seed without fetching it directly.
		queries = append(queries, fmt.Sprintf(`"%s" site:linkedin.com OR press`, name))
	}
	return queries
}

// braveResult is a parsed result from the Brave API response.
type braveResult struct {
	title       string
	description string
	url         string
}

// braveAPIResponse mirrors the relevant portion of the Brave Search JSON response.
type braveAPIResponse struct {
	Web struct {
		Results []struct {
			Title       string `json:"title"`
			Description string `json:"description"`
			URL         string `json:"url"`
		} `json:"results"`
	} `json:"web"`
}

// search executes a single Brave Search API request.
func (b *BraveSearcher) search(ctx context.Context, query string) ([]braveResult, error) {
	u, _ := url.Parse(braveSearchURL)
	q := u.Query()
	q.Set("q", query)
	q.Set("count", fmt.Sprintf("%d", b.count))
	q.Set("result_filter", "web")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Encoding", "gzip")
	req.Header.Set("X-Subscription-Token", b.apiKey)

	resp, err := b.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("brave API returned %d: %s", resp.StatusCode, string(body))
	}

	var apiResp braveAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	results := make([]braveResult, 0, len(apiResp.Web.Results))
	for _, r := range apiResp.Web.Results {
		results = append(results, braveResult{
			title:       r.Title,
			description: r.Description,
			url:         r.URL,
		})
	}
	return results, nil
}

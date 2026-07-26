package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/user/personalized-outreach/internal/models"
)

const tavilySearchURL = "https://api.tavily.com/search"

// TavilySearcher implements pipeline.Researcher using Tavily Search API.
// Tavily provides 1,000 free AI search queries/month with no credit card required.
type TavilySearcher struct {
	apiKey string
	client *http.Client
}

func NewTavilySearcher(apiKey string) *TavilySearcher {
	return &TavilySearcher{
		apiKey: apiKey,
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

type tavilyRequest struct {
	APIKey      string `json:"api_key"`
	Query       string `json:"query"`
	MaxResults  int    `json:"max_results"`
	SearchDepth string `json:"search_depth"`
}

type tavilyResponse struct {
	Results []struct {
		Title   string `json:"title"`
		URL     string `json:"url"`
		Content string `json:"content"`
	} `json:"results"`
}

func (t *TavilySearcher) Research(ctx context.Context, prospect models.Prospect) ([]string, error) {
	queries := buildQueries(prospect)
	seen := map[string]bool{}
	var snippets []string

	for _, q := range queries {
		results, err := t.search(ctx, q)
		if err != nil {
			continue
		}
		for _, r := range results {
			if seen[r.URL] {
				continue
			}
			seen[r.URL] = true
			snippet := strings.TrimSpace(r.Title + ". " + r.Content)
			if snippet != ". " && snippet != "" {
				snippets = append(snippets, fmt.Sprintf("[%s] %s", r.URL, snippet))
			}
		}
	}

	return snippets, nil
}

func (t *TavilySearcher) search(ctx context.Context, query string) ([]struct{ Title, URL, Content string }, error) {
	reqBody := tavilyRequest{
		APIKey:      t.apiKey,
		Query:       query,
		MaxResults:  5,
		SearchDepth: "basic",
	}
	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tavilySearchURL, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("tavily API returned %d: %s", resp.StatusCode, string(body))
	}

	var apiResp tavilyResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, err
	}

	out := make([]struct{ Title, URL, Content string }, len(apiResp.Results))
	for i, r := range apiResp.Results {
		out[i] = struct{ Title, URL, Content string }{
			Title:   r.Title,
			URL:     r.URL,
			Content: r.Content,
		}
	}
	return out, nil
}

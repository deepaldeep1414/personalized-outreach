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

const serperSearchURL = "https://google.serper.dev/search"

// SerperSearcher implements pipeline.Researcher using Serper Google Search API.
// Serper provides 2,500 free Google searches with no credit card required.
type SerperSearcher struct {
	apiKey string
	client *http.Client
}

func NewSerperSearcher(apiKey string) *SerperSearcher {
	return &SerperSearcher{
		apiKey: apiKey,
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

type serperRequest struct {
	Query string `json:"q"`
	Num   int    `json:"num"`
}

type serperResponse struct {
	Organic []struct {
		Title   string `json:"title"`
		Link    string `json:"link"`
		Snippet string `json:"snippet"`
	} `json:"organic"`
}

func (s *SerperSearcher) Research(ctx context.Context, prospect models.Prospect) ([]string, error) {
	queries := buildQueries(prospect)
	seen := map[string]bool{}
	var snippets []string

	for _, q := range queries {
		results, err := s.search(ctx, q)
		if err != nil {
			continue
		}
		for _, r := range results {
			if seen[r.Link] {
				continue
			}
			seen[r.Link] = true
			snippet := strings.TrimSpace(r.Title + ". " + r.Snippet)
			if snippet != ". " && snippet != "" {
				snippets = append(snippets, fmt.Sprintf("[%s] %s", r.Link, snippet))
			}
		}
	}

	return snippets, nil
}

func (s *SerperSearcher) search(ctx context.Context, query string) ([]struct{ Title, Link, Snippet string }, error) {
	reqBody := serperRequest{Query: query, Num: 5}
	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, serperSearchURL, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-KEY", s.apiKey)

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("serper API returned %d: %s", resp.StatusCode, string(body))
	}

	var apiResp serperResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, err
	}

	out := make([]struct{ Title, Link, Snippet string }, len(apiResp.Organic))
	for i, r := range apiResp.Organic {
		out[i] = struct{ Title, Link, Snippet string }{
			Title:   r.Title,
			Link:    r.Link,
			Snippet: r.Snippet,
		}
	}
	return out, nil
}

package search

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/user/personalized-outreach/internal/models"
	"github.com/user/personalized-outreach/internal/pipeline"
)

// MultiSearcher runs multiple search providers concurrently and merges/deduplicates snippets by URL.
// Combining multiple search engines (e.g. Tavily + Serper + Brave) yields higher precision,
// broader news coverage, and richer intelligence for signal extraction.
type MultiSearcher struct {
	searchers []pipeline.Researcher
	names     []string
}

// NewMultiSearcher combines multiple searchers into a unified multi-engine searcher.
func NewMultiSearcher(providers ...struct {
	Name       string
	Researcher pipeline.Researcher
}) *MultiSearcher {
	var searchers []pipeline.Researcher
	var names []string
	for _, p := range providers {
		if p.Researcher != nil {
			searchers = append(searchers, p.Researcher)
			names = append(names, p.Name)
		}
	}
	return &MultiSearcher{searchers: searchers, names: names}
}

func (m *MultiSearcher) Names() string {
	return strings.Join(m.names, " + ")
}

// Research executes all active search providers concurrently and merges deduplicated snippets.
func (m *MultiSearcher) Research(ctx context.Context, prospect models.Prospect) ([]string, error) {
	if len(m.searchers) == 0 {
		return nil, fmt.Errorf("no search providers available")
	}
	if len(m.searchers) == 1 {
		return m.searchers[0].Research(ctx, prospect)
	}

	type result struct {
		snippets []string
		err      error
	}

	results := make(chan result, len(m.searchers))
	var wg sync.WaitGroup

	for _, s := range m.searchers {
		wg.Add(1)
		go func(r pipeline.Researcher) {
			defer wg.Done()
			snips, err := r.Research(ctx, prospect)
			results <- result{snippets: snips, err: err}
		}(s)
	}

	wg.Wait()
	close(results)

	seenURL := map[string]bool{}
	var merged []string

	for r := range results {
		if r.err != nil {
			continue
		}
		for _, snip := range r.snippets {
			urlKey := snip
			if strings.HasPrefix(snip, "[") {
				if idx := strings.Index(snip, "]"); idx != -1 {
					urlKey = snip[1:idx]
				}
			}
			if seenURL[urlKey] {
				continue
			}
			seenURL[urlKey] = true
			merged = append(merged, snip)
		}
	}

	return merged, nil
}

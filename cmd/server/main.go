package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/user/personalized-outreach/internal/models"
	"github.com/user/personalized-outreach/internal/pipeline"
	"github.com/user/personalized-outreach/internal/providers/claude"
	"github.com/user/personalized-outreach/internal/providers/search"
	appserver "github.com/user/personalized-outreach/internal/server"
	"github.com/user/personalized-outreach/internal/store"
)

//go:embed static
var staticFiles embed.FS

func main() {
	var (
		port   = flag.Int("port", 8085, "HTTP listen port")
		dbPath = flag.String("db", "outreach.db", "SQLite database path")
		model  = flag.String("model", "", "Claude model override (default: claude-sonnet-4-5)")
		dryRun = flag.Bool("dry-run", false, "Use mock providers — no API keys required")
	)
	flag.Parse()

	loadEnv(".env")

	// ── Environment overrides ──────────────────────────────────────────────────
	groqKey := os.Getenv("GROQ_API_KEY")
	geminiKey := os.Getenv("GEMINI_API_KEY")
	openaiKey := os.Getenv("OPENAI_API_KEY")
	claudeKey := os.Getenv("ANTHROPIC_API_KEY")
	braveKey := os.Getenv("BRAVE_API_KEY")
	tavilyKey := os.Getenv("TAVILY_API_KEY")
	serperKey := os.Getenv("SERPER_API_KEY")

	if m := os.Getenv("OUTREACH_MODEL"); m != "" && *model == "" {
		*model = m
	}

	hasLLMKey := groqKey != "" || geminiKey != "" || openaiKey != "" || claudeKey != ""
	hasSearchKey := tavilyKey != "" || serperKey != "" || braveKey != ""

	if !*dryRun {
		if !hasLLMKey && !hasSearchKey {
			log.Println("Notice: No API keys set. Defaulting to --dry-run mode with mock providers.")
			*dryRun = true
		} else if !hasSearchKey {
			log.Println("Notice: No search API key set. Using LLM provider with mock search data.")
		} else if !hasLLMKey {
			log.Println("Notice: No LLM API key set. Defaulting to --dry-run mode.")
			*dryRun = true
		}
	}

	// ── Store ──────────────────────────────────────────────────────────────────
	st, err := store.Open(*dbPath)
	if err != nil {
		log.Fatalf("opening store at %q: %v", *dbPath, err)
	}
	defer st.Close()
	log.Printf("Database : %s", *dbPath)

	// ── Providers ──────────────────────────────────────────────────────────────
	type namedSearcher struct {
		Name       string
		Researcher pipeline.Researcher
	}
	var activeSearchers []namedSearcher

	if tavilyKey != "" {
		activeSearchers = append(activeSearchers, namedSearcher{"Tavily Search", search.NewTavilySearcher(tavilyKey)})
	}
	if serperKey != "" {
		activeSearchers = append(activeSearchers, namedSearcher{"Serper Google", search.NewSerperSearcher(serperKey)})
	}
	if braveKey != "" {
		activeSearchers = append(activeSearchers, namedSearcher{"Brave Search", search.NewBraveSearcher(braveKey)})
	}

	var researcher pipeline.Researcher
	if *dryRun {
		researcher = &search.MockSearcher{}
		log.Printf("Searcher : Mock Searcher (dry-run)")
	} else if len(activeSearchers) > 0 {
		var multiProviders []struct {
			Name       string
			Researcher pipeline.Researcher
		}
		for _, s := range activeSearchers {
			multiProviders = append(multiProviders, struct {
				Name       string
				Researcher pipeline.Researcher
			}{Name: s.Name, Researcher: s.Researcher})
		}
		ms := search.NewMultiSearcher(multiProviders...)
		researcher = ms
		log.Printf("Searcher : Multi-Engine [%s]", ms.Names())
	} else {
		researcher = &search.MockSearcher{}
		log.Printf("Searcher : Mock Searcher")
	}

	claudeClient := claude.NewClient(claudeKey, *model)
	if !*dryRun {
		log.Printf("LLM      : %s", claudeClient.ProviderName())
	}

	var (
		extractor pipeline.SignalExtractor = claude.NewSignalExtractor(claudeClient)
		selector  pipeline.HookSelector   = claude.NewHookSelector(claudeClient)
		drafter   pipeline.Drafter        = claude.NewDrafter(claudeClient)
	)

	// Full dry-run: replace LLM providers with in-process mocks too.
	if *dryRun && claudeKey == "" {
		extractor = &mockExtractor{}
		selector = &mockSelector{}
		drafter = &mockDrafter{}
	}

	pipelineCfg := pipeline.Config{
		Researcher:      researcher,
		SignalExtractor: extractor,
		HookSelector:    selector,
		Drafter:         drafter,
	}

	// ── HTTP ───────────────────────────────────────────────────────────────────
	mux := http.NewServeMux()

	// API routes
	srv := appserver.New(st, pipelineCfg)
	srv.RegisterRoutes(mux)

	// Static files at "/" (API routes registered above take precedence)
	sub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		log.Fatalf("static FS: %v", err)
	}
	mux.Handle("/", http.FileServer(http.FS(sub)))

	httpSrv := &http.Server{
		Addr:         fmt.Sprintf(":%d", *port),
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 0, // SSE streams need unlimited write time
		IdleTimeout:  120 * time.Second,
	}

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-quit
		log.Println("Shutting down…")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		httpSrv.Shutdown(ctx)
	}()

	mode := "production"
	if *dryRun {
		mode = "dry-run (mock providers)"
	}
	log.Printf("Mode     : %s", mode)
	log.Printf("Listening: http://localhost:%d", *port)

	if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server: %v", err)
	}
}

// ── Full dry-run mock providers ────────────────────────────────────────────────

type mockExtractor struct{}

func (m *mockExtractor) ExtractSignals(_ context.Context, p models.Prospect, _ []string) ([]models.Signal, error) {
	return []models.Signal{
		{
			Type:      models.SignalFundingRound,
			Title:     p.Company + " raises Series B",
			Summary:   p.Company + " recently secured $50M to expand its platform, signalling rapid growth.",
			Relevance: 0.88,
		},
		{
			Type:      models.SignalJobPosting,
			Title:     "Engineering hiring surge at " + p.Company,
			Summary:   p.Company + " is actively hiring across ML and platform engineering teams.",
			Relevance: 0.65,
		},
	}, nil
}

type mockSelector struct{}

func (m *mockSelector) SelectHook(_ context.Context, _ models.Prospect, signals []models.Signal) (models.Hook, error) {
	return models.Hook{
		Signal:    signals[0],
		Reasoning: "[DRY RUN] The funding round is the most timely and high-signal hook for cold outreach.",
	}, nil
}

type mockDrafter struct{}

func (m *mockDrafter) Draft(_ context.Context, p models.Prospect, hook models.Hook) (models.OutreachDraft, error) {
	body := fmt.Sprintf(
		"Hi %s, saw the news about %s — impressive momentum. "+
			"I work with teams going through similar growth inflection points and thought there might be a natural fit. "+
			"Would a 15-minute conversation make sense this week?",
		p.Name, hook.Signal.Title,
	)
	return models.NewPendingDraft(
		"Quick note re: "+hook.Signal.Title,
		body,
		string(hook.Signal.Type),
	), nil
}

// loadEnv reads a simple key=value .env file if present and sets env variables.
func loadEnv(filename string) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			k := strings.TrimSpace(parts[0])
			v := strings.TrimSpace(parts[1])
			v = strings.Trim(v, `"'`)
			if os.Getenv(k) == "" && v != "" {
				os.Setenv(k, v)
			}
		}
	}
}

package server

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/user/personalized-outreach/internal/models"
	"github.com/user/personalized-outreach/internal/pipeline"
	"github.com/user/personalized-outreach/internal/store"
)

// Server wires together the store, SSE broker, and pipeline providers.
type Server struct {
	store       *store.Store
	pipelineCfg pipeline.Config
	broker      *SSEBroker
}

// New creates a Server.
func New(st *store.Store, cfg pipeline.Config) *Server {
	return &Server{
		store:       st,
		pipelineCfg: cfg,
		broker:      newSSEBroker(),
	}
}

// RegisterRoutes attaches all API routes to mux.
// Static files should be registered separately on the same mux at "/".
func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /runs", s.handleCreateRun)
	mux.HandleFunc("GET /runs", s.handleListRuns)
	// More-specific paths (/stream, /replay) must be registered before /{id}.
	mux.HandleFunc("GET /runs/{id}/stream", s.handleStreamRun)
	mux.HandleFunc("GET /runs/{id}/replay", s.handleReplayRun)
	mux.HandleFunc("GET /runs/{id}", s.handleGetRun)
	mux.HandleFunc("POST /runs/{id}/review", s.handleReview)
}

// ── Run ID ────────────────────────────────────────────────────────────────────

// newRunID generates a URL-safe unique ID (UUID v4 without hyphens).
func newRunID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("run%d", time.Now().UnixNano())
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant
	return fmt.Sprintf("%x%x%x%x%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// ── Pipeline runner ───────────────────────────────────────────────────────────

// runPipeline is launched in a goroutine by handleCreateRun.
// It consumes the stage channel, writes each stage to the DB incrementally,
// publishes SSE events, and finalises the run record when done.
// Uses context.Background() so the run outlives the HTTP request.
func (s *Server) runPipeline(runID string, prospect models.Prospect) {
	p := pipeline.New(s.pipelineCfg)

	var (
		hookType, hookTitle     string
		draftSubject, draftBody string
		finalStatus             = "completed"
	)

	for event := range p.Run(context.Background(), prospect) {
		outputJSON, reasoning, status, errMsg, durationMs := unpackEvent(event)

		// Capture hook/draft fields for the runs summary row.
		if event.Hook != nil {
			hookType = string(event.Hook.Output.Hook.Signal.Type)
			hookTitle = event.Hook.Output.Hook.Signal.Title
		}
		if event.Draft != nil {
			draftSubject = event.Draft.Output.Draft.Subject
			draftBody = event.Draft.Output.Draft.Body
		}
		if status == string(models.StatusFailed) {
			finalStatus = "failed"
		}

		// ── Write to DB (incremental) ───────────────────────────────────────
		if err := s.store.InsertStage(store.RunStage{
			RunID:       runID,
			StageName:   event.Name,
			StageIndex:  event.Index,
			Status:      status,
			OutputJSON:  outputJSON,
			Reasoning:   reasoning,
			DurationMs:  durationMs,
			ErrorMsg:    errMsg,
			CompletedAt: time.Now(),
		}); err != nil {
			log.Printf("[%s] DB insert stage %s: %v", runID, event.Name, err)
		}

		// ── Publish SSE event ───────────────────────────────────────────────
		payload, _ := json.Marshal(map[string]any{
			"type":        "stage",
			"index":       event.Index,
			"total":       event.Total,
			"stage":       event.Name,
			"status":      status,
			"output":      json.RawMessage(outputJSON),
			"reasoning":   reasoning,
			"duration_ms": durationMs,
			"error":       errMsg,
		})
		s.broker.Publish(runID, payload)

		if status == string(models.StatusFailed) {
			break
		}
	}

	// ── Finalise run ────────────────────────────────────────────────────────
	if err := s.store.CompleteRun(runID, finalStatus, hookType, hookTitle, draftSubject, draftBody); err != nil {
		log.Printf("[%s] DB complete run: %v", runID, err)
	}

	done, _ := json.Marshal(map[string]any{"type": "done", "status": finalStatus})
	s.broker.Finish(runID, done)
	log.Printf("[%s] pipeline finished: %s", runID, finalStatus)
}

// unpackEvent extracts the common fields from a StageEvent regardless of which
// stage variant is set.
func unpackEvent(ev pipeline.StageEvent) (outputJSON, reasoning, status, errMsg string, durationMs int64) {
	marshal := func(v any) string {
		b, _ := json.Marshal(v)
		return string(b)
	}
	switch {
	case ev.Research != nil:
		r := ev.Research
		return marshal(r.Output), r.Reasoning, string(r.Status), r.Error, r.Duration.Milliseconds()
	case ev.Signals != nil:
		r := ev.Signals
		return marshal(r.Output), r.Reasoning, string(r.Status), r.Error, r.Duration.Milliseconds()
	case ev.Hook != nil:
		r := ev.Hook
		return marshal(r.Output), r.Reasoning, string(r.Status), r.Error, r.Duration.Milliseconds()
	case ev.Draft != nil:
		r := ev.Draft
		return marshal(r.Output), r.Reasoning, string(r.Status), r.Error, r.Duration.Milliseconds()
	case ev.Review != nil:
		r := ev.Review
		return marshal(r.Output), r.Reasoning, string(r.Status), r.Error, 0
	}
	return "{}", "", "ok", "", 0
}

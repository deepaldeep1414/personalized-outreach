package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/user/personalized-outreach/internal/models"
	"github.com/user/personalized-outreach/internal/store"
)

// ── Helpers ───────────────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// ── Rate limiter ──────────────────────────────────────────────────────────────

// rateLimiter is a simple in-memory per-IP token bucket.
// Maximum 10 POST /runs per IP per minute.
type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string][]time.Time
}

var rl = &rateLimiter{buckets: make(map[string][]time.Time)}

func (r *rateLimiter) allow(ip string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-time.Minute)
	ts := r.buckets[ip]
	// Drop timestamps older than 1 minute.
	j := 0
	for _, t := range ts {
		if t.After(cutoff) {
			ts[j] = t
			j++
		}
	}
	ts = ts[:j]
	if len(ts) >= 10 {
		r.buckets[ip] = ts
		return false
	}
	r.buckets[ip] = append(ts, now)
	return true
}

func clientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		return strings.TrimSpace(strings.Split(fwd, ",")[0])
	}
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

// ── Input validation ──────────────────────────────────────────────────────────

const (
	maxNameLen    = 120
	maxTitleLen   = 120
	maxCompanyLen = 120
	maxNotesLen   = 1000
	maxURLLen     = 500
)

type validationError struct{ msg string }

func (v validationError) Error() string { return v.msg }

func validateProspect(p models.Prospect) error {
	if strings.TrimSpace(p.Name) == "" {
		return validationError{"name is required"}
	}
	if len(p.Name) > maxNameLen {
		return validationError{fmt.Sprintf("name must be ≤ %d characters", maxNameLen)}
	}
	if strings.TrimSpace(p.Title) == "" {
		return validationError{"title is required"}
	}
	if len(p.Title) > maxTitleLen {
		return validationError{fmt.Sprintf("title must be ≤ %d characters", maxTitleLen)}
	}
	if strings.TrimSpace(p.Company) == "" {
		return validationError{"company is required"}
	}
	if len(p.Company) > maxCompanyLen {
		return validationError{fmt.Sprintf("company must be ≤ %d characters", maxCompanyLen)}
	}
	if len(p.Notes) > maxNotesLen {
		return validationError{fmt.Sprintf("notes must be ≤ %d characters", maxNotesLen)}
	}
	if p.LinkedInURL != "" {
		if len(p.LinkedInURL) > maxURLLen {
			return validationError{fmt.Sprintf("linkedin_url must be ≤ %d characters", maxURLLen)}
		}
		u, err := url.Parse(p.LinkedInURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			return validationError{"linkedin_url must be a valid http/https URL"}
		}
	}
	return nil
}

// ── POST /runs ────────────────────────────────────────────────────────────────

func (s *Server) handleCreateRun(w http.ResponseWriter, r *http.Request) {
	// Rate limit
	ip := clientIP(r)
	if !rl.allow(ip) {
		writeError(w, http.StatusTooManyRequests, "rate limit exceeded: max 10 runs per IP per minute")
		return
	}

	// Body size guard (1 MB)
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	var prospect models.Prospect
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&prospect); err != nil {
		msg := err.Error()
		if strings.Contains(msg, "unknown field") {
			writeError(w, http.StatusBadRequest, "unknown field in request: "+msg)
		} else {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+msg)
		}
		return
	}

	if err := validateProspect(prospect); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	// Sanitise whitespace
	prospect.Name = strings.TrimSpace(prospect.Name)
	prospect.Title = strings.TrimSpace(prospect.Title)
	prospect.Company = strings.TrimSpace(prospect.Company)
	prospect.Notes = strings.TrimSpace(prospect.Notes)

	runID := newRunID()
	if err := s.store.CreateRun(store.Run{
		ID:          runID,
		Name:        prospect.Name,
		Title:       prospect.Title,
		Company:     prospect.Company,
		LinkedInURL: prospect.LinkedInURL,
		Notes:       prospect.Notes,
		StartedAt:   time.Now(),
	}); err != nil {
		log.Printf("CreateRun DB error: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to create run — database error")
		return
	}

	go s.runPipeline(runID, prospect)
	writeJSON(w, http.StatusCreated, map[string]string{"run_id": runID})
}

// ── GET /runs ─────────────────────────────────────────────────────────────────

func (s *Server) handleListRuns(w http.ResponseWriter, r *http.Request) {
	runs, err := s.store.ListRuns()
	if err != nil {
		log.Printf("ListRuns DB error: %v", err)
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}

	type item struct {
		ID           string  `json:"id"`
		Name         string  `json:"name"`
		Title        string  `json:"title"`
		Company      string  `json:"company"`
		Status       string  `json:"status"`
		HookType     string  `json:"hook_type"`
		HookTitle    string  `json:"hook_title"`
		DraftSubject string  `json:"draft_subject"`
		ReviewStatus string  `json:"review_status"`
		StartedAt    string  `json:"started_at"`
		FinishedAt   *string `json:"finished_at"`
	}

	out := make([]item, len(runs))
	for i, run := range runs {
		out[i] = item{
			ID:           run.ID,
			Name:         run.Name,
			Title:        run.Title,
			Company:      run.Company,
			Status:       run.Status,
			HookType:     run.HookType,
			HookTitle:    run.HookTitle,
			DraftSubject: run.DraftSubject,
			ReviewStatus: run.ReviewStatus,
			StartedAt:    run.StartedAt.Format(time.RFC3339),
		}
		if run.FinishedAt != nil {
			ts := run.FinishedAt.Format(time.RFC3339)
			out[i].FinishedAt = &ts
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// ── GET /runs/{id} ────────────────────────────────────────────────────────────

func (s *Server) handleGetRun(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	run, err := s.store.GetRun(id)
	if err != nil {
		log.Printf("GetRun DB error: %v", err)
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	if run == nil {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}

	stages, err := s.store.GetRunStages(id)
	if err != nil {
		log.Printf("GetRunStages DB error: %v", err)
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}

	type stageItem struct {
		StageName  string          `json:"stage_name"`
		StageIndex int             `json:"stage_index"`
		Status     string          `json:"status"`
		Output     json.RawMessage `json:"output"`
		Reasoning  string          `json:"reasoning"`
		DurationMs int64           `json:"duration_ms"`
		ErrorMsg   string          `json:"error_msg,omitempty"`
	}

	stageItems := make([]stageItem, len(stages))
	for i, st := range stages {
		raw := json.RawMessage(st.OutputJSON)
		if !json.Valid([]byte(st.OutputJSON)) {
			raw = json.RawMessage("{}")
		}
		stageItems[i] = stageItem{
			StageName:  st.StageName,
			StageIndex: st.StageIndex,
			Status:     st.Status,
			Output:     raw,
			Reasoning:  st.Reasoning,
			DurationMs: st.DurationMs,
			ErrorMsg:   st.ErrorMsg,
		}
	}

	type detail struct {
		ID           string      `json:"id"`
		Name         string      `json:"name"`
		Title        string      `json:"title"`
		Company      string      `json:"company"`
		LinkedInURL  string      `json:"linkedin_url,omitempty"`
		Notes        string      `json:"notes,omitempty"`
		Status       string      `json:"status"`
		HookType     string      `json:"hook_type,omitempty"`
		HookTitle    string      `json:"hook_title,omitempty"`
		DraftSubject string      `json:"draft_subject,omitempty"`
		DraftBody    string      `json:"draft_body,omitempty"`
		ReviewStatus string      `json:"review_status"`
		StartedAt    string      `json:"started_at"`
		FinishedAt   *string     `json:"finished_at,omitempty"`
		Stages       []stageItem `json:"stages"`
	}

	d := detail{
		ID:           run.ID,
		Name:         run.Name,
		Title:        run.Title,
		Company:      run.Company,
		LinkedInURL:  run.LinkedInURL,
		Notes:        run.Notes,
		Status:       run.Status,
		HookType:     run.HookType,
		HookTitle:    run.HookTitle,
		DraftSubject: run.DraftSubject,
		DraftBody:    run.DraftBody,
		ReviewStatus: run.ReviewStatus,
		StartedAt:    run.StartedAt.Format(time.RFC3339),
		Stages:       stageItems,
	}
	if run.FinishedAt != nil {
		ts := run.FinishedAt.Format(time.RFC3339)
		d.FinishedAt = &ts
	}
	writeJSON(w, http.StatusOK, d)
}

// ── GET /runs/{id}/stream ─────────────────────────────────────────────────────

func (s *Server) handleStreamRun(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	run, err := s.store.GetRun(id)
	if err != nil || run == nil {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	if origin := r.Header.Get("Origin"); origin != "" {
		w.Header().Set("Access-Control-Allow-Origin", origin)
	}
	w.WriteHeader(http.StatusOK)

	history, ch, alreadyDone := s.broker.Subscribe(id)
	for _, data := range history {
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}
	if alreadyDone {
		return
	}
	defer s.broker.Unsubscribe(id, ch)

	ctx := r.Context()
	for {
		select {
		case data, open := <-ch:
			if !open {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		case <-ctx.Done():
			return
		}
	}
}

// ── GET /runs/{id}/replay ─────────────────────────────────────────────────────
// Replays a completed run's stored stage outputs as SSE events with a short
// delay between each stage. No LLM or search calls are made — pure DB replay.
// Identical SSE event format to /stream so the frontend JS works unchanged.

const replayDelay = 900 * time.Millisecond

func (s *Server) handleReplayRun(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	run, err := s.store.GetRun(id)
	if err != nil || run == nil {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}
	if run.Status == "running" {
		writeError(w, http.StatusConflict, "run is still in progress — use /stream instead")
		return
	}

	stages, err := s.store.GetRunStages(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	if origin := r.Header.Get("Origin"); origin != "" {
		w.Header().Set("Access-Control-Allow-Origin", origin)
	}
	w.WriteHeader(http.StatusOK)

	ctx := r.Context()
	for _, st := range stages {
		select {
		case <-ctx.Done():
			return // client disconnected
		case <-time.After(replayDelay):
		}

		raw := json.RawMessage(st.OutputJSON)
		if !json.Valid([]byte(st.OutputJSON)) {
			raw = json.RawMessage("{}")
		}

		payload, _ := json.Marshal(map[string]any{
			"type":        "stage",
			"index":       st.StageIndex,
			"total":       5,
			"stage":       st.StageName,
			"status":      st.Status,
			"output":      raw,
			"reasoning":   st.Reasoning,
			"duration_ms": st.DurationMs,
			"error":       st.ErrorMsg,
		})
		fmt.Fprintf(w, "data: %s\n\n", payload)
		flusher.Flush()
	}

	// Send terminal done event
	done, _ := json.Marshal(map[string]any{"type": "done", "status": run.Status})
	fmt.Fprintf(w, "data: %s\n\n", done)
	flusher.Flush()
}

// ── POST /runs/{id}/review ────────────────────────────────────────────────────

func (s *Server) handleReview(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	var body struct {
		Action string `json:"action"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if body.Action != "approved" && body.Action != "discarded" {
		writeError(w, http.StatusBadRequest, `action must be "approved" or "discarded"`)
		return
	}

	run, err := s.store.GetRun(id)
	if err != nil || run == nil {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}
	if run.Status != "completed" {
		writeError(w, http.StatusConflict, "cannot review a run that has not completed")
		return
	}

	if err := s.store.UpdateReview(id, body.Action); err != nil {
		log.Printf("UpdateReview DB error: %v", err)
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": body.Action})
}

// ── DELETE /runs/{id} ─────────────────────────────────────────────────────────

func (s *Server) handleDeleteRun(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	run, err := s.store.GetRun(id)
	if err != nil {
		log.Printf("GetRun DB error: %v", err)
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	if run == nil {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}
	if run.Status == "running" {
		writeError(w, http.StatusConflict, "cannot delete a run that is still in progress")
		return
	}

	if err := s.store.DeleteRun(id); err != nil {
		log.Printf("DeleteRun DB error: %v", err)
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// ── DELETE /runs ──────────────────────────────────────────────────────────────
// Clears all run history. Intentionally has no confirmation server-side —
// the frontend confirms with the user before calling this.

func (s *Server) handleDeleteAllRuns(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteAllRuns(); err != nil {
		log.Printf("DeleteAllRuns DB error: %v", err)
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "cleared"})
}

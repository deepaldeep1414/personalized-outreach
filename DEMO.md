# Personalized Outreach — Demo Guide

> **Case study:** "From prospect to draft" — a production-ready AI pipeline that researches a prospect, extracts signals, picks the best outreach hook, and generates a personalised cold email draft for human review. Every stage is auditable; nothing is ever auto-sent.

---

## Quick Start

```powershell
# Install deps (first time only)
go mod download

# Run with real APIs
$env:ANTHROPIC_API_KEY = "sk-ant-..."
$env:BRAVE_API_KEY     = "BSA..."
go run ./cmd/server

# Offline dry-run (mock providers — no API keys needed)
go run ./cmd/server --dry-run

# Then open:
# http://localhost:8085
```

Flags:

| Flag        | Default       | Purpose                                       |
|-------------|---------------|-----------------------------------------------|
| `--port`    | `8085`        | HTTP listen port                              |
| `--db`      | `outreach.db` | SQLite database path                          |
| `--model`   | env var       | Override Claude model                         |
| `--dry-run` | `false`       | Use mock providers, no API calls              |

---

## Pipeline Architecture

```
POST /runs (prospect JSON)
      │
      ▼
┌─────────────────────────────────────────────────────────┐
│  Pipeline (goroutine, context.Background)               │
│                                                         │
│  1. Research        → Brave Search API                  │
│     └─ returns []string snippets                        │
│                                                         │
│  2. ExtractSignals  → Claude                            │
│     └─ returns []Signal + EdgeCaseFlags                 │
│                                                         │
│  3. SelectHook      → Claude                            │
│     └─ returns Hook + optional RunnerUp                 │
│                                                         │
│  4. DraftMessage    → Claude                            │
│     └─ returns OutreachDraft (status: pending_human_review) │
│                                                         │
│  5. AwaitReview     → no-op terminal stage              │
│                                                         │
│  Each stage writes to SQLite INCREMENTALLY              │
│  and publishes an SSE event to /runs/{id}/stream        │
└─────────────────────────────────────────────────────────┘
      │
      ▼
Browser (EventSource) — stage cards light up one at a time
```

**Persistence:** `modernc.org/sqlite` (pure Go, no CGO). Two tables:
- `runs` — one row per execution (prospect info, final status, hook, draft summary)
- `run_stages` — one row per stage (output JSON, reasoning, duration_ms)

**Streaming:** SSE broker with per-run history replay so clients that connect mid-run (or reconnect) receive all completed stages before any live events.

---

## Edge Cases — What Each One Demonstrates

### 1 · No Public Footprint (`02_no_footprint.json`)

**Trigger:** Search returns 0 usable results (small/private company, no press).

**What happens:**
- Research stage → `degraded`, reasoning: *"EDGE CASE — No public footprint: search returned 0 usable results..."*
- ExtractSignals stage → `degraded`, injects a synthetic `SignalType=general` signal
- Draft uses a role/industry angle ("reaching out to CROs in the logistics space…")

**What to say in the demo:**
> "When there's no public intel, the pipeline doesn't silently fail. It detects the gap, logs it explicitly in the reasoning trail, and falls back to a credible role/industry angle. The reviewer can see exactly why we used a generic hook instead of a timely one."

---

### 2 · Stale Signal (any prospect with old news)

**Trigger:** Extractor finds signals with `published_at` > 24 months ago.

**What happens:**
- Stale signals get `relevance_score -= 0.4` (min 0.1)
- Their summary is prefixed with `[STALE — recency check failed: published >24 months ago]`
- The `edge_cases.stale_signals_removed` field is populated
- The UI shows a blue **🕐 Stale signals detected** banner on the ExtractSignals card
- In the UI, stale signal rows are visually struck through

**What to say in the demo:**
> "The extractor applies an explicit recency check. Anything older than 24 months is penalised — not discarded silently — so a human reviewer can see exactly which signals were down-ranked and why. The pipeline still runs; it just leads with fresher evidence."

---

### 3 · Competing Signals (`03_competing_signals.json`)

**Trigger:** Two signals within 0.15 relevance of each other.

**What happens:**
- Selector is forced by its system prompt to name both candidates and explicitly justify why the winner beats the runner-up
- The Go stage code detects the narrow gap and appends *"EDGE CASE — Competing signals: X (0.82) vs Y (0.79), gap=0.03. See runner_up in output."* to the reasoning
- UI shows a **Runner-up (considered & rejected)** box below the winning signal card

**What to say in the demo:**
> "Most systems just pick a signal. Ours surfaces the trade-off: you can see the runner-up, its score, and exactly why the winner was chosen. In a real sales workflow, a rep might override this choice — that's intentional."

---

### 4 · Acquisition / Conflicting Company Info (`04_acquisition.json`)

**Trigger:** Research surfaces conflicting company names or an acquisition announcement.

**What happens:**
- Extractor detects that the company appears to have been acquired/rebranded
- Sets `edge_cases.conflicting_company` with a note like *"Company appears to have been acquired by IBM. Using the acquisition itself as a primary signal."*
- Adds an `acquisition`-type signal with high relevance (acquisition = strong buying trigger)
- Draft leads with the acquisition angle
- UI shows a purple **🔀 Conflicting company info** banner

**What to say in the demo:**
> "An acquisition is actually a golden hook — new leadership, new budget cycles, integration anxiety. Our pipeline doesn't get confused by the renamed entity. Instead it flags the conflict, uses the acquisition as the signal, and crafts an outreach that speaks to the transition."

---

## API Reference

```
POST   /runs                 Start a run → {run_id}
GET    /runs                 List all runs (dashboard)
GET    /runs/{id}            Full detail: all stages, reasoning, draft
GET    /runs/{id}/stream     SSE live stream (one event per stage)
GET    /runs/{id}/replay     SSE replay from DB — no API calls (offline demo)
POST   /runs/{id}/review     {"action": "approved" | "discarded"}
```

**Validation enforced on POST /runs:**
- `name`, `title`, `company` required; max 120 chars each
- `notes` max 1000 chars
- `linkedin_url` must be a valid `http/https` URL if provided
- Unknown JSON fields rejected (400)
- Rate limit: 10 requests/IP/minute (429)
- Body size limit: 1 MB

---

## 5-Minute Demo Script

> Assume server is running with real API keys. Load `http://localhost:8080` in a browser.

---

**[0:00 – 0:30] Dashboard overview**

Open `http://localhost:8080`. Show the dashboard — explain the columns (prospect, company, status, hook signal, review status). If you have pre-run fixtures loaded, point to each edge-case row.

---

**[0:30 – 1:30] Happy path — live run**

1. Click **+ New Run**
2. Enter Akshay Kothari / COO / Notion (or load `fixtures/01_happy_path.json` via curl)
3. Submit and watch the stage cards animate live:
   - **Research** → lights up, shows snippet count
   - **Extract Signals** → reveals 3-4 signals with relevance bars
   - **Select Hook** → winning signal highlighted, reasoning visible
   - **Draft Message** → full email appears in the card
   - **Await Review** → amber "Needs Human Review" card appears at the bottom
4. Click **Approve Draft** to demonstrate the review gate

> *"Five stages. Zero auto-sending. Every decision is logged with a reason."*

---

**[1:30 – 2:30] No-footprint edge case**

1. POST `fixtures/02_no_footprint.json` (or enter manually)
2. Point to the **Research** card → degraded state, amber banner
3. Point to **Extract Signals** → no-footprint banner, fallback signal shown
4. Point to **Draft** → role/industry angle used, not a timely hook
5. Click reasoning ▶ on ExtractSignals to show the exact text: *"EDGE CASE — No public footprint..."*

> *"The pipeline doesn't silently fail. It detects the gap, names it, and falls back gracefully. The reviewer sees exactly why this draft is less targeted."*

---

**[2:30 – 3:15] Competing signals**

1. POST `fixtures/03_competing_signals.json`
2. Wait for SelectHook to complete
3. Point to the **runner-up box** beneath the winning signal: *"Considered & rejected"*
4. Open the reasoning accordion — the LLM explains why funding beat product launch

> *"Most pipelines pick silently. Ours surfaces the trade-off so a human can override."*

---

**[3:15 – 4:00] Acquisition / conflicting company**

1. POST `fixtures/04_acquisition.json`
2. On the ExtractSignals card, point to the purple **🔀 Conflicting company info** banner
3. On SelectHook, point to the `acquisition`-type chip on the winning signal
4. Draft references IBM's acquisition of HashiCorp

> *"An acquisition is a buying trigger. The pipeline turns a potential point of confusion into the strongest possible hook."*

---

**[4:00 – 4:45] Offline replay**

1. Go back to dashboard
2. Click **⟳ Replay** on any completed run
3. Watch the stage cards animate exactly as in the live view, but with a purple **REPLAY** badge
4. No API calls made — all data from SQLite

> *"This lets me demo reliably without burning API quota or needing a network connection. Replay uses the exact same SSE event format as live streaming — the frontend is unchanged."*

---

**[4:45 – 5:00] Wrap-up talking points**

- **Auditable by design:** every reasoning string is stored in SQLite and surfaced in the UI
- **Fail-safe:** pipeline always produces a draft (degraded, not failed) unless search hard-errors
- **Human-in-the-loop enforced:** draft status is always `pending_human_review`; Approve button must be clicked explicitly
- **Production-ready patterns:** incremental DB writes, SSE broker with replay, rate-limiting, input validation, graceful shutdown

---

## POST a fixture via curl

```bash
# Windows PowerShell
Invoke-RestMethod -Uri http://localhost:8080/runs -Method POST `
  -ContentType "application/json" `
  -InFile "fixtures/01_happy_path.json"

# macOS / Linux
curl -s -X POST http://localhost:8080/runs \
  -H "Content-Type: application/json" \
  -d @fixtures/01_happy_path.json
```

Then open `http://localhost:8080` and click **View →** next to the new run.

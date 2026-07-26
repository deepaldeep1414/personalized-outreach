# Personalized Outreach Pipeline

A Go CLI that takes a prospect and runs a 5-stage pipeline: **Research → ExtractSignals → SelectHook → DraftMessage → AwaitReview** — producing a personalized cold-outreach draft grounded in a real, timely signal.

Every stage prints its output to stdout as it completes (not all at once at the end). The draft is always marked `pending_human_review` — nothing is ever sent automatically.

---

## Prerequisites

- Go 1.21+ (uses generics)
- LLM Provider Key (Choose ANY ONE — free options available):
  - [Groq API](https://console.groq.com/keys) — **100% FREE** (Llama 3.3 70B, ultra-fast, No credit card needed)
  - [Google Gemini API](https://aistudio.google.com/app/apikey) — **100% FREE** (Gemini 2.0 Flash, No credit card needed)
  - [OpenAI API](https://platform.openai.com/) — GPT-4o / GPT-4o-mini
  - [Anthropic API](https://console.anthropic.com/) — Claude
- Search API Key (Choose ANY ONE — free options available):
  - [Tavily Search API](https://app.tavily.com/) — **1,000 free searches/month** (No credit card needed)
  - [Serper Google Search API](https://serper.dev/) — **2,500 free searches** (No credit card needed)
  - [Brave Search API](https://api.search.brave.com/) — 2,000 free queries/month

---

## Setup

```powershell
# Windows (PowerShell)

# 1. Choose ANY ONE LLM provider (Groq & Gemini are 100% FREE):
$env:GROQ_API_KEY   = "gsk_..."   # Groq (Free Llama 3.3 70B: https://console.groq.com/keys)
# OR
$env:GEMINI_API_KEY = "AIza..."  # Google Gemini (Free: https://aistudio.google.com/app/apikey)
# OR
$env:ANTHROPIC_API_KEY = "sk-ant-..."

# 2. Choose ANY ONE search provider (Tavily & Serper are 100% FREE):
$env:TAVILY_API_KEY = "tvly-..."  # Tavily (1,000 free searches/mo: https://app.tavily.com/)
# OR
$env:SERPER_API_KEY = "..."       # Serper (2,500 free searches: https://serper.dev/)
```

```bash
# Unix/macOS
export GROQ_API_KEY="gsk_..."     # Or GEMINI_API_KEY or ANTHROPIC_API_KEY
export TAVILY_API_KEY="tvly-..."  # Or SERPER_API_KEY or BRAVE_API_KEY
```

---

## Usage

### Via flags

```bash
go run ./cmd/outreach \
  --name    "Sarah Chen" \
  --title   "VP of Engineering" \
  --company "Stripe" \
  --linkedin "https://linkedin.com/in/sarahchen" \
  --notes   "Met at SaaStr 2024"
```

### Via JSON file

```bash
go run ./cmd/outreach --file prospect.json
```

`prospect.json` format:
```json
{
  "name":        "Sarah Chen",
  "title":       "VP of Engineering",
  "company":     "Stripe",
  "linkedin_url": "https://linkedin.com/in/sarahchen",
  "notes":       "Met at SaaStr 2024"
}
```

### Dry-run (no API keys needed)

```bash
go run ./cmd/outreach --name "Alice Wang" --title "CTO" --company "Vercel" --dry-run
```

Dry-run uses the mock search provider but still calls Claude for the LLM stages if `ANTHROPIC_API_KEY` is set. If neither key is set, all stages use in-process mock providers.

### All flags

| Flag        | Description                                   | Default              |
|-------------|-----------------------------------------------|----------------------|
| `--name`    | Prospect's full name (required)               | —                    |
| `--title`   | Job title (required)                          | —                    |
| `--company` | Company name (required)                       | —                    |
| `--linkedin`| LinkedIn URL (optional)                       | —                    |
| `--notes`   | Additional context (optional)                 | —                    |
| `--file`    | JSON file path (alternative to flags)         | —                    |
| `--model`   | Claude model override                         | `claude-sonnet-4-5`  |
| `--output`  | `pretty` (default) or `json`                  | `pretty`             |
| `--dry-run` | Use mock providers instead of real API calls  | `false`              |
| `--verbose` | Print full reasoning text per stage           | `false`              |

---

## Web Server

```powershell
# With real API keys (Tavily, Serper, or Brave)
$env:ANTHROPIC_API_KEY = "sk-ant-..."
$env:TAVILY_API_KEY    = "tvly-..."   # Tavily (1,000 free searches/mo)
go run ./cmd/server

# Fully offline — mock providers, no keys needed
go run ./cmd/server --dry-run
```

Then open **http://localhost:8085** in your browser.

| Flag        | Default        | Description                              |
|-------------|----------------|------------------------------------------|
| `--port`    | `8085`         | HTTP listen port                         |
| `--db`      | `outreach.db`  | SQLite database file path                |
| `--model`   | _(env var)_    | Claude model override                    |
| `--dry-run` | `false`        | Use mock providers (no API keys needed)  |

### API

| Method | Path                    | Description                                    |
|--------|-------------------------|------------------------------------------------|
| POST   | `/runs`                 | Start a run; returns `{"run_id":"..."}`         |
| GET    | `/runs`                 | List all runs (dashboard data)                 |
| GET    | `/runs/{id}`            | Full run detail: all stages, reasoning, draft  |
| GET    | `/runs/{id}/stream`     | SSE stream — emits one event per stage         |
| GET    | `/runs/{id}/replay`     | SSE replay from DB (offline demo, no API calls)|
| POST   | `/runs/{id}/review`     | `{"action":"approved"\|"discarded"}`           |

### SSE event format

```json
{ "type": "stage", "index": 2, "total": 5, "stage": "ExtractSignals",
  "status": "ok", "output": {...}, "reasoning": "...", "duration_ms": 1840 }

{ "type": "done", "status": "completed" }
```

---

## Package Layout

```
personalized-outreach/
├── cmd/
│   ├── outreach/
│   │   └── main.go              # CLI entrypoint
│   └── server/
│       ├── main.go              # Web server entrypoint (go:embed static/)
│       └── static/
│           ├── index.html       # Dashboard page
│           ├── run.html         # New run form + live pipeline view
│           ├── style.css        # All styles
│           └── app.js           # All frontend JS (SSE, stage cards, review)
├── fixtures/                    # Demo fixture JSON files
│   ├── 01_happy_path.json
│   ├── 02_no_footprint.json
│   ├── 03_competing_signals.json
│   ├── 04_acquisition.json
│   └── README.md
├── internal/
│   ├── models/                  # Domain types & stage results
│   ├── pipeline/                # Pipeline orchestration & stage runners
│   ├── providers/
│   │   ├── claude/              # Claude Client, Extractor, Selector, Drafter
│   │   └── search/              # Tavily, Serper, Brave, and Mock searchers
│   ├── store/
│   │   └── store.go             # SQLite store — runs + run_stages tables
│   └── server/
│       ├── server.go            # Server struct & runPipeline goroutine
│       ├── handlers.go          # HTTP handlers (POST/GET /runs, SSE, replay, review)
│       └── sse.go               # SSE broker (per-run history + live fanout)
├── .env.example                 # Environment variables template
├── DEMO.md                      # Architecture diagram, edge cases & 5-min demo script
├── go.mod
└── README.md
```

---

## Error Handling

| Condition                         | Behaviour                                                        |
|-----------------------------------|------------------------------------------------------------------|
| Search returns 0 results          | `degraded` — injects generic role/company fallback signal        |
| LLM returns malformed JSON        | Retries once with stricter prompt; on 2nd failure → `degraded`   |
| No usable signal extracted        | Injects `SignalType = "general"` fallback                        |
| Claude API error                  | `degraded` for extractor/selector/drafter; fallback draft used   |
| Research API hard fail            | `failed` — pipeline stops with clear error message               |
| Missing API key at startup        | Fast-fail with helpful setup instructions before any network call |

---

## Adding Providers

All stages are backed by interfaces in `internal/pipeline/interfaces.go`:

```go
type Researcher     interface { Research(ctx, prospect) ([]string, error) }
type SignalExtractor interface { ExtractSignals(ctx, prospect, snippets) ([]Signal, error) }
type HookSelector   interface { SelectHook(ctx, prospect, signals) (Hook, error) }
type Drafter        interface { Draft(ctx, prospect, hook) (OutreachDraft, error) }
```

Wire your implementation in `cmd/outreach/main.go` → `pipeline.Config{}`.

---

## Features Built & Completed

- [x] Staged pipeline with channel-based SSE live streaming
- [x] SQLite persistence layer (`modernc.org/sqlite` pure Go)
- [x] Full Web Server (`net/http`) & dashboard UI with card-based stage view
- [x] Offline replay mode (`/runs/{id}/replay`)
- [x] 4 handled edge cases (No public footprint, Stale signals, Competing signals, Acquisition/rebrands)
- [x] Multiple search providers (Tavily, Serper, Brave, and Mock searchers)
- [x] Human-in-the-loop draft review gate (Approve / Discard)


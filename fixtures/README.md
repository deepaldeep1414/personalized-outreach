# Demo Fixtures

These JSON files can be POSTed directly to `POST /runs` for the live demo.

| File | Prospect | Edge Case Demonstrated |
|------|----------|------------------------|
| `01_happy_path.json` | Akshay Kothari, COO @ Notion | None — clean run with clear signals |
| `02_no_footprint.json` | Robert Sandoval, CRO @ MidStates Freight | No public footprint → role/industry fallback |
| `03_competing_signals.json` | Daniela Braga, VP GTM @ Anthropic | Near-tie signals → runner-up displayed |
| `04_acquisition.json` | Armon Dadgar, CTO @ HashiCorp | Acquired company → conflicting_company flag |

## Posting a fixture

```powershell
# PowerShell
Invoke-RestMethod -Uri http://localhost:8080/runs -Method POST `
  -ContentType "application/json" `
  -InFile "fixtures/01_happy_path.json"
```

```bash
# macOS / Linux
curl -s -X POST http://localhost:8080/runs \
  -H "Content-Type: application/json" \
  -d @fixtures/01_happy_path.json | jq .
```

After posting, open `http://localhost:8080` and click **View →** next to the new run.
To replay offline: click **⟳ Replay** on any completed run in the dashboard.

# DreadGOAD Web App

An agentic web UI to build, manage, reset, and validate DreadGOAD Active
Directory lab ranges. A chat pane (left) drives an LLM agent + a fixed set of
slash-commands; a live RangeView (right) shows the range topology and per-host
status/health. Each browser tab is an independent range/agent **session**.

Adapted from the ALFRED two-pane shell. The backend never reimplements cloud
logic — it shells out to the `dreadgoad` CLI for everything.

## Quick start

Prereqs: `python3` (3.10+), `node`/`npm`, and the `dreadgoad` Go binary (on
`PATH` or at `cli/dreadgoad`). Build the binary if needed:

```bash
cd cli && go build -o dreadgoad .
```

Set the LLM key (needed for the agent — free-text and agent-dispatch commands):

```bash
export OPENROUTER_API_KEY=sk-or-...
```

Launch from the repo root:

```bash
./dreadgoad-web            # build frontend, serve on http://localhost:7331
./dreadgoad-web --dev      # vite hot-reload + uvicorn --reload (development)
```

The launcher creates `.venv`, installs `webapp/backend/requirements.txt` (prefers
`uv`), builds the SPA, and serves it plus the API from one uvicorn process. Open
`http://localhost:7331`, create a session (point it at a `dreadgoad.yaml` + an
environment name), and drive it with `/` commands or plain text.

## Environment variables

| Variable | Default | Purpose |
|----------|---------|---------|
| `OPENROUTER_API_KEY` | — | LLM key for the agent. Reads work without it; agent turns don't. |
| `DREADGOAD_WEBAPP_PORT` | `7331` | HTTP port |
| `DREADGOAD_WEBAPP_MODEL` | `openrouter/anthropic/claude-sonnet-5` | Default agent model |
| `DREADGOAD_WEBAPP_STATE_ROOT` | `.dreadgoad/webapp/` | SQLite DB + per-session working dirs |
| `DREADGOAD_WEBAPP_DB` | `<state_root>/state.db` | Override the DB path |
| `DREADGOAD_WEBAPP_FRONTEND_DIST` | — | Static SPA dir (set by the launcher) |

## Commands

A message is either a slash-command or free text. Free text and **agent-dispatch**
commands go to the LLM agent (which runs the CLI via a constrained `run_dreadgoad`
tool); **direct-dispatch** commands run the CLI programmatically. The agent can
never run a direct command or raw cloud CLI — a safety property.

| Command | dreadgoad CLI | Dispatch | Purpose |
|---------|--------------|----------|---------|
| `/up` | `up` | 🤖 agent | Full bring-up (doctor→infra→provision→health) |
| `/provision` | `provision` | 🤖 agent | Re-run config playbooks |
| `/reset` | `lab reset` | 🤖 agent | Restore known-clean AD baseline |
| `/variant` | `variant generate` | 🤖 agent | Generate a randomized-name variant |
| `/extensions` | `extension list` / `provision` | 🤖 agent | List or provision extensions |
| `/score` | `score fetch` + `score --report` | 🤖 agent | Fetch an agent report off the attack box and score it |
| `/instances` | `lab status --json` | ⚡ direct | Cloud power state |
| `/health` | `health-check --json` | ⚡ direct | Per-host AD health (rendered as a table) |
| `/validate` | `validate` | ⚡ direct | Vuln-config correctness |
| `/diagnose` | `diagnose` | ⚡ direct | DC connectivity drill-down |
| `/start` | `lab start` | ⚡ direct | Power on |
| `/stop` | `lab stop` | ⚡ direct | Power off |
| `/scrub` | `score reset` | ⚡ direct | Clean agent artifacts |
| `/destroy` | `infra destroy` | ⚡ direct | Tear down infra (operator-only) |

Config/env are injected from the session — you never pass `--config`/`--env`.
Direct commands take no arguments; agent commands accept free-form text the agent
interprets into flags (e.g. `/up using the variant at ad/GOAD-foo`).

## Prompts

Agent prompt content lives as editable markdown in `backend/prompts/`:

- `system.md` — the shared system prompt (`$placeholder` template filled per session)
- `<command>.md` — optional per-command guidance injected when that command runs
  (flags pulled from the CLI source, not invented)

Adding guidance to a command is a drop-in file; no code change. Editing the system
prompt is just editing `system.md`.

## Architecture

```
webapp/
  frontend/   React + TypeScript + Vite SPA (@xyflow/react RangeView)
  backend/    FastAPI + a per-session LLM agent (dreadnode/rigging)
```

One multiplexed `/ws/chat` WebSocket carries a `session_id` per message; turns are
serialized per session and run concurrently across sessions. A post-command
**ingestion hook** runs `lab status --json` and overlays live cloud state onto the
config-seeded range topology (this is also where the attack box is discovered for
`/score`). State persists to SQLite (document model over JSON columns); **no
credentials are stored** — only config/env references.

### Backend modules

| Module | Responsibility |
|--------|---------------|
| `server.py` | FastAPI app, REST endpoints, `/ws/chat` |
| `chat.py` | Multiplexed WS runtime, `run_cli` pipeline, dispatch routing |
| `agent.py` | Per-session `LocalTaskAgent` + the constrained `run_dreadgoad` tool |
| `commands.py` | Slash-command registry, argv builder, prompt loader |
| `cli.py` | Subprocess runner (streaming + cancel; `capture` for JSON reads) |
| `hook.py` | Ingestion hook: instance→host overlay, health overlay, attack-box sync |
| `labconfig.py` | Snapshot derivation + range topology seeding from lab config |
| `sessions.py` | Session lifecycle service |
| `db.py` | SQLite persistence (single-worker executor, WAL) |
| `fetch.py` | `/score` report fetch via `dreadgoad score fetch` |
| `paths.py` | Filesystem locations (repo root, state root, session dirs) |

## Development & tests

```bash
./dreadgoad-web --dev          # hot-reload backend + frontend

# Backend tests (each suite is standalone-runnable, no pytest required):
.venv/bin/python webapp/backend/tests/test_commands.py
# ... test_db.py, test_labconfig.py, test_sessions.py, test_server_rest.py,
#     test_commands.py, test_hook.py, test_longops.py, test_chat.py, test_fetch.py

ruff format webapp/backend/ && ruff check webapp/backend/
pyright --pythonpath .venv/bin/python webapp/backend/

# Frontend:
cd webapp/frontend && npx tsc --noEmit && npm run build
```

The `dreadgoad` CLI is stubbed in tests — the web app is tested, not the CLI
(which has its own Go tests). Live behavior (real ranges) needs cloud credentials
and the compiled binary.

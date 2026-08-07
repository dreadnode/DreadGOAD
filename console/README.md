# DreadGOAD Console

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

Set the LLM key (needed for the agent — free-text and agent-dispatch commands).
Either export it before launch:

```bash
export OPENROUTER_API_KEY=sk-or-...
```

…or leave it unset and set it in-app: click the **⚙** in the tab bar and paste
your key (stored in the server's memory for the session, never written to disk).
A **⚠ no key** indicator shows in the tab bar until one is set.

Launch from the repo root:

```bash
./dreadgoad-console            # build frontend, serve on http://localhost:24749
./dreadgoad-console --dev      # vite hot-reload + uvicorn --reload (development)
```

The launcher creates `.venv`, installs `console/backend/requirements.txt` (prefers
`uv`), builds the SPA, and serves it plus the API from one uvicorn process. Open
`http://localhost:24749`, create a session (point it at a `dreadgoad.yaml` + an
environment name), and drive it with `/` commands or plain text.

## Environment variables

| Variable | Default | Purpose |
|----------|---------|---------|
| `OPENROUTER_API_KEY` | — | LLM key for the agent. Reads work without it; agent turns don't. |
| `DREADGOAD_CONSOLE_PORT` | `24749` | HTTP port |
| `DREADGOAD_CONSOLE_MODEL` | `openrouter/anthropic/claude-sonnet-5` | Default agent model |
| `DREADGOAD_CONSOLE_STATE_ROOT` | `.dreadgoad/console/` | SQLite DB + per-session working dirs |
| `DREADGOAD_CONSOLE_DB` | `<state_root>/state.db` | Override the DB path |
| `DREADGOAD_CONSOLE_FRONTEND_DIST` | — | Static SPA dir (set by the launcher) |

## Commands

A message is either a slash-command or free text. The **dispatch** column says
what happens when *you type it*: **agent** commands go to the LLM, which turns
your prose into flags and runs the CLI through its constrained `run_dreadgoad`
tool; **direct** commands run the CLI programmatically, with no model in the
loop.

| Command | dreadgoad CLI | Dispatch | Purpose |
|---------|--------------|----------|---------|
| `/up` | `up` | 🤖 agent | Full bring-up (doctor→infra→provision→health) |
| `/provision` | `provision` | 🤖 agent | Re-run config playbooks |
| `/reset` | `lab reset` | 🤖 agent | Restore known-clean AD baseline |
| `/variant` | `variant generate` | 🤖 agent | Generate a randomized-name variant |
| `/extensions` | `extension` | 🤖 agent | List available extensions, or provision one |
| `/score` | `score` | 🤖 agent | Fetch an agent report off the attack box and score it |
| `/exec` | `exec --json` | 🤖 agent | Run a script on named hosts via the cloud control plane |
| `/restart` | `lab restart-vm` | 🤖 agent | Reboot one host, leaving the rest of the range up |
| `/instances` | `lab status --json` | ⚡ direct | Cloud power state |
| `/health` | `health-check --json` | ⚡ direct | Per-host AD health (rendered as a table) |
| `/validate` | `validate` | ⚡ direct | Vuln-config correctness |
| `/start` | `lab start` | ⚡ direct | Power on |
| `/stop` | `lab stop` | ⚡ direct | Power off |
| `/scrub` | `score reset` | ⚡ direct | Clean agent artifacts (add `dry` to preview) |
| `/destroy` | `infra destroy` | ⚡ direct | Tear down infra (operator-only) |

`/help` is a sixteenth command that runs nothing: it prints the range workflow
end to end and is what an empty chat pane shows, so a new session opens on the
guide rather than a blank screen. It is client-side — it maps to no CLI verb,
and the agent cannot "run" it.

Config/env are injected from the session — you never pass `--config`/`--env`.
Agent commands accept free-form text the agent interprets into flags (e.g. `/up
using the variant at ad/GOAD-foo`); of the direct commands only `/scrub` takes
an argument.

### What the agent may run

The agent's tool can reach **every command in the table**, direct ones included,
so it can answer a question by running a read and act on a request in plain
English. Confirmation before something destructive is a prompt-level guarantee,
not a mechanical one — that is an operator's choice, and it is the reason
`system.md` matters.

Two limits *are* mechanical. The agent picks a command name and arguments; it
never picks the program, so it cannot invoke `az`, `aws`, `terraform` or a
shell. And `--config`/`--env` come from the session anchor and are rejected in
any supplied argument: cobra resolves repeated flags last-wins, so an appended
`--config other.yaml` would otherwise retarget the run at a different range.

## Prompts

Agent prompt content lives as editable markdown in `backend/prompts/`:

- `system.md` — the shared system prompt (`$placeholder` template filled per session)
- `<command>.md` — optional per-command guidance injected when that command runs
  (flags pulled from the CLI source, not invented)

Adding guidance to a command is a drop-in file; no code change. Editing the system
prompt is just editing `system.md`.

## Architecture

```
console/
  frontend/   React + TypeScript + Vite SPA (@xyflow/react RangeView)
  backend/    FastAPI + a per-session LLM agent (dreadnode/rigging)
```

One multiplexed `/ws/chat` WebSocket carries a `session_id` per message; turns are
serialized per session and run concurrently across sessions. A post-command
**ingestion hook** runs `lab status --json` and overlays live cloud state onto the
config-seeded range topology (this is also where the attack box is discovered for
`/score`). State persists to SQLite (document model over JSON columns); **no
credentials are stored** — only config/env references.

Node positions in the RangeView survive a reload: dragging a node saves through
`PUT /api/ranges/{id}/layout`, which carries a revision so a stale write from a
second tab is rejected with a 409 rather than overwriting the newer layout.

### Backend modules

| Module | Responsibility |
|--------|---------------|
| `server.py` | FastAPI assembly, lifecycle, router registration, frontend mount |
| `config_routes.py` | Health, configuration, settings, and command-catalog routes |
| `session_routes.py` | Session lifecycle and model-selection routes |
| `range_routes.py` | Range reads and revision-protected layout routes |
| `chat_socket.py` | Bounded WebSocket protocol and multiplexed chat transport |
| `chat.py` | Thin chat facade: turn dispatch, agent routing, model switching |
| `chat_runtime.py` | Per-session state, connection ownership, cancellation, cleanup |
| `chat_events.py` | Event formatting, persistence, WebSocket delivery, replay |
| `command_runner.py` | Shared CLI pipeline, streaming, hooks, and report overlays |
| `agent.py` | Per-session `LocalTaskAgent` + the constrained `run_dreadgoad` tool |
| `commands.py` | Slash-command registry, argv builder, prompt loader |
| `summary.py` | Condenses CLI output into bounded tool results (structured, else clipped with a marker) |
| `cli.py` | Subprocess runner (streaming + cancel; `capture` for JSON reads) |
| `hook.py` | Compatibility facade for post-command synchronization |
| `inventory_sync.py` | Instance→host overlay, cloud metadata, attack-box sync |
| `health_sync.py` | Health-report parsing and per-host health overlays |
| `topology_sync.py` | Range reseeding and extension-node discovery |
| `labconfig.py` | Snapshot derivation + range topology seeding from lab config |
| `sessions.py` | Session lifecycle service |
| `db.py` | SQLite persistence (single-worker executor, WAL) |
| `fetch.py` | `/score` report fetch via `dreadgoad score fetch` |
| `paths.py` | Filesystem locations (repo root, state root, session dirs) |

## Development & tests

```bash
./dreadgoad-console --dev          # hot-reload backend + frontend

# Backend tests (each suite is standalone-runnable, no pytest required):
.venv/bin/python console/backend/tests/test_commands.py
# ... test_chat.py, test_db.py, test_fetch.py, test_hook.py, test_labconfig.py,
#     test_longops.py, test_server_rest.py, test_sessions.py, test_summary.py

ruff format console/backend/ && ruff check console/backend/
pyright --pythonpath .venv/bin/python console/backend/

# Frontend:
cd console/frontend && npx tsc --noEmit && npm run build
```

The `dreadgoad` CLI is stubbed in tests — the console is tested, not the CLI
(which has its own Go tests). Live behavior (real ranges) needs cloud credentials
and the compiled binary.

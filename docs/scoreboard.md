# GOAD Scoreboard

`dreadgoad scoreboard` turns a GOAD lab into a live status board for an
engagement: it parses the lab's `config.json` into an answer key of
objectives, polls the agent's JSONL report, and verifies findings against
the key in a Bubbletea/Lipgloss TUI.

## Quick Start

```bash
dreadgoad scoreboard generate-key            # build answer_key.json once per lab
dreadgoad scoreboard run --report ./report.jsonl
dreadgoad scoreboard demo                    # preview the layout with mock findings
```

Point the agent at `/tmp/report.jsonl` using
[`scoreboard/agent_prompt.md`](../scoreboard/agent_prompt.md). For remote
reports use `--transport ssm` or `--transport ares` (see below).

## `scoreboard generate-key`

Builds the verification checklist (`answer_key.json`) from a GOAD
`config.json`. Each objective covers one provable finding (a password,
hash, kerberoastable SPN, ADCS template, ACL chain step, etc.), grouped
by category. Regenerate after lab edits or variant generation. The output
is gitignored.

| Flag        | Description                                                       |
|-------------|-------------------------------------------------------------------|
| `--config`  | Path to GOAD `config.json` (default `ad/GOAD/data/config.json`)   |
| `--output`  | Output path (default `scoreboard/answer_key.json`)                |

```bash
dreadgoad scoreboard generate-key
dreadgoad scoreboard generate-key --config ad/GOAD-variant-1/data/config.json
```

The command prints the total objective count and a per-group breakdown.

## `scoreboard run`

Polls the agent's JSONL report, verifies each finding against the answer
key, and renders the live board.

| Flag            | Default                          | Description                                                        |
|-----------------|----------------------------------|--------------------------------------------------------------------|
| `--transport`   | `local`                          | `local`, `ssm`, or `ares`                                          |
| `--report`      | `/tmp/report.jsonl`              | Path to the agent's report on the target                           |
| `--answer-key`  | `scoreboard/answer_key.json`     | Path to the answer key                                             |
| `--instance-id` |                                  | EC2 instance ID (required for `ssm` and `ares`)                    |
| `--ssm-region`  | falls back to `--region`         | AWS region for SSM                                                 |
| `--ares-binary` | `/usr/local/bin/ares`            | Path to the `ares` binary on the target                            |
| `--interval`    | `3s`                             | Poll interval                                                      |
| `--restart`     | `false`                          | Delete the report file on the target before starting               |
| `--once`        | `false`                          | Fetch and verify once, print the static board, exit (no TUI)       |

### Keybindings

The live TUI accepts the following keys (a subset is shown in the
footer hint when the board is not in compact mode):

| Key                       | Action                              |
|---------------------------|-------------------------------------|
| `q`, `ctrl+c`, `esc`      | Quit                                |
| `r`                       | Force an immediate re-poll          |
| `j` / `down`              | Scroll down one row                 |
| `k` / `up`                | Scroll up one row                   |
| `space`, `pgdown`, `ctrl+d` | Scroll down one page              |
| `pgup`, `ctrl+u`          | Scroll up one page                  |
| `g`, `home`               | Jump to top                         |
| `G`, `end`                | Pin to bottom (follows new findings) |

When the natural board layout would overflow the terminal height (e.g.
running in a short tmux pane), the TUI automatically switches to a
compact mode that drops blank spacers — the scroll keys above are how
you reach content that is below the viewport.

### Transports

- **`local`**: read a JSONL file from the host running the CLI. Best
  for development, or when the agent writes its report to a synced
  directory.
- **`ssm`**: read `/tmp/report.jsonl` (or `--report`) from an EC2
  instance over SSM. Requires the SSM agent, IAM, and `--instance-id`.
- **`ares`**: invoke an `ares` binary on the target to stream findings.
  Use when an agent writes findings through `ares` instead of a flat
  file. `--restart` is a no-op for this transport.

### Examples

```bash
# One-shot static board (CI/CD friendly)
dreadgoad scoreboard run --once --report ./report.jsonl

# SSM, fresh run (wipe the remote report first)
dreadgoad scoreboard run \
  --transport ssm \
  --instance-id i-0123456789abcdef0 \
  --restart

# Faster polling for short engagements
dreadgoad scoreboard run --interval 1500ms
```

## `scoreboard demo`

Generates a synthetic report against the current lab config and renders
the static board. Use it to preview the layout, sanity-check the answer
key, or screenshot the dashboard without running a real agent.

| Flag       | Description                                                     |
|------------|-----------------------------------------------------------------|
| `--config` | Path to GOAD `config.json` (default `ad/GOAD/data/config.json`) |

```bash
dreadgoad scoreboard demo
```

## Agent Report Format

The TUI consumes a JSONL stream: one header line followed by one finding
per line.

```json
{"agent_id": "dreadnode-agent", "start_time": "2026-05-11T17:00:00Z"}
{"target": "samwell.tarly@north.sevenkingdoms.local", "evidence": "Heartsbane1", "description": "password from AD description"}
```

[`scoreboard/agent_prompt.md`](../scoreboard/agent_prompt.md) is the
canonical spec and is suitable to hand to an agent verbatim.

## Related Documentation

- [`validation.md`](./validation.md): operator-side vulnerability validation
- [`GOAD-vulnerabilities-comprehensive.md`](./GOAD-vulnerabilities-comprehensive.md): vulnerability catalog the answer key is derived from

# DreadGOAD Scoreboard

Live status board that tracks agent progress against a GOAD Active Directory lab.

## Setup

```bash
pip install rich
```

Or from the scoreboard directory:

```bash
pip install -r requirements.txt
```

## Usage

### 1. Generate the answer key

```bash
./scoreboard/run.sh generate-key
```

This parses `ad/GOAD/data/config.json` and writes `scoreboard/answer_key.json`. Regenerate after changing the lab config.

### 2. Run the scoreboard

**Local (dev/testing):**

```bash
./scoreboard/run.sh run --transport local --report /tmp/report.jsonl
```

**AWS SSM (production):**

```bash
./scoreboard/run.sh run --transport ssm --instance-id i-0abc123 --profile lab
```

Options:

| Flag | Description |
|---|---|
| `--transport` | `local` or `ssm` (default: local) |
| `--instance-id` | EC2 instance ID (required for SSM) |
| `--profile` | AWS CLI profile |
| `--region` | AWS region |
| `--report` | Report file path on target (default: `/tmp/report.jsonl`) |
| `--interval` | Poll interval in seconds (default: 3) |
| `--restart` | Delete existing report file before starting |

### 3. Demo mode

Renders a sample board with mock findings — no agent or lab needed:

```bash
./scoreboard/run.sh demo
```

## Agent reporting

Include the contents of `agent_prompt.md` in your agent's system prompt. The agent appends JSONL findings to `/tmp/report.jsonl` on the target instance, which the scoreboard polls and verifies against the answer key.

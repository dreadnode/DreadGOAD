`/up` runs `dreadgoad up`, the one-command end-to-end bring-up. It runs the full
pipeline in order: doctor → infra → provision → health-check.

Flags (all optional — default is a clean full run, no args needed):
- `--from <step>`        resume from a step: `doctor`, `infra`, `provision`, or `health-check`
- `--skip-doctor`        skip the pre-flight doctor checks
- `--limit <hosts>`      limit provisioning to specific hosts
- `--plays <csv>`        comma-separated playbooks to run (default: all)
- `--module <name>`      target a specific infra module (default: all)
- `--exclude <csv>`      exclude infra modules (comma-separated)
- `--max-retries <n>`    provisioning retry attempts
- `--retry-delay <sec>`  delay between retries (seconds)

Guidance:
- A plain `/up` with NO args does the full clean bring-up — that's the common case.
- "resume from provisioning" / "pick up where it failed" → `--from provision`.
  "just redo infra" → `--from infra`. Map the operator's step name to the four
  valid values above; if theirs doesn't match, ask.
- `/up` deploys real cloud infra and costs money — if the range may already be up
  or the request is ambiguous, confirm intent before running.

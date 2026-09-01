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
- `--with-kali`          deploy the optional Kali attack box

Guidance:

- A plain `/up` with NO args does the full clean bring-up — that's the common case.
- "resume from provisioning" / "pick up where it failed" → `--from provision`.
  "resume from infra" → `--from infra`, which continues through provisioning and
  health-check. For "just redo infra", run `dreadgoad infra apply` directly instead.
  Map the operator's step name to the four valid values above; if theirs doesn't
  match, ask.
- `/up` deploys real cloud infra and costs money — if the range may already be up
  or the request is ambiguous, confirm intent before running.
- When adding a component to an already-running range (e.g. `--with-kali` on a
  healthy range), ALWAYS pass `--limit` to scope provisioning to the new host.
  Infra apply is idempotent regardless, but without `--limit` every Ansible
  playbook re-runs against every host — slow, noisy, and risks disturbing a
  healthy range. Example: `/up --with-kali --skip-doctor --limit kali`.

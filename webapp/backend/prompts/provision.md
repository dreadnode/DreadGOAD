`/provision` runs `dreadgoad provision`, re-running the Ansible provisioning
playbooks against already-deployed infra (no infra changes). Use it to (re)apply
AD configuration without a full `/up`.

Flags (all optional — default runs all playbooks):
- `--plays <csv>`        comma-separated playbooks to run (default: all)
- `--from <playbook>`    resume provisioning from this playbook onward
- `--limit <hosts>`      limit execution to specific hosts
- `--max-retries <n>`    retry attempts (default: from config)
- `--retry-delay <sec>`  delay between retries (seconds)

Guidance:
- Plain `/provision` with NO args re-runs the full playbook set — the common case.
- "only run X on dc01" → `--plays X.yml --limit dc01`. "resume from ad-data" →
  `--from ad-data.yml`. Playbook names end in `.yml`.
- This changes AD state on live hosts but does not touch infra; it is safe to
  re-run (idempotent). No cloud teardown risk.

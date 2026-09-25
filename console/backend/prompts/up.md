`/up` runs `dreadgoad up`, the one-command end-to-end bring-up. It runs the full
pipeline in order: doctor → infra → provision → health-check.

Flags (all optional — default is a clean full run, no args needed):

- `--from <step>`        resume from a step: `doctor`, `infra`, `provision`, or `health-check`
- `--from-playbook <yml>` resume provisioning from this playbook onward
- `--skip-doctor`        skip the pre-flight doctor checks
- `--limit <hosts>`      limit provisioning to specific hosts
- `--plays <csv>`        comma-separated playbooks to run (default: all)
- `--module <name>`      target a specific infra module (default: all)
- `--exclude <csv>`      exclude infra modules (comma-separated)
- `--max-retries <n>`    provisioning retry attempts
- `--retry-delay <sec>`  delay between retries (seconds)
- `--with-kali`          deploy the optional Kali attack box
- `--infra-only`         stop after infrastructure apply; skip Ansible and health

Guidance:

- A plain `/up` with NO args does the full clean bring-up — that's the common case.
- "resume from provisioning" / "pick up where it failed" → `--from provision`.
  "resume from infra" → `--from infra`, which continues through provisioning and
  health-check. For "just redo infra", run `dreadgoad infra apply` directly instead.
  Map the operator's step name to the four valid values above; if theirs doesn't
  match, ask.
- "resume from build.yml" → `--from provision --from-playbook build.yml`.
  Preserve `--from-playbook` when the operator supplies it; it is a valid `/up`
  flag and is distinct from the pipeline-level `--from` flag.
- `/up` deploys real cloud infra and costs money. If the request is ambiguous,
  clarify it first. Once intent and arguments are clear, call the command; the
  backend separately shows the exact argv and requires operator approval before
  it executes.
- Kali is infrastructure-managed and intentionally absent from the Ansible
  inventory. Never pass `--limit kali`: it cannot match and Ansible has nothing
  to configure on the attack box. For a fresh range, use `/up --with-kali` so
  the Windows lab and Kali are created together. To add Kali to an already
  healthy range, run only its infrastructure unit:
  `/up --with-kali --skip-doctor --infra-only --module kali`.
- For Ansible-managed components added to a healthy range, continue to use
  `--limit <inventory-host>` so existing hosts are not reprovisioned.

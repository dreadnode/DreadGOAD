`/reset` runs `dreadgoad lab reset`, restoring the lab to a known-clean AD
baseline. It is a two-stage reset: (1) purge unmanaged AD objects (users/
computers/groups not in the lab config), then (2) re-run the AD-state playbooks.

Flags (all optional — default runs both stages):

- `--skip-purge`         skip the unmanaged-object purge stage
- `--skip-provision`     skip the AD-state playbook stage
- `--plays <csv>`        comma-separated playbooks (default: the AD-state set)
- `--limit <hosts>`      limit playbook execution to specific hosts
- `--skip-creator-check` skip the Domain/Enterprise Admin creator-SID safety belt
- `--max-retries <n>`    retry attempts (default: from config)
- `--retry-delay <sec>`  delay between retries (seconds)

Guidance:

- Plain `/reset` with NO args does the full two-stage reset — the common case.
- "just re-apply AD state, don't delete anything" → `--skip-purge`. "only clean
  up stray objects" → `--skip-provision`.
- The purge DELETES AD objects an agent created. That is the point of a reset,
  but if the operator seems unsure whether they want data wiped, confirm first.
- Do NOT pass `--skip-creator-check` unless the operator explicitly asks — it
  disables a safety belt protecting privileged accounts.

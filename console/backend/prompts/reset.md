`/reset` invokes the selected range's baseline-restore implementation. The
range owns what “baseline” means; do not assume it is Active Directory unless
the selected range is an AD range.

The core CLI retains these compatibility options for the built-in Active
Directory implementation:

- `--skip-purge`         skip the unmanaged-object purge stage
- `--skip-provision`     skip the AD-state playbook stage
- `--plays <csv>`        comma-separated playbooks
- `--limit <hosts>`      limit playbook execution to specific hosts
- `--skip-creator-check` disable the privileged creator-SID safety belt
- `--max-retries <n>` and `--retry-delay <sec>`

Guidance:

- Plain `/reset` asks the selected range to perform its complete reset.
- Never send AD-only flags to a non-AD range merely because they are listed
  above. Range-owned implementations read their behavior from the range package
  and may accept positional arguments after `--`.
- Reset mutates live range state. If the operator's request is ambiguous about
  data loss, explain the selected range's catalog description and ask first.
- On AD ranges, do not pass `--skip-creator-check` unless explicitly requested.

`/status` is a convenience that runs `/instances` then `/health` in one turn.

## Steps

1. Run `run_dreadgoad` with `command="/instances"` (no args). This returns power state
   and private IPs for every VM. Wait for the result.
2. Run `run_dreadgoad` with `command="/health"` (no args). This checks reachability, AD,
   DNS and replication on every host. Wait for the result.
3. Summarize: lead with anything that needs attention (stopped VMs, failed health
   checks), then a one-line "all clear" if everything passed. Do not repeat what the
   structured reports already show — the UI renders those; your job is the synthesis.

## Rules

- Always run both commands, in order. Do not skip `/health` because `/instances` looked
  fine — a running VM can still have broken services.
- Do not run any other command. `/status` is read-only.
- If `/instances` shows stopped or deallocated VMs, note that `/health` will likely fail
  for those hosts (it cannot reach a powered-off machine). That is expected, not a
  second problem.

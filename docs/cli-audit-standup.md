# CLI audit — the range stand-up path

Findings from a review of the commands that build a range: `up`, `doctor`,
`infra`, `provision`. One issue is fixed; the rest are open and intended for a
follow-up branch.

Every finding below is in the **Go CLI**, not the web console. The console is a
pass-through that maps a slash command to an argv tuple, so it inherits all of
these. In two places it makes them worse, because it always invokes bare with no
flags — noted per finding.

Line numbers are against `d4d23a5`.

---

## Status

| # | Finding | Severity | Area |
|---|---------|----------|------|
| 1 | `up` could not deploy the Azure modules it depends on | — | **Fixed in `d4d23a5`** |
| 2 | `up --from provision` re-runs the entire playbook sequence | High | Correctness / UX |
| 3 | Flag lookup errors discarded everywhere | High | Systemic |
| 4 | Preflight failures downgraded to warnings | Medium | Correctness |
| 5 | `--max-retries 0` cannot express "no retries" | Medium | UX |
| 6 | Fixed 120s sleep after reboot playbooks | Medium | Efficiency |
| 7 | `context.Background()` throughout; no signal handling | Medium | Cancellation |
| 8 | Proxmox password passed on the terraform argv | **Security** | Secrets |
| 9 | Inventory files written world-readable | **Security** | Secrets |
| 10–14 | Smaller defects | Low | Various |

Not reviewed: `reset`, `scrub`, `exec`, `validate`, `score`, `variant`. Given the
hit rate in the four commands that were reviewed — one total break plus a billing
leak — these should not be assumed clean.

---

## 1. `up` could not deploy the Azure modules it depends on — FIXED

Recorded for context; fixed in `d4d23a5`.

`runUpInfraApply` drove `infra apply` through a hand-maintained synthetic
command that never carried `--with-bastion` / `--with-controller`. Those govern
the terragrunt `exclude{}` blocks, so both modules were skipped, and step 3
reaches the Windows hosts only over Bastion → controller → SOCKS5. Every Azure
`up` deployed the full range and then could not reach any of it.

The flags were added to `infra apply` in #161, three days after `up.go` was
written in #141, and were never forwarded. A missing flag reads back as `false`
with the lookup error discarded, so nothing complained — see finding 3.

Fixing it exposed a second defect: the destroy-time fallback that includes a
module whose directory exists covered only `kali`, while every `exclude{}` block
uses `actions = ["all"]`. Since the console's `/destroy` runs `infra destroy`
bare, a Bastion deployed by `up` would have survived teardown and kept billing.

---

## 2. `up --from provision` re-runs the entire playbook sequence

**Severity:** High — wastes ~13 minutes of unconditional waiting per resume, and
the hint that produces it claims to do the opposite.

### Evidence

`up.go:112` prints on any step failure:

```
Resume with: dreadgoad up --from provision
```

`runUpProvision` registers `from` at `up.go:210` and never sets it, so
`resolvePlaybooks` receives `""` and returns the full list.

Measured against the real `playbooks.yml`:

```
full sequence:                       18 playbooks, network_setup.yml → vulnerabilities.yml
synthetic provision cmd:             plays="" from=""
`up --from provision` runs:          18 playbooks, starting at network_setup.yml
`provision --from ad-data.yml` runs: 10 playbooks, starting at ad-data.yml
```

The resume hint is byte-identical to a cold start.

### Impact

Of the 18 replayed playbooks, 4 are in `config.RebootPlaybooks`
(`ad-parent_domain`, `ad-child_domain`, `ad-members`, `ad-trusts`), each
followed by a 120s sleep at `provision.go:419` — 8 minutes. Plus `wait5m.yml`,
another 5. So ~13 minutes of pure `time.Sleep` before any real work, every time
the operator follows the hint.

There is also no way to express a real resume through `up` at all: `up --from`
is claimed by the step name, so the operator must drop to
`dreadgoad provision --from <playbook>` and lose the health-check step.

### Proposed fix

1. **Make the failure structured.** `provision.go:407` already knows the failing
   playbook and buries it in a formatted string. Replace with a
   `provisionFailure` error type carrying `Playbook` and `LogFile`, recoverable
   via `errors.As`.
2. **Make the hint precise.** When the provision step fails, print the command
   that actually resumes:
   ```
   Resume with: dreadgoad up --from provision --from-playbook ad-data.yml
   ```
   Other steps keep the current hint, which is already correct.
3. **Add `--from-playbook` to `up`**, forwarded to provision's `--from`. This is
   what makes the hint real.
4. **Validate rather than ignore.** `--from-playbook` combined with a `--from`
   that skips the provision step is an error, not a silent no-op (this is
   finding 14). Mutually exclusive with `--plays`, mirroring
   `provisionCmd.MarkFlagsMutuallyExclusive("plays", "from")`.

**Open design question:** flag naming. `up --from` means *step*, `provision
--from` means *playbook* — the collision that caused this. `--from-playbook`
reads well beside the existing flag. The alternative is a compound value
(`--from provision:ad-data.yml`), which avoids a second flag but invents syntax.
Recommendation: `--from-playbook`.

### Tests

- Given a `provisionFailure` at playbook N, `up` prints a hint naming N.
- `up --from provision --from-playbook X` resolves to the suffix starting at X,
  not the full list.
- `--from-playbook` with a step list excluding provision is rejected.

**Effort:** ~half a day including tests.

### Caveat

The re-run is wasteful; whether it is also *risky* is unverified. Ansible is
generally idempotent, but domain-controller promotion replayed against an
already-promoted DC is worth confirming before treating this as cost-only.

---

## 3. Flag lookup errors discarded everywhere

**Severity:** High — this is the class of bug that produced finding 1, and it
will produce the next one.

### Evidence

The pattern throughout `infra_cmd.go` and `up.go`:

```go
module, _  := cmd.Flags().GetString("module")
autoApprove, _ := cmd.Flags().GetBool("auto-approve")
```

`GetBool` on an unregistered flag returns `(false, error)`. With the error
discarded, **a flag that does not exist is indistinguishable from one left at
its default.** Finding 1 survived three months because of exactly this.

### Proposed fix

Not a blanket refactor — the errors are genuinely uninteresting when the flag is
registered. Two targeted measures:

1. **A `mustBool`/`mustString` helper** that treats a lookup failure as the
   programming error it is (panic in test builds, or a returned error naming the
   flag). Apply at the `runInfraAction*` read sites, which are the ones driven by
   synthetic commands.
2. **Structural tests** asserting that any synthetic command's flag set is a
   superset of the real command's. One such test now exists for `up`
   (`TestUpInfraCommandForwardsEveryFlag`); the same shape applies to
   `runUpProvision` and any future synthetic caller.

The second measure is the higher-value one: it catches drift at the point of
divergence rather than at the point of use.

**Effort:** ~half a day.

---

## 4. Preflight failures downgraded to warnings

**Severity:** Medium — converts a definitive stop into a confusing failure hours
later.

### Evidence

`provision.go:163-170`:

```go
if isSSMInventory(cfg) {
    if err := ensureInventorySynced(ctx, cfg); err != nil {
        slog.Warn("inventory sync check failed", "error", err)   // :165
    }
    if err := generateInstanceMapping(ctx, ""); err != nil {
        slog.Warn("instance mapping generation failed, ...", "error", err)  // :168
    }
}
```

`ensureInventorySynced` can return `no running instances found for env=%s`
(`provision.go:288`) — a definitive stop condition — and it is swallowed.
Provisioning then proceeds against stale instance IDs and fails much later
inside Ansible with an error that does not point at the cause.

### Console amplification

In a terminal an operator may notice a `slog.Warn` scroll past. In a web
transcript, buried in thousands of lines of Ansible output, they will not.

### Proposed fix

Separate the two cases rather than warning on both:

- **Fatal:** no live instances for the environment; inventory parse failure;
  provider construction failure. These mean provisioning cannot succeed.
- **Warn and continue:** instance-mapping generation failure — the playbooks
  have a documented runtime-detection fallback, so this is genuinely advisory.

`generateInstanceMapping` stays a warning; `ensureInventorySynced` becomes an
error, with the message naming the remedy (`dreadgoad infra apply`, or
`dreadgoad inventory sync`).

### Tests

- A provider reporting zero instances aborts before any playbook runs.
- A mapping-generation failure does not abort.

**Effort:** ~2 hours.

---

## 5. `--max-retries 0` cannot express "no retries"

**Severity:** Medium — a documented flag silently does nothing at one value.

### Evidence

`up.go:220` and `provision.go:399,402` both guard on `> 0`:

```go
if maxRetries > 0 { opts.MaxRetries = maxRetries }
if retryDelay > 0 { opts.RetryDelay = ... }
```

So `--max-retries 0` means "use the config default" (3), not "do not retry".
Retries cannot be disabled, which matters when debugging a reproducible failure
— each retry multiplies the wait.

### Proposed fix

Use `cmd.Flags().Changed("max-retries")` to distinguish "not passed" from
"passed as 0". This requires threading the `*cobra.Command` (or an explicit
`*int`) into `provisionPlaybooks`, which currently takes plain ints.

Cleanest shape: change the signature to take a small `provisionOptions` struct
with `MaxRetries *int` / `RetryDelay *int`, nil meaning "unset".

### Tests

- `--max-retries 0` results in zero retry attempts.
- Omitting the flag preserves the config default.

**Effort:** ~2 hours; touches `lab reset`, which shares `provisionPlaybooks`.

---

## 6. Fixed 120s sleep after reboot playbooks

**Severity:** Medium — simultaneously too short and too long.

### Evidence

`provision.go:416-420`:

```go
if useSSM && slices.Contains(config.RebootPlaybooks, playbook) {
    log.Info("playbook may have caused reboots, waiting for SSM reconnection", ...)
    time.Sleep(120 * time.Second)
}
```

An unconditional guess: wasted time when agents reconnect in 20s, and
insufficient on a slow or loaded region — where it produces a confusing
downstream connection failure rather than a clear timeout. It is also not
context-aware, so it ignores cancellation (see finding 7).

### Proposed fix

Poll instead of sleep. The SSM API already reports agent ping status; wait until
every host in the inventory reports online, with a ceiling (say 5 minutes) and a
clear timeout error naming the hosts that never came back.

```go
if err := ansible.WaitForSSMReconnect(ctx, cfg.Env, hosts, 5*time.Minute); err != nil {
    return fmt.Errorf("hosts did not reconnect after %s: %w", playbook, err)
}
```

Typically returns in well under 120s, and when it does not, it says which host
is the problem instead of failing later somewhere unrelated.

### Tests

- Returns as soon as all hosts report online (fake provider).
- Times out with the offline hosts named.
- Honours context cancellation.

**Effort:** ~half a day.

---

## 7. `context.Background()` throughout; no signal handling

**Severity:** Medium — cancellation works by process-group kill, not by design.

### Evidence

`provision.go:223,324`; `infra_cmd.go:252,333,388,437,453,489`. Only
`health_check.go` and `ami.go` install a signal handler
(`signal.NotifyContext`).

`up` calls `SetContext(cmd.Context())` on both synthetic commands — dead code,
since nothing downstream reads it.

### Why it appears to work today

The console SIGINTs the whole process group (`cli.py`, `_killpg`), so terragrunt
and ansible do receive the signal and unwind. But the Go process itself has no
handler, so Go's default terminates it immediately **without running deferred
cleanup** — `defer socksTunnel.Close()` in `provisionPlaybooks` never runs. The
tunnel and the `az network bastion tunnel` child are cleaned up only because
they happen to share the process group.

### Impact

A cancel mid-`terraform apply` may leave a state lock. The console escalates to
SIGKILL after a 12s grace (`cli.py: _KILL_GRACE`), which is not long enough for
a large terragrunt run to release cleanly. There is no `force-unlock` guidance
anywhere in the CLI or the agent's prompt.

### Proposed fix

1. Install `signal.NotifyContext` in `runUp` and use it for every step.
2. Thread that context through `runInfraAction*` and `provisionPlaybooks`
   instead of `context.Background()`.
3. Add a state-lock recovery hint: when terragrunt fails with a lock error,
   print the `force-unlock` command with the lock ID.

Item 3 is independently valuable and much cheaper than 1–2.

**Effort:** 1–2 days for the full threading; ~2 hours for item 3 alone.

---

## 8. Proxmox password passed on the terraform argv — SECURITY

**Severity:** Security (moderate; local disclosure).

### Evidence

`infra_cmd.go:380`:

```go
opts.Vars = append(opts.Vars, "pm_password="+password)
```

`terraform/runner.go:92-94` turns each into `-var pm_password=<secret>`, so the
plaintext password appears in the process argument list — readable by any local
user via `ps`.

The comment says this avoids storing the secret in rendered files. It does not
remove the secret; it relocates it from a file to the process table, which is
world-readable on Linux and not obviously the safer of the two.

### Proposed fix

Pass it through the child environment instead. Terraform reads `TF_VAR_<name>`
natively:

```go
opts.ExtraEnv = append(opts.ExtraEnv, "TF_VAR_pm_password="+password)
```

Environment is per-process and not exposed via `ps` on Linux. Requires adding
`ExtraEnv` to `terraform.Options` — `terragrunt.Options` already has it.

### Tests

- No argument matching `pm_password=` appears in the built argv.
- The variable is present in the child environment.

**Effort:** ~1 hour.

---

## 9. Inventory files written world-readable — SECURITY

**Severity:** Security (moderate; local disclosure).

### Evidence

`provision.go:214` and `provision.go:253` both write `0o644`. Also
`infra_cmd.go:124` for the materialized lab config.

Live inventories carry credentials — the inventory at the repo root contains 5
credential-bearing lines. `0644` makes them readable by every local account.

### Proposed fix

Write `0o600`. These are per-operator files that only the CLI and Ansible read.
Also consider `0o600` for the materialized lab config, which may embed
per-environment secrets depending on the overlay.

Worth checking whether any existing inventory needs a one-time `chmod`; a note
in the release notes is cheaper than migration code.

### Tests

- A bootstrapped inventory has mode `0600`.

**Effort:** ~30 minutes.

---

## 10–14. Smaller defects

**10. `materializeLabConfig` swallows the resolution error** (`infra_cmd.go:109`)

```go
resolved, err := cfg.ResolvedLabConfigPath()
if err != nil {
    return nil // no config to materialize -- let terragrunt surface the error
}
```

Deliberate, but it means a config typo surfaces as a terragrunt error rather
than a config one. It also calls `os.WriteFile` (`:124`) without `MkdirAll`, so
a missing `ad/GOAD/data/` directory produces a bare write error. *Fix:* keep the
pass-through but log at debug; add `MkdirAll`.

**11. `ensureVariant` treats any `Stat` error as "exists"** (`provision.go:109`)

```go
if _, err := os.Stat(target); !os.IsNotExist(err) {
```

A permission error is read as "already generated" and generation is skipped. It
also never validates that an existing directory is a *complete* generation, so a
half-written variant from an interrupted run is used as-is. *Fix:* handle the
error explicitly; add a completeness marker written at the end of generation.

**12. Doctor advertises `--skip-doctor` as the remedy** (`up.go:155`)

The failure message for failed preflight checks suggests bypassing preflight.
*Fix:* point at `dreadgoad doctor` for detail and reserve the bypass mention for
cases the operator has already diagnosed.

**13. `up` has no `--deployment`**

`resolveDeployment` reads the flag, but `up`'s synthetic command always passes
`""`, so `up` can only ever build `cfg.Infra.Deployment`. *Fix:* add the flag and
forward it; covered by the structural test from finding 3.

**14. `up` does not validate flags against the steps that will run**

`up --from health-check --limit dc01` silently ignores `--limit`, because the
provision step never runs. *Fix:* reject flags that only apply to a skipped
step. Folded into finding 2.

---

## Suggested order

Ranked by (risk × likelihood) ÷ effort, not by severity alone.

1. **9** — inventory permissions. 30 minutes, removes a credential exposure.
2. **8** — Proxmox password on argv. 1 hour, same category.
3. **4** — preflight warnings. 2 hours, and it is the one most likely to be
   costing debugging time today.
4. **3** — the structural tests half. Cheap, and prevents the next finding-1.
5. **2** — resume semantics. Half a day, needs the naming decision first.
6. **6** — poll instead of sleep. Half a day, pure time savings.
7. **5** — retry flags. 2 hours, touches `lab reset`.
8. **7** — context threading. Largest; do item 3 (lock hint) early and
   separately.
9. **10–14** — opportunistically alongside the above.

## Before starting

Two things are worth settling first:

- **The naming decision in finding 2** (`--from-playbook` vs a compound
  `--from` value). Everything else in that finding follows from it.
- **How often provisioning actually fails partway.** It sets the real priority
  of finding 2 relative to finding 4, and it is not knowable from the code.

Findings 1 and 2 were confirmed by execution against the real repo, not by
reading alone. Findings 3–14 are code review, verified by reading the current
source and grepping call sites, but not reproduced at runtime.

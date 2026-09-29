# DreadGOAD Console security model

The DreadGOAD Console is a local, single-operator control plane for deliberately
vulnerable training ranges. It can create cloud resources, run administrator-level
commands on range hosts, and destroy environments. Treat access to the console as
equivalent to access to the cloud and host credentials available to the process
that launched it.

This document describes the console's security boundary. It does not describe
the security posture of the intentionally vulnerable ranges; use `/secure` for
the selected range's network exposure and access-control checks.

## Supported deployment model

The supported configuration is one trusted operator running
`./dreadgoad-console` on a trusted workstation. The launcher binds Uvicorn and,
in development mode, Vite to loopback. It does not provide TLS, user accounts,
roles, or tenant isolation.

Do not expose the console through a public bind, port forward, reverse proxy,
shared development host, or browser-based remote-access service. Adding TLS to
a proxy does not turn the console into a multi-user service: one launch token
grants full access to every session and operation.

## Trust boundaries

| Boundary | Enforced control | Remaining trust |
|---|---|---|
| Browser to HTTP API | A fresh launch token is required as a bearer credential on every `/api/` request. | Anyone with the token has full console authority. |
| Browser to WebSocket | The token is required in the WebSocket subprotocol offer; browser origins must be loopback. Frames, field sets, identifiers, and content sizes are bounded. | The same token authorizes every console session. Non-browser clients may omit `Origin` but still need the token. |
| Model to local execution | The model has no shell. Its tool selects from an allowlist of `dreadgoad` commands; the backend injects the session config/environment, rejects selector overrides, validates `/exec`, and confines model-selected local paths. Subprocesses use argv, not a shell command string. | Approved commands still operate with the launcher's cloud and host credentials. Model and command output remain untrusted input. |
| Range content to agent | Range-authored guidance can describe the range but cannot add tools, commands, or bypass backend policy. | A checkout, imported config, playbook, module, inventory, or CLI binary is executable operational input and must be trusted. |
| State on disk | State directories use `0700`; SQLite and managed config files use `0600`; non-regular database paths are rejected. | The launching OS user and privileged local users can read the data. Disk encryption and backups are outside the console boundary. |

## Authentication and browser handling

Each launcher run creates a new 256-bit token. The launch URL carries it in the
URL fragment, which is not sent in the HTTP request. The frontend copies the
token into tab-scoped `sessionStorage`, removes it from the visible URL, sends
it in the HTTP `Authorization` header, and offers it as a WebSocket subprotocol
credential. The backend uses constant-time comparisons and removes the console
authentication environment variable before it starts CLI/cloud subprocesses.

The token is not an identity and there is no per-session authorization. Do not
share the launch URL, terminal output containing it, browser profile, or an
authenticated tab. Restart the console to invalidate a token that may have been
exposed.

The frontend renders user and command fields through React. Agent Markdown is
rendered without raw HTML support. The backend does not enable cross-origin
resource sharing, and browser WebSocket handshakes accept only loopback origins.
FastAPI responses and Vite's development/preview responses also deny framing
through CSP `frame-ancestors` and `X-Frame-Options`, suppress referrer data, and
disable MIME sniffing.

## Command authority and approvals

The backend requires a separate, exact, single-use approval for `/up` and
`/destroy`. The approval displays the final argv, belongs to one session,
expires after five minutes, and fails closed on denial, cancellation, timeout,
shutdown, or error.

That approval boundary is intentionally narrower than "all commands that can
change state." `/provision`, `/reset`, `/start`, `/stop`, `/restart`, `/exec`,
`/scrub`, `/variant`, and extension provisioning can change hosts, files, or
cloud state without an additional backend approval. Their prompts and UI copy
help the operator make the right decision, but prompt instructions are not a
security boundary. Review the selected session and exact command before asking
the agent to act. `/exec` is administrator-level remote execution and has no dry
run. Allowing agent-initiated `/exec` without repeated approvals is an accepted
usability tradeoff for iterative troubleshooting, not a claim that model intent
is trustworthy. It depends on disposable, network-isolated range hosts and
least-privilege cloud credentials.

Cancellation stops the console-owned local process tree. It cannot retract a
cloud operation already accepted by a provider or a remote action already
started on a host.

## Secrets and retained data

The provider-native LLM API key selected by the launcher or entered in Settings
is kept in the backend process environment and is not returned by the API or
written to the console database. `OPENROUTER_API_KEY` remains the default for
the default OpenRouter model; other providers retain their own variable names.
The launcher registers both that default and the selected provider variable,
then removes their values before dependency, build, and frontend helpers run.
Every registered LLM credential is also removed from the environment passed to
CLI, cloud CLI, Terraform, Ansible, and other backend subprocesses.

Cloud credentials are not collected or managed by the console. Provider tooling
uses the ambient credentials available to the launcher through environment
variables, profile selectors, configuration files, and provider CLI caches. The
console does not persist, refresh, rotate, or revoke them; `/login` delegates to
`aws` or `az`. Session snapshots deliberately retain selectors, not credential
values.

The SQLite database does retain session metadata, config paths, cloud account
and resource identifiers, private/public IP addresses, chat messages, model
output, tool arguments/results, and bounded command output. Per-session working
directories may contain fetched reports and model-created files. Managed config
files can contain provider-specific secrets if an operator adds them manually.

Deleting a session removes its database records and working directory. It does
not delete an attached config or deployed infrastructure. `/destroy --purge`
removes local artifacts only for a console-managed, single-environment config
whose ownership markers match.

## Hardening checklist

- Run the console only from a trusted checkout and with a trusted
  `cli/dreadgoad` binary.
- Keep the HTTP and Vite listeners on loopback. Do not publish or proxy them.
- Use a dedicated browser profile and close authenticated tabs when finished.
- Give the launcher least-privilege cloud credentials scoped to the training
  account/subscription and expected regions.
- Treat imported configs, range guidance, playbooks, Terraform/Terragrunt
  modules, inventories, extensions, and fetched reports as trusted operational
  inputs or untrusted data according to how the CLI uses them.
- Review `/up` and `/destroy` approval argv carefully. Treat `/exec`, reset,
  scrub, provisioning, variant generation, and extension provisioning as
  equally consequential even though they have no second approval dialog.
- Protect `.dreadgoad/console/`, cloud CLI caches, SSH keys, terminal scrollback,
  backups, and the workstation with OS-level access controls and disk encryption.

## Known residual risks

- CLI, cloud CLI, Terraform, Ansible, and helper subprocesses inherit the
  backend environment after registered LLM credentials are removed. Other
  launcher secrets are not classified or scrubbed automatically because broad
  suffix rules would also remove required infrastructure credentials. Run the
  console with a minimal environment and only trusted binaries and automation.
- Only `/up` and `/destroy` have a mechanical second approval. Other mutating
  commands rely on authenticated operator intent and agent prompt policy.

## Security verification

Run the console's focused security and regression checks from the repository
root:

```bash
ruff check console/backend/
pyright --pythonpath .venv/bin/python console/backend/*.py

uv run --no-project --with-requirements console/backend/requirements.txt \
  --with pytest --with pytest-asyncio --with httpx \
  python -m pytest console/backend/tests/ -q -o asyncio_mode=auto

cd console/frontend
npm audit --omit=dev
npm run build
npm run test:auth
```

Also review the branch with Semgrep (the repository CI uploads SARIF to GitHub
Security) and inspect dependency advisories for both runtime and build-time
packages. A clean production audit does not make build-tool advisories
irrelevant; it only changes their reachability.

## Reporting a vulnerability

Do not include launch tokens, API keys, cloud credentials, SSH keys, range
passwords, or sensitive logs in a public issue. Use the repository's private
GitHub security-advisory reporting flow when available. If private reporting is
not available, contact a maintainer without including secrets and coordinate a
secure channel for reproduction details.

# Add a new lab

To create a new lab, create a folder in `ad/` with the lab name. The `dreadgoad` CLI automatically discovers labs by scanning the `ad/` directory (use `dreadgoad lab list` to verify your lab is detected).

## Directory structure

Create the following structure inside `ad/<lab_name>/`:

```text
ad/<lab_name>/
    range.yml                       # range kind, creation policy, lifecycle hooks
    data/
        config.json                 # JSON containing all the lab information
        inventory                   # global lab inventory file with the VM groups and the main variables
        inventory_disable_vagrant   # inventory to disable/enable vagrant user
    files/                          # extra files needed during provisioning (scripts, templates, etc.)
    commands/                       # semantic handlers; health required for service ranges
        health
        validate
        score
        init-score
        reset
        scrub
    prompts/
        agent.md                    # optional range-specific model guidance
    providers/
        aws|azure|proxmox/          # Terraform/Terragrunt based providers
            inventory               # inventory specific to the provider
            linux.tf                # linux VMs
            windows.tf              # windows VMs
        ludus/                      # Ludus provider
            inventory               # inventory specific to the provider
            config.yml              # Ludus configuration file
        virtualbox|vmware/          # Vagrant based providers
            inventory               # inventory specific to the provider
            Vagrantfile             # VM definitions
    scripts/                        # PowerShell or other scripts used by Ansible roles
```

## config.json format

The `config.json` file in `data/` defines all lab hosts, their domains, users, groups, vulnerabilities, and security settings. Required top-level structure:

```json
{
  "lab": {
    "hosts": {
      "<host_id>": {
        "hostname": "short hostname",
        "type": "dc|server",
        "local_admin_password": "strong password",
        "domain": "example.local",
        "path": "DC=example,DC=local",
        "local_groups": {
          "Administrators": ["domain\\user"],
          "Remote Desktop Users": ["domain\\group"]
        },
        "scripts": ["script.ps1"],
        "vulns": ["vuln_name"],
        "security": ["security_feature"],
        "security_vars": {}
      }
    }
  }
}
```

Key fields per host:

| Field | Required | Description |
|-------|----------|-------------|
| `hostname` | Yes | Short hostname for the VM |
| `type` | Yes | `dc` for domain controller, `server` for member server |
| `domain` | Yes | FQDN of the AD domain this host belongs to |
| `path` | Yes | LDAP distinguished name path |
| `local_admin_password` | Yes | Local administrator password |
| `local_groups` | No | Local group memberships |
| `scripts` | No | PowerShell scripts to execute on the host |
| `vulns` | No | Vulnerability configurations to apply |
| `security` | No | Security hardening features to enable |

See `ad/GOAD/data/config.json` for a complete reference example.

### Environment overlays

Rather than maintaining full config copies per environment, create small
overlay files (`{env}-overlay.json`) that contain only the fields that
differ from the base `config.json`. The CLI merges them at runtime using
RFC 7386 JSON Merge Patch. See `docs/cli.md` in the repository for the overlay format and resolution order.

Arrays replace, they do not merge. If an overlay redeclares a host's `vulns`,
that list becomes the complete set for the environment and the base list is
discarded, so adding a vuln to `config.json` alone does nothing wherever an
overlay names that host. The failure is silent in both directions: nothing
dangles, so a referential check sees a valid document, and `vulnerabilities.yml`
just never includes the missing role, leaving `PLAY RECAP` reporting `failed=0`.

After adding a vuln to any `config.json`, add it to every `{env}-overlay.json`
that redeclares that host. `TestLabConfigIntegrity` enforces this via
`CheckOverlayDrops`; a deliberate removal goes in
`cli/internal/labconfig/testdata/known_findings.txt` with a reason.

## Inventory files

The `data/inventory` file is an Ansible inventory that defines host groups and connection variables (WinRM settings, credentials). Each provider also has its own `inventory` file under `providers/<provider>/` that overrides connection-specific values (IP addresses, ports) for that provider.

The `data/inventory_disable_vagrant` inventory is used by the `disable_vagrant.yml` and `enable_vagrant.yml` playbooks to manage the vagrant user on VMs.

## Provider-specific files

- **Terraform/Terragrunt providers** (aws, azure, proxmox): Include `windows.tf` and optionally `linux.tf` files that define the VMs as Terraform resources. Infrastructure modules live in `infra/` and are invoked via Terragrunt.
- **Vagrant providers** (virtualbox, vmware): Include a `Vagrantfile` that defines VM resources, networking, and linked clones.
- **Ludus provider**: Uses a `config.yml` that describes the VMs in Ludus format.

## Lab discovery

The CLI discovers labs automatically by scanning the `ad/` directory. No
additional central registration step is needed. The lab name is derived from the
directory name.

To make a range available in the web console's **Create a new environment**
workflow, declare the provider scaffolding contract in `range.yml`:

```yaml
schema_version: 1
display_name: Example Range
kind: service-range                 # or active-directory
variants:
  supported: false
agent:
  prompt: prompts/agent.md
commands:
  health:
    protocol: health/v1
    description: Check the example services
    handler:
      type: executable
      path: commands/health
  validate:
    protocol: validate/v1
    description: Validate the example range's expected state
    handler:
      type: executable
      path: commands/validate
  score:
    protocol: score/v1
    description: Score the example range's objectives
    handler:
      type: executable
      path: commands/score
    initializer:                       # optional private session setup
      type: executable
      path: commands/init-score
  reset:
    protocol: operation/v1
    description: Restore the example range baseline
    handler:
      type: executable
      path: commands/reset
  scrub:
    enabled: false
discovery:
  range_tag: EXAMPLE                  # stable Range tag on cloud resources
infrastructure:
  azure:
    deployment: example-deployment
    scaffold_profile: template      # complete authored environment template
    template_environment: example-dev
    default_region: centralus
    network:
      cidr: 10.60.0.0/16
      editable: false
lifecycle:
  session_init: []
```

The provider must also have `providers/<provider>/inventory`, and the referenced
template must exist below `infra/`. For the legacy `active-directory` scaffold
profile, every host in `data/config.json` must have a corresponding Terragrunt
host module in the reference environment. `dreadgoad lab list --json` only
advertises provider combinations that pass these structural checks, preventing
the console from offering a range it can only partially create.

### Range-owned commands

`commands` configures five fixed semantic extension points: `health`,
`validate`, `score`, `reset`, and `scrub`. Every `service-range` must implement
`health`; it is the common readiness contract used by both `dreadgoad up` and
`/health`. The other commands may be implemented or disabled. A manifest cannot
introduce a new CLI or console slash command. Adding a sixth command requires
coordinated CLI and console code changes.

Each enabled command must use its assigned protocol:

| Command | Protocol | CLI entry point | Console command |
|---------|----------|-----------------|-----------------|
| `health` | `health/v1` | `dreadgoad health-check` | `/health` |
| `validate` | `validate/v1` | `dreadgoad validate` | `/validate` |
| `score` | `score/v1` | `dreadgoad score` | `/score` |
| `reset` | `operation/v1` | `dreadgoad lab reset` | `/reset` |
| `scrub` | `operation/v1` | `dreadgoad score reset` | `/scrub` |

A handler may be a CLI-owned built-in or an executable shipped below the range
directory:

```yaml
commands:
  health:
    protocol: health/v1
    handler:
      type: executable
      path: commands/health
  validate:
    protocol: validate/v1
    handler:
      type: builtin
      profile: active-directory
  scrub:
    enabled: false
    description: This range does not retain engagement artifacts
```

`active-directory` is currently the only built-in profile. New range families
should normally use executable handlers. A disabled command must not declare a
protocol, handler, or initializer. Command names, manifest fields, handler
types, protocols, and paths are validated strictly when `range.yml` is loaded.

Active Directory ranges may omit `commands` and retain all current built-ins.
They may override `health`, but no range may disable it. A `service-range`
manifest that omits `commands.health` is invalid, and `dreadgoad up` resolves
the handler before running doctor or creating infrastructure. This also catches
a missing, non-executable, or escaping handler path before deployment begins.
Other undeclared service-range commands are omitted from the console and model
command sets, and direct CLI invocation reports that they are unsupported. The
older `inspection.profile` field remains a compatibility bridge for AD health
and validation but should not be used for new ranges.

#### How command routing works

```text
ad/<range>/range.yml
        │
        ▼
dreadgoad range capabilities
        │
        ├── CLI command calls rangecommand.Require()
        │       └── built-in implementation or confined executable
        │
        └── console loads a capability snapshot for the selected session
                ├── filters autocomplete and /help
                ├── filters the model's command instructions and tool metadata
                └── rejects unsupported model tool calls
```

The manifest selects implementation, availability, description, and detail.
It does not control whether a command is direct or model-assisted, whether it
is destructive, or whether approval is required; those policies remain owned
by the CLI and console. The console caches the capability snapshot for the
session, so restart or recreate an active session after changing `range.yml`.
The CLI still resolves the capability on every invocation as the final
fail-closed boundary.

For a variant environment, the authored source owns command and agent context
until variant generation begins. A completed generated target then becomes the
single root used to resolve command capabilities, handlers, initializer, agent
prompt, handler working directory, and `lab_path`. An existing target without
the generator completion marker fails closed instead of mixing source behavior
with partial generated files. The console automatically rebuilds its cached
agent context after `/up`, `/provision`, or `/variant` can change that root.

#### Executable handler contract

Executable paths are relative to the range directory. DreadGOAD resolves
symlinks, rejects paths that escape the range, and requires a regular file with
an executable bit. It starts the executable directly, with the range directory
as its working directory, and writes exactly one JSON request to stdin:

```json
{
  "schema": "dreadgoad/range-command-request/v1",
  "command": "health",
  "protocol": "health/v1",
  "environment": "example-dev",
  "config_path": "/workspace/dreadgoad.yaml",
  "project_root": "/workspace/DreadGOAD",
  "lab_path": "/workspace/DreadGOAD/ad/Example",
  "options": {"json": true},
  "arguments": []
}
```

The request contains selectors and parsed command inputs, not cloud credentials
or configuration-file contents. The child environment is allowlisted: ordinary
process and model secrets are removed, while standard runtime, proxy,
SSH-agent, and cloud-provider variables (`AWS_*`, `AZURE_*`, `ARM_*`,
`GOOGLE_APPLICATION_CREDENTIALS`, and similar provider context) are retained.
Handlers are trusted range code and may read files below `lab_path` when needed.

A handler may stream human-readable progress to stdout. Its final non-empty
stdout line must be a single JSON object whose `schema` exactly matches the
command protocol and must not exceed 1 MiB. Earlier progress output is streamed
without counting toward that record limit. Write diagnostics to stderr and
return a non-zero status on failure. A non-zero exit, missing or oversized
final object, malformed JSON, or invalid protocol payload fails the command.
Handlers should ignore unknown option keys so later CLI versions can add inputs
compatibly.

The `options` object contains these command-specific values:

| Command | Options |
|---------|---------|
| `health` | `json` (boolean) |
| `validate` | `verbose`, `no_fail`, `quick`, `plain`, `json` (booleans); `output`, `poll` (strings) |
| `score` | `report` (required string), `live_verify` (boolean); optional `answer_key`, `output`, `range_artifacts`, `attack_box`, `region`, `profile`, `ssh_key`, `ssh_user` strings |
| `reset` | `skip_purge`, `skip_provision`, `skip_creator_check` (booleans); `plays`, `limit` (strings); `max_retries`, `retry_delay` (integers); `extra_vars` (string array). Unparsed positional values are also supplied in `arguments`. |
| `scrub` | `apply`, `skip_kali`, `skip_windows`, `purge_ad`, `save_report` (booleans); `attack_box`, `ssh_key`, `ssh_user`, `report_output` (strings) |

The following are minimal valid final results. Check objects may carry
additional range-specific fields. For useful console rendering, health checks
should include `name`, `host`, `status`, and `detail`; validation checks should
include `category`, `name`, `status`, and `detail`. Health statuses are `OK`,
`FAIL`, or `SKIP`; validation statuses are `PASS`, `FAIL`, `WARN`, `SKIP`, or
`INFO`.

```json
{"schema":"health/v1","passed":1,"failed":0,"skipped":0,"checks":[{"name":"HTTP","host":"WEB01","status":"OK","detail":"responded on port 443"}]}
```

```json
{"schema":"validate/v1","passed":1,"failed":0,"warnings":0,"total_checks":1,"checks":[{"category":"web","name":"TLS configuration","status":"PASS","detail":"expected certificate installed"}]}
```

```json
{"schema":"score/v1","score":8,"maximum":10,"objectives":[{"id":"admin-access","achieved":true,"points":8}]}
```

```json
{"schema":"operation/v1","changed":true,"steps":[{"name":"restore database","status":"OK"}]}
```

A minimal executable health handler can be written as:

```python
#!/usr/bin/env python3
import json
import sys

request = json.load(sys.stdin)
if request.get("command") != "health":
    raise SystemExit("unexpected command")

print("Checking the example service...")
print(json.dumps({
    "schema": "health/v1",
    "passed": 1,
    "failed": 0,
    "skipped": 0,
    "checks": [{
        "name": "HTTP",
        "host": "WEB01",
        "status": "OK",
        "detail": "responded on port 443",
    }],
}, separators=(",", ":")))
```

Save it at the manifest path and mark it executable, for example
`chmod 0755 ad/Example/commands/health`. Before opening the range in the
console, inspect the resolved capability set and exercise each enabled entry
point directly:

```bash
dreadgoad --config /path/to/dreadgoad.yaml --env example-dev range capabilities
dreadgoad --config /path/to/dreadgoad.yaml --env example-dev health-check --json
dreadgoad --config /path/to/dreadgoad.yaml --env example-dev validate --json
```

The capabilities output must list all five semantic commands, with unsupported
ones carrying `"supported": false`. An enabled executable is not ready until
its CLI entry point exits zero and accepts its final structured result.

#### Score session initialization

An optional score `initializer` runs once when a console session is attached.
It receives a private directory in `options.output_dir`, writes any
range-specific objectives or reference data there, and finishes with a
`session-init/v1` result:

```json
{"schema":"session-init/v1","artifacts":["/absolute/private/path/objectives.json"],"message":"scoring data ready"}
```

Every artifact must be a regular file that resolves inside `output_dir`. The
console later supplies that directory to the score handler as
`options.range_artifacts`; console users and the model cannot override it. Test
an initializer without the console using `dreadgoad range init-session
--output-dir /path/to/private-test-dir --json` with the same config and
environment selectors.

`dreadgoad score generate-key` belongs only to the built-in Active Directory
scorer. It is rejected when scoring is disabled or owned by an executable
handler, preventing a custom range from accidentally generating or overwriting
the legacy shared answer key. Executable-owned scorers should declare an
`initializer` when they need private setup artifacts and use `range
init-session` to exercise it outside the console. Do not also declare the
legacy `lifecycle.session_init: generate_answer_key` action: manifests reject
that combination. For built-in scoring, the console requires the session-local
`answer_key.json`; if initialization did not create it, scoring fails instead
of falling back to the repository-wide key.

`discovery.range_tag` is required for every manifest-backed range. Every cloud instance in
the range must carry `Range=<range_tag>`; AWS and Azure discovery combine that
identity with the selected environment so commands cannot cross range
boundaries. The value is independent of the range's directory name and display
name, allowing either to change without altering cloud identity.

`template` profiles currently require a fixed private IPv4 `/16` and must not
set `network.editable: true`. The template owns its subnet layout, so accepting
an arbitrary CIDR without a range-specific renderer would make the config and
the copied infrastructure disagree.

### Range-specific agent guidance

The console composes model instructions in a fixed order: console-owned general
policy, the optional range prompt, an authoritative generated command catalog,
and current session context. Declare the range prompt in `range.yml`:

```yaml
agent:
  prompt: prompts/agent.md
```

Use this file to explain the range's purpose, terminology, topology, intended
state, and useful diagnostic workflows. For example:

```markdown
This range models a vulnerable container registry and Kubernetes workload.
Do not assume Active Directory or Windows hosts exist. Treat the API, registry,
and worker readiness checks as the core service-health signals.
```

The path is relative to the range directory. DreadGOAD resolves symlinks,
rejects paths that escape the range, requires a non-empty UTF-8 regular file,
and limits the prompt to 32 KiB. Do not put credentials or generated session
secrets in this source-controlled file; handlers and read tools provide live
range data when needed.

Range guidance supplies domain context, not authority. It cannot add tools or
commands, enable an unsupported capability, expand the backend-accepted argument
surface, remove approval, or redefine console safety classifications. It may
guide the model toward supported arguments and workflows for that range. The
console renders the generated command catalog after the range prompt, using
range-owned descriptions together with console-owned policy. Restart or recreate
an active console session after changing the prompt because agent instructions
are cached per session.

Run `dreadgoad lab list --json` to confirm the range appears and inspect its
`provider_settings`. Then exercise `dreadgoad --config <path> --env <name> env
create <name>` in a disposable checkout before exposing it to console users.

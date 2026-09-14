# GOAT — Game of Agent Trust

**GOAT** (**Game of Agent Trust**) is the user-facing name for the internally
stable `SCOPE-RANGE` lab. It is a six-host Linux environment for exercising offensive AI agents
against a connected set of applications, databases, development platforms,
storage systems, and infrastructure services. Its fictional scenario follows
the Dreadnode Biology Division and Project KRAKEN, a deep-ocean program breeding
massive, aggressive octopuses. It is deployed alongside the existing GOAD labs
and selected by range name in the DreadGOAD web console or through the normal
CLI environment configuration.

The environment contains synthetic identities and data only. Its credentials
are intentionally deterministic and must never be reused outside the range.

The final `scope-seed.yml` play adds a versioned, repeatable activity layer
after the foundational services are ready. `scope-seed-v2` creates four Garage
buckets, real database snapshots, a collaborative Gitea repository, an Actions
image build, a Jenkins export job, RabbitMQ-backed cross-host exports, six mail
messages, a Nextcloud S3 mount, and Kali browser fixtures. Re-running the play
within a version-two deployment reconciles this state without duplicating
issues, mail, jobs, or objects.
The database fixtures include 12 deep-sea organizations, 16 mythic projects,
24 invoices, 10 experiments, 12 telemetry readings, 8 specimens, and 8 dive
logs. The shared identities are `michael` and `shane`; application administration
uses the synthetic `poseidon` account.

`scope-seed-v2` changes persistent database and application principals as well
as the scenario data. A range previously provisioned with `scope-seed-v1` must
be destroyed and freshly provisioned; the seed play does not attempt an in-place
rename of existing PostgreSQL, MongoDB, WordPress, Nextcloud, Gitea, or Jenkins
accounts.

## Topology

| Host | Address | Purpose | Principal services |
| --- | --- | --- | --- |
| `kali01` | `10.50.10.10` | Agent workstation and Ansible jump host | Client tooling, agent workspace, service smoke tests |
| `web01` | `10.50.10.20` | Web applications | Nginx, WordPress, Nextcloud |
| `data01` | `10.50.10.30` | Application and synthetic business data | PostgreSQL, MariaDB, MongoDB, Redis |
| `dev01` | `10.50.10.40` | Development and automation | Gitea, Gitea Actions, Jenkins, OCI registry |
| `storage01` | `10.50.10.50` | Object and network file storage | Garage S3, Samba, NFS, SFTP, rsync |
| `services01` | `10.50.10.60` | Shared infrastructure | BIND9, OpenLDAP, RabbitMQ, Postfix, Dovecot |

All six hosts share the private `10.50.10.0/24` workload subnet and no workload
VM receives a public IP. On Azure, Bastion uses `10.50.20.0/26`; provisioning
opens a Bastion tunnel to Kali and layers a local SOCKS5 proxy over SSH. On AWS,
the VPC uses `10.50.0.0/16`, a single public `10.50.0.0/24` NAT subnet, and the
private workload subnet. Ansible and interactive access use SSM, with private
SSM, EC2 Messages, SSM Messages, and S3 VPC endpoints.

The first provisioning play uses deterministic `/etc/hosts` entries. BIND9 is
then configured on `services01`, verified locally, and activated across all
hosts with the cloud resolver retained as a fallback. This avoids a bootstrap dependency
on the DNS server that Ansible is in the process of creating.

## Deploy

Prerequisites are Python 3.10 or newer, OpenTofu, Terragrunt, Ansible, the locally
built `dreadgoad` CLI, and an authenticated CLI for the selected provider
(`az` for Azure or `aws` for AWS).

```bash
cd cli
go build -o dreadgoad .
cd ..

./cli/dreadgoad --env scope-dev config show
./cli/dreadgoad --env scope-dev infra validate
./cli/dreadgoad --env scope-dev infra plan
./cli/dreadgoad --env scope-dev infra apply
./cli/dreadgoad --env scope-dev provision
```

The equivalent single workflow is:

```bash
./cli/dreadgoad --env scope-dev up
```

The environment selects these first-class values from `dreadgoad.yaml`:

```yaml
environments:
  scope-dev:
    lab: SCOPE-RANGE
    provider: azure
    deployment: scope-range-deployment
    region: centralus
```

For AWS, select `provider: aws` and a region (the authored default is
`us-east-2`). The CLI scaffolds from the `scope-aws` template and automatically
bootstraps an account-qualified S3/DynamoDB remote-state backend on the first
plan or apply. The six instances use fixed private addresses, encrypted gp3
volumes, IMDSv2, and SSM; no EC2 instance receives a public address.

Before the first AWS deployment, the account must accept the official Kali
Linux Marketplace image terms. If Marketplace use is restricted, set an
approved Kali `ami_id` in the generated Kali Terragrunt inputs instead. The
remaining five hosts use Canonical's public Ubuntu 24.04 image family.

For Azure, the shared SSH key is generated during infrastructure apply at
`~/.dreadgoad/keys/azure-scope-dev-goat-admin`. Azure resources use the
`<environment>-goat-*` prefix, including the `scope-dev-goat-rg` resource group.
New service hosts use the `goatadmin` infrastructure account. Kali is mandatory
because it is both the agent workstation and the private-network provisioning
hop.

## Web console

Launch the console from the repository root:

```bash
./dreadgoad-console
```

In **New Session**, choose **Create a new environment**, select **GOAT**, choose
Azure or AWS, and name the environment. The selected provider's default region
(`centralus` or `us-east-2`) and fixed `10.50.0.0/16` network are shown from the
range manifest. GOAT does not support randomized variants, so no variant control
is shown.

The console creates a private config under `.dreadgoad/console/configs/`, copies
the authored GOAT infrastructure template to the new environment, creates its
inventory, and opens a session. This preparation does not create cloud resources;
use `/up` when ready to deploy. Existing CLI-created environments remain
selectable through **Use an existing environment**.

The session uses the same CLI, state, inventory, validation manifest, and
range-owned lifecycle as a terminal workflow. The main commands are:

| Command | Behavior for SCOPE-RANGE |
| --- | --- |
| `/up` | Deploy and provision the complete range after operator confirmation |
| `/instances` | Show cloud power state for the GOAT hosts |
| `/health` | Run the GOAT-specific core availability checks |
| `/status` | Run `/instances` followed by `/health` |
| `/validate` | Check provider topology, Linux services, applications, users, and seeded data |
| `/start [host]` | Start the whole range or one named host |
| `/stop [host]` | Stop the whole range or one named host |
| `/restart <host>` | Restart one named host |
| `/destroy [host]` | Destroy the range or one named host after operator confirmation |

These command names are range-agnostic at the console surface. The backend
selects the GOAT-specific inspection and validation implementations from the
session's configured lab. SCOPE-RANGE declares no session initialization hooks,
so creating a console session does not generate a GOAD answer key or modify the
range. See the [console guide](https://github.com/dreadnode/DreadGOAD/blob/main/console/README.md)
for setup, command dispatch, approvals, and security boundaries.

## Service validation

`scope-kali.yml`, the final foundational provisioning play, waits for the core
endpoints and runs `/usr/local/bin/scope-range-smoke` from Kali. The smoke test
verifies DNS, both web hosts, Gitea, the registry, PostgreSQL and its seed data,
MariaDB, Redis, OpenLDAP, Garage S3, SMB, and NFS. The subsequent
`scope-seed.yml` play reconciles and verifies the versioned activity layer.

To rerun the live service gate without rebuilding infrastructure:

```bash
./cli/dreadgoad --env scope-dev provision --plays scope-kali.yml
```

To reconcile only the versioned activity layer:

```bash
./cli/dreadgoad --env scope-dev provision --plays scope-seed.yml
```

For a read-only audit of the deployed range, use the standard lab validation
command:

```bash
./cli/dreadgoad --env scope-dev validate
```

The validator reads the deployed-state contract from
`ad/SCOPE-RANGE/data/validation.json`. On Azure it checks the resource group,
exact VM set, private addressing, sizes, tags, VNet, subnets, public IP set, and
Bastion. On AWS it checks the exact EC2 set, private addresses, instance types,
tags, VPC, subnets, NAT gateway, private service endpoints, security-group
exposure, and SSM status. It then uses Azure Run Command or AWS SSM shell
commands to verify all six hosts concurrently. Those remote checks cover users,
services, containers, application configuration, database schemas and seed
records, shared storage, DNS, LDAP, messaging, mail, development history,
build outputs, browser access, and cross-host automation. The full manifest
shares 84 host/service checks across both providers, plus provider-specific
topology assertions.
The assertions are read-only and do not intentionally alter configured range
state; Azure Run Command still records normal execution metadata and logs.

By default the validator writes a timestamped JSON report under `/tmp` and
returns a non-zero status when a check fails. Useful options are:

```bash
# Critical checks only
./cli/dreadgoad --env scope-dev validate --quick

# Select a subscription and report path explicitly
AZURE_SUBSCRIPTION_ID="<subscription-id>" \
./cli/dreadgoad --env scope-dev validate \
  --output /tmp/scope-range-validation.json

# Validate both provider contracts without contacting either cloud
./scripts/validate-scope-range-live.py --provider azure --manifest-only
./scripts/validate-scope-range-live.py --provider aws --manifest-only
```

`--no-fail` preserves the report and returns zero even when an assertion fails.
`--verbose` prints cloud command diagnostics, but redacts the encoded remote
payload because it includes the range's synthetic credentials.

The CLI dispatches `SCOPE-RANGE` to `scripts/validate-scope-range-live.py` and
forwards `--quick`, `--output`, `--verbose`, and `--no-fail`. The GOAT validator
always streams plain output, so `--plain` is accepted as a no-op. Continuous
`--poll` mode remains specific to the GOAD live dashboard and is rejected for
SCOPE-RANGE.

For static validation of the implementation before deployment:

```bash
./scripts/validate-scope-range.sh
```

## Access and synthetic credentials

Connection settings are installed on Kali at
`/home/kali/.config/scope-range/services.env` with mode `0600`. The file includes
the deliberately synthetic range credentials and endpoints used by the smoke
test. The agent runtime itself is intentionally left unspecified; install it in
`/opt/scope-agent` and use `/home/kali/workspace` for agent work.

## Teardown

The deployment creates six VMs and their managed disks. Azure also creates a
VNet and Standard Bastion; AWS creates a VPC, NAT gateway, and private service
endpoints. Bastion and the NAT gateway are material parts of the hourly cost.
Destroy the environment when it is not in use:

```bash
./cli/dreadgoad --env scope-dev infra destroy
```

Terraform state is stored beneath `~/.dreadgoad/state/azure/scope-range/`,
outside both the repository and the disposable Terragrunt cache. The CLI keeps
that tree private and automatically migrates the checkout-local state path used
by early SCOPE-RANGE builds. Back up this directory while a range is live: it is
the record the normal CLI destroy path needs to clean up the deployment.
The state directory retains its original internal name for compatibility.
Existing environments whose `env.hcl` uses the former `scope-range` resource
prefix and SSH key name remain supported; newly created environments use
`goat`.

AWS uses its account-qualified S3 remote-state bucket and a DynamoDB lock table.
Those backend resources are retained after range destruction so state remains
recoverable; the DreadGOAD-managed SSM transfer bucket is deleted after a
successful full infrastructure destroy.

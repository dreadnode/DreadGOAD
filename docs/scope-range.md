# SCOPE-RANGE on Azure

SCOPE-RANGE is a six-host Linux environment for exercising offensive AI agents
against a connected set of applications, databases, development platforms,
storage systems, and infrastructure services. It is deployed alongside the
existing GOAD labs and selected through the normal DreadGOAD environment
configuration.

The environment contains synthetic identities and data only. Its credentials
are intentionally deterministic and must never be reused outside the range.

The final `scope-seed.yml` play adds a versioned, repeatable activity layer
after the foundational services are ready. `scope-seed-v1` creates four Garage
buckets, real database snapshots, a collaborative Gitea repository, an Actions
image build, a Jenkins export job, RabbitMQ-backed cross-host exports, six mail
messages, a Nextcloud S3 mount, and Kali browser fixtures. Re-running the play
reconciles this state without duplicating issues, mail, jobs, or objects.

## Topology

| Host | Address | Purpose | Principal services |
| --- | --- | --- | --- |
| `kali01` | `10.50.10.10` | Agent workstation and Ansible jump host | Client tooling, agent workspace, service smoke tests |
| `web01` | `10.50.10.20` | Web applications | Nginx, WordPress, Nextcloud |
| `data01` | `10.50.10.30` | Application and synthetic business data | PostgreSQL, MariaDB, MongoDB, Redis |
| `dev01` | `10.50.10.40` | Development and automation | Gitea, Gitea Actions, Jenkins, OCI registry |
| `storage01` | `10.50.10.50` | Object and network file storage | Garage S3, Samba, NFS, SFTP, rsync |
| `services01` | `10.50.10.60` | Shared infrastructure | BIND9, OpenLDAP, RabbitMQ, Postfix, Dovecot |

All six hosts share the private `10.50.10.0/24` workload subnet. Azure Bastion
uses `10.50.20.0/26`; no workload VM receives a public IP. During provisioning,
the CLI opens an Azure Bastion tunnel to Kali and layers a local SOCKS5 proxy
over SSH so Ansible can reach the private service hosts.

The first provisioning play uses deterministic `/etc/hosts` entries. BIND9 is
then configured on `services01`, verified locally, and activated across all
hosts with Azure DNS retained as a fallback. This avoids a bootstrap dependency
on the DNS server that Ansible is in the process of creating.

## Deploy

Prerequisites are Python 3.10 or newer, an authenticated Azure CLI, OpenTofu,
Terragrunt, Ansible, and the locally built `dreadgoad` CLI.

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

The shared SSH key is generated during infrastructure apply at
`~/.dreadgoad/keys/azure-scope-dev-scope-range-admin`. Kali is mandatory because
it is both the agent workstation and the private-network provisioning hop.

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

For a read-only audit of the deployed range, run the standalone live validator:

```bash
./scripts/validate-scope-range-live.py --env scope-dev
```

The validator reads the deployed-state contract from
`ad/SCOPE-RANGE/data/validation.json`. It checks the Azure resource group, exact
VM set, private addressing, absence of workload public IPs, sizes, tags, VNet,
subnets, public IP set, and Bastion configuration. It then uses Azure Run
Command to verify all six hosts concurrently. Those remote checks cover users,
services, containers, application configuration, database schemas and seed
records, shared storage, DNS, LDAP, messaging, mail, development history,
build outputs, browser access, and cross-host automation. The full manifest
contains 128 assertions: 44 Azure/topology checks and 84 host/service checks.
The assertions are read-only and do not intentionally alter configured range
state; Azure Run Command still records normal execution metadata and logs.

By default the validator writes a timestamped JSON report under `/tmp` and
returns a non-zero status when a check fails. Useful options are:

```bash
# Critical checks only
./scripts/validate-scope-range-live.py --env scope-dev --quick

# Select a subscription and report path explicitly
./scripts/validate-scope-range-live.py \
  --env scope-dev \
  --subscription "$AZURE_SUBSCRIPTION_ID" \
  --output /tmp/scope-range-validation.json

# Validate the expected-state manifest without contacting Azure
./scripts/validate-scope-range-live.py --manifest-only
```

`--no-fail` preserves the report and returns zero even when an assertion fails.
`--verbose` prints Azure command diagnostics, but redacts the encoded remote
payload because it includes the range's synthetic credentials.

The current branch predates the generalized `dreadgoad validate` command. When
that command is merged, its lab dispatch should invoke this validator for
`SCOPE-RANGE` instead of the Active Directory vulnerability checks used by GOAD
labs.

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

The deployment creates six VMs, six managed data/OS disk sets, a VNet, and an
Azure Bastion Standard instance. Bastion is a material part of the hourly cost.
Destroy the environment when it is not in use:

```bash
./cli/dreadgoad --env scope-dev infra destroy
```

Terraform state is stored beneath `.dreadgoad/state/azure/scope-range/` rather
than the disposable Terragrunt cache, so the normal CLI destroy path retains the
state required to clean up the deployment.

# DreadGOAD

A heavily modified fork of [GOAD (Game of Active Directory)](https://github.com/Orange-Cyberdefense/GOAD)
by Orange Cyberdefense. DreadGOAD deploys vulnerable Active Directory lab
environments for penetration testing and security research.

> **Warning:** This lab is extremely vulnerable by design. Do not deploy it on
> the internet without proper network isolation, and do not reuse any of these
> configurations in production environments.

## What's Different from GOAD?

DreadGOAD extends the upstream GOAD project with:

- **Go CLI (`dreadgoad`)** -- single binary for provisioning, health checks, trust verification, and vulnerability validation
- **Web console (`dreadgoad-console`)** -- agentic chat UI for building, managing, and validating lab ranges with a live topology view
- **AWS infrastructure automation** -- Terragrunt/Terraform modules for deploying labs in AWS with SSM-based management (no open ports)
- **Modular extension system** -- plug-in extensions for ELK, Exchange, Wazuh, Guacamole, and more
- **Variant generator** -- create graph-isomorphic lab copies with randomized entity names while preserving all attack paths
- **Ansible collection (`dreadnode.goad`)** -- 120+ roles packaged as a reusable collection
- **Multi-provider support** -- VirtualBox, VMware, Proxmox, AWS, Azure, and Ludus

## Lab Environments

| Lab | VMs | Forests | Domains | Description |
|-----|-----|---------|---------|-------------|
| [GOAD](ad/GOAD/) | 5 | 2 | 3 | Full lab -- the complete Game of Active Directory experience |
| [GOAD-Light](ad/GOAD-Light/) | 3 | 1 | 2 | Lighter variant for resource-constrained setups |
| [GOAD-Mini](ad/GOAD-Mini/) | 1 | 1 | 1 | Minimal single-DC lab |
| [GOAD-variant-1](ad/GOAD-variant-1/) | 5 | 2 | 3 | Graph-isomorphic copy of GOAD with randomized entity names |
| [MINILAB](ad/MINILAB/) | 2 | 1 | 1 | One DC + one workstation |
| [SCCM](ad/SCCM/) | 4 | 1 | 1 | MECM/SCCM attack scenarios |
| [NHA](ad/NHA/) | 5 | 2 | 2 | Ninja Hacker Academy -- challenge mode |
| [DRACARYS](ad/DRACARYS/) | 3 | 1 | 1 | Training challenge variant |
| [TEMPLATE](ad/TEMPLATE/) | 2 | 1 | 1 | Scaffold for authoring your own lab |

Vulnerability coverage varies by lab. The full GOAD lab is the richest, planting
50+ intentional misconfigurations spanning Kerberoasting, AS-REP roasting, ACL
abuse chains, ADCS misconfigurations (ESC1-4, ESC6-11, ESC13, ESC15), MSSQL
linked-server abuse, and delegation abuse. The smaller labs deliberately carry a
narrow subset: GOAD-Mini, MINILAB, DRACARYS, and TEMPLATE have no Kerberoastable
accounts or MSSQL instances at all, and SCCM concentrates on MECM/SCCM attack
paths. See [docs/GOAD-vulnerabilities-comprehensive.md](docs/GOAD-vulnerabilities-comprehensive.md)
for the full catalog, which is scoped to the full GOAD lab.

## Quick Start

### Prerequisites

- ansible-core >= 2.20, < 2.21 (2.19 breaks Windows modules over AWS SSM — [ansible/ansible#86397](https://github.com/ansible/ansible/issues/86397); fixed in 2.20)
- Go 1.26+ (for building the CLI)
- A supported infrastructure provider (VirtualBox, VMware, Proxmox, AWS, Azure, or Ludus)

### Install

```bash
# Clone the repo
git clone https://github.com/dreadnode/DreadGOAD.git
cd DreadGOAD

# Install Ansible dependencies
ansible-galaxy collection install -r ansible/requirements.yml

# Build the CLI
cd cli && go build -o dreadgoad . && cd ..
```

### Deploy a Lab

```bash
# Provision the full GOAD lab
./cli/dreadgoad provision

# Health check all instances
./cli/dreadgoad health-check

# Validate vulnerabilities are configured
./cli/dreadgoad validate --quick
```

For provider-specific setup instructions, see the [provider documentation](docs/mkdocs/docs/providers/).

### Generate a Variant

Create a randomized copy of any lab with unique names but identical attack paths:

```bash
./cli/dreadgoad variant generate --source ad/GOAD --target ad/my-variant --name my-variant
```


## Documentation

- [Console guide](console/README.md) -- web UI quick start, commands, and environment variables
- [CLI reference](docs/cli.md) -- command index plus Viper-based config, environment variables, and per-environment settings
- [Domains and users](docs/domains-and-users.md) -- full network topology, credentials, and attack paths
- [Vulnerability catalog](docs/GOAD-vulnerabilities-comprehensive.md) -- 50+ vulnerabilities in the full GOAD lab, with exploitation techniques
- [Domain compromise paths](docs/domain-compromise-paths.md) -- every provisioned way to reach Domain Admin / golden ticket (29 distinct paths, ~133 permutations)
- [Validation guide](docs/validation.md) -- automated vulnerability validation (with live dashboard and polling)
- [Scoreboard guide](docs/scoreboard.md) -- live engagement status board (answer key + agent report verification)
- [Provider guides](docs/mkdocs/docs/providers/) -- VirtualBox, VMware, Proxmox, AWS, Azure, Ludus
- [AWS AMI build & deploy workflow](docs/mkdocs/docs/providers/aws-ami-workflow.md) -- end-to-end warpgate + Terragrunt + Ansible
- [Extension guides](docs/mkdocs/docs/extensions/) -- ELK, Exchange, Wazuh, hardened workstation
- [Architecture diagram](docs/architecture.svg)
- [Upstream GOAD docs](https://orange-cyberdefense.github.io/GOAD/) -- original project documentation

## Project Structure

```text
DreadGOAD/
├── ad/                    # Lab definitions (GOAD, GOAD-Light, MINILAB, SCCM, NHA, ...)
├── ansible/               # Ansible collection with 120+ roles and custom modules
├── cli/                   # Go CLI source (dreadgoad), including the variant generator
├── console/               # Web console -- agentic chat + live range topology (Python/TypeScript)
├── docs/                  # Documentation and architecture diagrams
├── extensions/            # Pluggable lab extensions (ELK, Exchange, Wazuh, ...)
├── infra/                 # Terragrunt configurations for AWS deployments
├── modules/               # Terraform modules (AWS networking, instance factory)
├── packer/                # VM templating (Vagrant, Proxmox)
├── warpgate-templates/    # Golden AMI build templates (warpgate)
└── template/              # Provider templates
```

## Contributing

Contributions are welcome! Please see [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## Acknowledgments

DreadGOAD is built on the excellent work of the [GOAD](https://github.com/Orange-Cyberdefense/GOAD)
project by [Mayfly](https://github.com/Mayfly277) and [Orange Cyberdefense](https://github.com/Orange-Cyberdefense).
If you find this useful, consider [sponsoring the original creator](https://github.com/sponsors/Mayfly277).

Additional references and credits can be found in the [upstream documentation](https://orange-cyberdefense.github.io/GOAD/).

## License

GPL-3.0-or-later -- see [LICENSE](LICENSE).

## Disclaimer

This project deploys intentionally vulnerable configurations for security
research and penetration testing training. **Do not use in production
environments.** Use at your own risk.

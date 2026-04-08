# DreadGOAD: Open-Sourcing the Infrastructure Behind Our Offensive Security Research

By Jayson Grace · April 7, 2026

---

Many of the Active Directory evaluations we publish at Dreadnode - from [PentestJudge](https://arxiv.org/abs/2508.02921) to [Kerberoasting agents](https://dreadnode.io/research/evaluating-offensive-cyber-agents-kerberoasting) to [training an 8B model to pop Domain Admin](https://dreadnode.io/research/worlds-a-simulation-engine-for-agentic-pentesting) - start the same way: spin up a vulnerable AD network, point an agent at it, and measure what happens. That network is GOAD, the [Game of Active Directory](https://github.com/Orange-Cyberdefense/GOAD) by Orange Cyberdefense. Over the past year we've rebuilt the tooling around it into something we could run reliably at scale. Today we're open-sourcing the result: [**DreadGOAD**](https://github.com/dreadnode/DreadGOAD).

## Why GOAD, and Why Fork It

GOAD is one of the best open-source resources in AD security training. Mayfly277 and the Orange Cyberdefense team built a multi-domain, multi-forest Active Directory environment packed with over 50 real-world vulnerabilities - Kerberoasting, AS-REP roasting, ACL abuse chains, ADCS misconfigurations (ESC1 through ESC8), delegation abuse, MSSQL attacks, and more. It's what a messy corporate AD actually looks like, compressed into a handful of VMs.

We adopted GOAD early as the target environment for our offensive agent evaluations. It didn't take long to hit the walls. We needed to deploy labs in AWS without exposing management ports, tear them down and rebuild programmatically between eval runs, and validate that all 50+ vulnerabilities were actually configured correctly after provisioning - because a misconfigured lab silently invalidates every result you collect against it. On top of that, we needed to generate structurally identical variants of the lab with different entity names, so agents couldn't memorize their way to Domain Admin and Golden Tickets.

None of that existed in upstream GOAD. So we built it.

## What DreadGOAD Adds

DreadGOAD is a downstream fork that preserves GOAD's core lab designs while wrapping them in infrastructure automation we can actually run unattended. The major additions:

### A Single Go Binary

The `dreadgoad` CLI replaces a collection of Python scripts and shell commands with one binary that handles the full lab lifecycle:

```bash
# Deploy infrastructure, provision the lab, validate everything works
dreadgoad infra init
dreadgoad infra apply
dreadgoad provision
dreadgoad validate --quick

# Check health, manage instances
dreadgoad health-check
dreadgoad lab status
dreadgoad lab stop-vm winterfell

# Open an interactive shell - no SSH keys, no open ports
dreadgoad ssm connect kingslanding
```

The CLI covers infrastructure deployment, Ansible provisioning with retry logic, health checks, vulnerability validation, SSM sessions, multi-environment management, extensions, and variant generation. Configuration is Viper-based - YAML files, environment variables, and CLI flags all merge cleanly.

### AWS Infrastructure as Code

Terragrunt and Terraform modules deploy the full lab into AWS with a design built around SSM Session Manager. Lab instances don't need public IPs, and management access doesn't depend on exposed SSH or RDP ports. Management traffic flows through VPC endpoints, and state is stored in S3 with DynamoDB locking. Golden AMIs built via warpgate pre-bake Windows Updates, AD DS roles, and MSSQL, cutting per-deployment provisioning time substantially.

### Automated Vulnerability Validation

This is the feature we wished existed from day one. `dreadgoad validate` executes PowerShell checks over SSM against every configured vulnerability in the lab - Kerberoastable SPNs, AS-REP roastable accounts, ACL misconfigurations, ADCS template abuse, constrained and unconstrained delegation, LAPS configurations, the works. Output is a structured report (table or JSON) telling you exactly what's correctly configured and what isn't, before you waste compute on an eval run against a broken lab.

### Variant Generator

For evaluations, you don't want agents that have memorized that `joffrey.baratheon` is Kerberoastable. The variant generator, built by Michael Kouremetis, creates graph-isomorphic copies of any lab. Entity names are randomized where possible while structural relationships and attack paths stay intact. It transforms 96+ files across JSON, YAML, PowerShell, Terraform, and Vagrant configurations, and produces a mapping file so you can trace back to the original topology. Same vulnerabilities, different surface.

### Modular Extensions

Plug-in extensions add optional components without modifying core lab definitions: ELK for log aggregation, Wazuh for EDR monitoring, Exchange for email-based attack scenarios, Guacamole for browser-based access, and additional workstation and Linux host configurations. Extensions include provider-specific configurations across VirtualBox, VMware, Proxmox, AWS, Azure, and Ludus where applicable.

### Seven Lab Environments

Beyond the original GOAD and GOAD-Light, DreadGOAD includes GOAD-Mini (single DC, fastest deployment), MINILAB (one DC plus one workstation), SCCM (Microsoft Endpoint Configuration Manager scenarios), NHA (Ninja Hacker Academy challenge mode), and DRACARYS (training challenges). A template is included for building custom labs.

## How We Use It

DreadGOAD is the infrastructure layer for the kind of AD-focused evaluation work we publish: reproducible deployments, resettable environments, automated validation, and controlled variants.

That pattern shows up across recent research:

[PentestJudge](https://dreadnode.io/research/pentestjudge-judging-agent-behavior-against-operational-requirements) is the clearest example. Grading agent behavior against live enterprise targets means you need the same target environment every time - same vulnerabilities, same topology, same attack surface. A lab that drifts between runs poisons your ground truth. DreadGOAD's validation step catches that before we burn compute on trajectories we can't trust.

The [Kerberoasting benchmark](https://dreadnode.io/research/evaluating-offensive-cyber-agents-kerberoasting) pushed us hardest on the rapid teardown-rebuild cycle. [Worlds](https://dreadnode.io/research/worlds-a-simulation-engine-for-agentic-pentesting) - our sim2real transfer work - needed a concrete AD environment to check whether synthetic trajectories actually hold up against real targets. Our [evals foundation](https://dreadnode.io/research/evals-the-foundation-for-autonomous-offensive-security) work treats GOAD-style labs as canonical targets, and DreadGOAD is what makes those targets practical to run repeatedly. The [LLM-powered AMSI provider](https://dreadnode.io/research/llm-powered-amsi-provider-vs-red-team-agent) research was a reminder that host provisioning and repeatability matter just as much as the detection logic you're testing.

In short: when we need a realistic AD target that can be programmatically deployed, validated, and torn down, this is what we reach for.

## Getting Started

```bash
git clone https://github.com/dreadnode/DreadGOAD.git
cd DreadGOAD

# Install Ansible dependencies
ansible-galaxy collection install -r ansible/requirements.yml

# Build the CLI
cd cli && go build -o dreadgoad . && cd ..

# Deploy and validate
./cli/dreadgoad provision
./cli/dreadgoad validate --quick
```

DreadGOAD supports six infrastructure providers: VirtualBox and VMware for local deployment, Proxmox for on-premises hypervisors, AWS and Azure for cloud, and Ludus for range-based environments. Provider-specific guides are in the [GitHub documentation](https://github.com/dreadnode/DreadGOAD/tree/main/docs/mkdocs/docs/providers).

For AWS deployments, the [AMI build and deploy workflow](https://github.com/dreadnode/DreadGOAD/blob/main/docs/mkdocs/docs/providers/aws-ami-workflow.md) walks through the full pipeline from golden AMI creation through Terragrunt deployment to Ansible provisioning.

## Acknowledgments

DreadGOAD wouldn't exist without [Mayfly277](https://github.com/Mayfly277) and the [Orange Cyberdefense](https://github.com/Orange-Cyberdefense) team. They built and maintain the upstream GOAD project, and everything here is downstream of their work. If you find DreadGOAD useful, consider [sponsoring the original creator](https://github.com/sponsors/Mayfly277).

## What's Next

We're actively building on DreadGOAD - new lab configurations for additional attack scenarios, better provisioning reliability across providers, and closer integration with our eval toolchain. Contributions are welcome - see the [contribution guidelines](https://github.com/dreadnode/DreadGOAD/blob/main/CONTRIBUTING.md).

DreadGOAD is available now at [github.com/dreadnode/DreadGOAD](https://github.com/dreadnode/DreadGOAD).

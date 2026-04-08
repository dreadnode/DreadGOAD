# Security Policy

## About this project

DreadGOAD intentionally deploys **vulnerable Active Directory environments**
for offensive security training, penetration-testing practice, detection
engineering, and security research. Weak passwords, kerberoastable accounts,
ADCS misconfigurations (ESC1–8), ACL abuse paths, MSSQL attack chains, and
similar weaknesses inside the deployed labs are **intentional product
features**, not security bugs.

> [!CAUTION]
> Never deploy DreadGOAD on a production network, on a network shared with
> production assets, or on any host reachable from the public internet without
> strict isolation. Treat any DreadGOAD deployment as fully compromised by
> default.

## What counts as a vulnerability in DreadGOAD

We *do* want to hear about security issues in the **DreadGOAD tooling itself**,
which is everything that ships outside the deliberately-vulnerable lab content:

- The `dreadgoad` Go CLI (`cli/`)
- The Ansible collection and custom modules (`ansible/`)
- Terraform / Terragrunt modules (`infra/`, `modules/`)
- Warpgate AMI build templates (`warpgate-templates/`)
- Packer templates (`packer/`)
- Variant generator and other tooling (`tools/`)
- GitHub Actions workflows (`.github/workflows/`)

Examples of issues we consider in scope:

- Command injection, path traversal, or unsafe deserialization in the CLI or
  Python tooling
- Privilege escalation in deployment scripts that runs against the operator's
  workstation rather than the lab
- Exposure of operator credentials (AWS keys, Azure tokens, etc.) by the
  tooling — for example, leaking them into world-readable logs or remote state
- Supply-chain issues such as a compromised release artifact or a malicious
  dependency pin
- A cloud module that opens lab VMs to the public internet by default rather
  than gating them behind SSM / a private subnet

Examples of issues that are **not** vulnerabilities (please do not report
these — they are how the project works):

- Weak or known passwords on lab accounts
- Kerberoastable / AS-REP-roastable users
- Vulnerable ADCS templates, ACL misconfigurations, unconstrained delegation
- Plaintext credentials inside `ad/`, `ansible/`, or `extensions/` lab content
- The lab being exploitable end-to-end — that is the point

## How to report

Please report tooling vulnerabilities **privately** using GitHub's private
vulnerability reporting:

1. Go to <https://github.com/dreadnode/DreadGOAD/security/advisories/new>
2. Provide a clear description, affected version / commit, reproduction steps,
   and (if possible) a suggested fix or mitigation.

If GitHub private reporting is unavailable to you, you may instead open a
minimal public issue asking for a private contact channel — do not include
exploit details in the public issue.

Please do **not** report tooling vulnerabilities via public GitHub issues,
pull requests, discussions, or social media before we have had a chance to
respond.

## What to expect

- We will acknowledge receipt of your report within a few business days.
- We will work with you to confirm the issue and determine impact.
- Once a fix is ready, we will coordinate disclosure and credit you in the
  release notes (unless you prefer to remain anonymous).

## Supported versions

DreadGOAD is provided as-is for research and training. Security fixes are
applied to the `main` branch; users are expected to track `main` or the most
recent tagged release. Older releases do not receive backports.

---
title: DreadGOAD
---

<div align="center">
<img alt="GOAD" src="./img/logo_GOAD3.png">
</div>

Welcome to **DreadGOAD** -- a heavily modified fork of [GOAD (Game of Active Directory)](https://github.com/Orange-Cyberdefense/GOAD/) by Orange Cyberdefense.

DreadGOAD deploys vulnerable Active Directory lab environments for penetration testing and security research. It extends the upstream GOAD project with a Go CLI, AWS infrastructure automation, a modular extension system, and a variant generator for creating randomized lab copies.

!!! note
    GOAD main labs (GOAD/GOAD-Light/SCCM) are not pro labs environments (like those you can find on HTB). These labs give you an environment to practice a lot of vulnerability and misconfiguration exploitations. Consider GOAD like a DVWA but for Active Directory. If you want a challenge, deploy the NHA lab.

!!! warning
    This lab is extremely vulnerable. Do not reuse these configurations to build your production environment and do not deploy this environment on the internet without proper network isolation. Use at your own risk.

!!! info "Windows Licenses"
    This lab uses free Windows VM evaluation images (180-day trial). After that period, enter a license on each server or rebuild the lab.

## What's Different from GOAD?

- **Go CLI (`dreadgoad`)** -- single binary for provisioning, health checks, trust verification, and vulnerability validation
- **AWS infrastructure automation** -- Terragrunt/Terraform modules for deploying labs in AWS with SSM-based management
- **Modular extension system** -- plug-in extensions for ELK, Exchange, Wazuh, Guacamole, and more
- **Variant generator** -- create graph-isomorphic lab copies with randomized entity names while preserving all attack paths
- **Ansible collection (`dreadnode.goad`)** -- 80+ roles packaged as a reusable collection

## Acknowledgments

DreadGOAD is built on the excellent work of the [GOAD](https://github.com/Orange-Cyberdefense/GOAD) project by [Mayfly](https://github.com/Mayfly277) (Cyril Servieres) and [Orange Cyberdefense](https://github.com/Orange-Cyberdefense). If you find this useful, consider [sponsoring the original creator](https://github.com/sponsors/Mayfly277).

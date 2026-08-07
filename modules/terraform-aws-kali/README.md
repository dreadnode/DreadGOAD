# AWS Kali attack box

Deploys an optional Kali Linux attack box into a private DreadGOAD subnet. The
module uses the official Kali AWS Marketplace image family constrained to
Kali's Marketplace product ID, attaches an
`AmazonSSMManagedInstanceCore` instance profile, and installs the SSM agent and
the tools used by live scoring during first boot.

The instance is tagged `Role=AttackBox`, `Project=DreadGOAD`, and
`Environment=<env>` so the CLI can discover it without adding it to the Ansible
inventory.

The instance is private by default. Set `assign_public_ip = true` only for a
standalone deployment in a public subnet that has no NAT gateway or SSM VPC
endpoints. The module still creates no public ingress rule.

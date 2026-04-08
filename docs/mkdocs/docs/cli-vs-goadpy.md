# CLI vs `goad.py` — which tool to use

DreadGOAD ships **two tools, scoped strictly by provider**. They are not interchangeable.

- **`dreadgoad`** — the Go CLI (`cli/dreadgoad`). One-shot commands, declarative flags, no REPL. **AWS only** for everything operational. Drives the full AWS workflow: Terragrunt infrastructure, Warpgate golden AMIs, Ansible provisioning, AWS Session Manager access, vulnerability validation, and lab lifecycle.
- **`goad.sh`** (a wrapper around `goad.py`) — the Python REPL. The tool for **VirtualBox, VMware, Proxmox, Azure, and Ludus**. Drives Vagrant-based VM lifecycle and Ansible provisioning end-to-end. Still fully supported.

If you remember nothing else: **AWS → `dreadgoad`. Anything else → `goad.sh`.**

## Provider × tool support

| Provider     | Tool         | Notes                                                        |
|--------------|--------------|--------------------------------------------------------------|
| AWS          | `dreadgoad`  | Terragrunt + Warpgate + Ansible + SSM. No public ports.      |
| VirtualBox   | `goad.sh`    | Vagrant + Ansible.                                           |
| VMware       | `goad.sh`    | Vagrant + Ansible.                                           |
| Proxmox      | `goad.sh`    | Proxmoxer + Ansible.                                         |
| Azure        | `goad.sh`    | Azure SDK + Ansible.                                         |
| Ludus        | `goad.sh`    | Ludus API + Ansible.                                         |

> [!IMPORTANT]
> `dreadgoad`'s operational commands (`provision`, `health-check`, `validate`, `verify-trusts`, `inventory`, `lab`, `ssm`) all assume the lab is running on AWS EC2 and access it through Systems Manager. They will not work against a VirtualBox / VMware / Proxmox / Azure / Ludus deployment. For those providers, use `goad.sh` for the entire workflow.

## What `dreadgoad` does (AWS only)

Full lifecycle for an AWS lab:

| Command                                          | Purpose                                                       |
|--------------------------------------------------|---------------------------------------------------------------|
| `dreadgoad doctor`                               | Pre-flight checks (ansible-core, AWS CLI, Terragrunt, …)      |
| `dreadgoad ami build|list-resources|purge`       | Build / inspect / clean up Warpgate golden AMIs               |
| `dreadgoad infra init|plan|apply|destroy|output` | Manage Terragrunt-backed AWS infrastructure                   |
| `dreadgoad inventory sync|show|mapping`          | Sync the Ansible inventory with live EC2 instance IDs         |
| `dreadgoad provision`                            | Run the Ansible playbooks against the deployed lab            |
| `dreadgoad health-check`                         | Verify all instances are reachable and healthy via SSM        |
| `dreadgoad validate [--quick]`                   | Verify the intentional vulnerabilities are configured         |
| `dreadgoad verify-trusts`                        | Verify AD trust relationships across domains                  |
| `dreadgoad lab status|start|stop`                | Lab-wide EC2 instance lifecycle                               |
| `dreadgoad lab start-vm|stop-vm|restart-vm`      | Per-host EC2 lifecycle                                        |
| `dreadgoad ssm connect|run|status|cleanup`       | AWS Session Manager access — no open ports                    |
| `dreadgoad env create|list`                      | Manage multiple deployment environments (dev / staging / prod)|

## What `goad.sh` / `goad.py` does (everything else)

Full lifecycle for VirtualBox, VMware, Proxmox, Azure, and Ludus, via an interactive REPL:

| REPL command                       | Purpose                                                      |
|------------------------------------|--------------------------------------------------------------|
| `set_lab <lab>` / `set_provider`   | Select the lab and provider for the current session          |
| `check`                            | Pre-flight checks                                             |
| `install`                          | Create the VMs and run all provisioning playbooks            |
| `provide`                          | Create the VMs only (Vagrant / API / etc.)                   |
| `provision_lab`                    | Run the full Ansible playbook sequence                       |
| `provision_lab_from <playbook>`    | Resume provisioning from a specific playbook                 |
| `start` / `stop` / `status`        | Lab-wide VM lifecycle                                         |
| `start_vm` / `stop_vm` / `destroy_vm` | Per-VM lifecycle                                          |
| `snapshot` / `reset`               | Snapshot and restore VM state                                |
| `ssh_jumpbox` / `ssh_jumpbox_proxy`| Access lab VMs through the jumpbox                           |
| `config`                           | Show current settings                                         |
| `?`                                | Interactive help                                              |

There is also `goad_docker.sh`, which runs the same tool with `-m docker -d local -d runner` to drive Ansible from inside a Docker container.

## Provider-agnostic `dreadgoad` utilities

A handful of `dreadgoad` subcommands have **no AWS dependency** and can be used regardless of which tool you use to deploy your lab:

- `dreadgoad config show|init|set` — manage `~/.config/dreadgoad/dreadgoad.yaml`
- `dreadgoad variant generate ...` — generate graph-isomorphic randomized lab copies (operates on lab definition files in `ad/`)
- `dreadgoad lab-list` — list available labs

These are file/config utilities, not operational commands.

## Mental model: AWS workflow vs `goad.py` workflow

If you've used the legacy Python REPL and are wondering "what does that look like on AWS?":

| Goal                                        | `goad.py` REPL (any non-AWS provider) | `dreadgoad` (AWS only)                       |
|---------------------------------------------|---------------------------------------|----------------------------------------------|
| Pre-flight checks                           | `check`                               | `dreadgoad doctor`                           |
| Build / customize the base VM image         | (Packer or vendor's default image)    | `dreadgoad ami build <template>`             |
| Create the VMs                              | `install` or `provide`                | `dreadgoad infra init && infra apply`        |
| Run all provisioning playbooks              | `provision_lab`                       | `dreadgoad provision`                        |
| Re-run from a specific playbook             | `provision_lab_from <playbook>`       | `dreadgoad provision --from <playbook>`      |
| Show effective configuration                | `config`                              | `dreadgoad config show`                      |
| Stop / start / status                       | `stop` / `start` / `status`           | `dreadgoad lab stop|start|status`            |
| Get a shell on a host                       | `ssh_jumpbox` then SSH                | `dreadgoad ssm connect <host>`               |
| Tear it down                                | `destroy`                             | `dreadgoad infra destroy`                    |
| Verify it works as intended                 | (manual)                              | `dreadgoad health-check`                     |
| Verify the vulnerabilities are configured   | (manual)                              | `dreadgoad validate --quick`                 |
| Verify AD trusts                            | (manual)                              | `dreadgoad verify-trusts`                    |

The right column **only** applies to AWS deployments. If your lab is running on VirtualBox, VMware, Proxmox, Azure, or Ludus, use the left column.

## Long-term direction

The Go CLI is where new functionality is being added, but it is currently AWS-scoped by design. `goad.py` is **not** deprecated — it remains the supported way to drive every non-AWS provider, and it will continue to receive bug fixes. If support for other providers is added to `dreadgoad` in the future, this page will be updated.

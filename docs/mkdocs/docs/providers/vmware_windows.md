# VMware on Windows

This guide covers deploying GOAD labs using VMware Workstation on a **Windows host**, with Ansible running from a Kali or Ubuntu VM inside the same VMware instance.

## Prerequisites

### On the Windows host

- [VMware Workstation Pro](https://www.vmware.com/products/workstation-pro.html) (Pro required -- Player does not support clone/snapshot)
- [Vagrant for Windows](https://developer.hashicorp.com/vagrant/install#Windows)
- [VMware Utility Driver](https://developer.hashicorp.com/vagrant/install/vmware)
- Vagrant plugins:
    - `vagrant-reload`
    - `vagrant-vmware-desktop`

### Controller VM (Kali or Ubuntu)

You need a Linux VM inside VMware Workstation to run Ansible. Configure it with **two network adapters**:

1. **NAT or Bridged** -- for internet access
2. **Host-only** -- on the same subnet as the GOAD lab (`192.168.56.0/24`)

Use VMware Workstation's Virtual Network Editor to configure the host-only network with subnet `192.168.56.0` and netmask `255.255.255.0`.

Inside the controller VM, install dependencies:

```bash
pip install --upgrade pip
pip install ansible-core pywinrm
sudo apt install sshpass lftp rsync openssh-client

# Install Ansible requirements
cd DreadGOAD
ansible-galaxy collection install -r ansible/requirements.yml
```

## Create the VMs

From a Windows PowerShell or cmd prompt:

```powershell
cd ad\GOAD\providers\vmware
vagrant up
```

This pulls down and starts the lab VMs. Wait for it to complete before proceeding.

## Provision with Ansible

Once the VMs are running, switch to your Kali/Ubuntu controller VM and run the provisioning:

```bash
cd DreadGOAD

# Using the CLI
./cli/dreadgoad provision

# Or using the legacy script
./goad.sh -t install -l GOAD -p vmware -m local -a
```

The `-a` flag skips interactive prompts and runs with defaults.

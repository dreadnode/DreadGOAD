# :simple-vmware: Vmware

!!! quote
    "Virtualbox c'est no way" @mpgn

<div align="center">
  <img alt="vagrant" width="153" height="150" src="../img/icon_vagrant.png">
  <img alt="icon_vwmare" width="176"  height="150" src="../img/icon_vwmare.png">
  <img alt="icon_ansible" width="150"  height="150" src="../img/icon_ansible.png">
</div>

## Prerequisites

- Providing
    - [Vmware workstation](https://support.broadcom.com/group/ecx/productdownloads?subfamily=VMware+Workstation+Pro)
    - [Vagrant](https://developer.hashicorp.com/vagrant/docs)
    - [Vmware utility driver](https://developer.hashicorp.com/vagrant/install/vmware)
    - Vagrant plugins:
        - vagrant-reload
        - vagrant-vmware-desktop
        - winrm
        - winrm-fs
        - winrm-elevated

- Provisioning
    - Ansible (installed via the DreadGOAD CLI prerequisites)
    - ansible-galaxy requirements (`ansible-galaxy collection install -r ansible/requirements.yml`)


## check dependencies

```bash
dreadgoad doctor
```

![vmware_check.png](./../img/vmware_check.png)

!!! info
    If there is some missing dependencies goes to the [installation](../installation/index.md) chapter and follow the guide according to your os.

!!! note
    check give mandatory dependencies in red and non mandatory in yellow (but you should be compliant with them too depending one your operating system)

## Install

- Once Vagrant has created the VMs, provision the lab using the DreadGOAD CLI:

```bash
dreadgoad provision
```

![vmware_install](./../img/vmware_install.png)

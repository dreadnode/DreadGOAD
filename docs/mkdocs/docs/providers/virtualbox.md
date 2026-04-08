# :simple-virtualbox: Virtualbox

<div align="center">
  <img alt="vagrant" width="153" height="150" src="../img/icon_vagrant.png">
  <img alt="icon_virtualbox" width="150"  height="150" src="../img/icon_virtualbox.png">
  <img alt="icon_ansible" width="150"  height="150" src="../img/icon_ansible.png">
</div>

## Prerequisites

- Providing
    - [Virtualbox](https://www.virtualbox.org/)
    - [Vagrant](https://developer.hashicorp.com/vagrant/docs)
    - Vagrant plugins:
        - vagrant-reload
        - vagrant-vbguest
        - winrm
        - winrm-fs
        - winrm-elevated

- Provisioning
    - Ansible (installed via the DreadGOAD CLI prerequisites)
    - ansible-galaxy requirements (`ansible-galaxy collection install -r ansible/requirements.yml`)


## Check dependencies

```bash
dreadgoad doctor
```

![vbox_check_example.png](./../img/vbox_check_example.png)

!!! info
    If there is some missing dependencies goes to the [installation](../installation/index.md) chapter and follow the guide according to your os.

!!! note
    check give mandatory dependencies in red and non mandatory in yellow (but you should be compliant with them too depending one your operating system)

## Install

- Once Vagrant has created the VMs, provision the lab using the DreadGOAD CLI:

```bash
dreadgoad provision
```

![vbox_install](./../img/vbox_install.png)

# :material-microsoft-windows: Windows

- First you will prepare your windows host for a hypervisor
- Second you will install the `dreadgoad` CLI

## Prepare Windows Host

=== ":simple-virtualbox: Virtualbox"
    If you want to use virtualbox as a hypervisor to create your vm.

    - VAGRANT

        If you want to create the lab on your windows computer you will need vagrant. Vagrant will be responsible to automate the process of vm download and creation.

        - Download and install visual c++ 2019   : [https://aka.ms/vs/17/release/vc_redist.x64.exe](https://aka.ms/vs/17/release/vc_redist.x64.exe)
        - Install vagrant : [https://developer.hashicorp.com/vagrant/install](https://developer.hashicorp.com/vagrant/install)

    - Virtualbox

        - Install virtualbox <= 7.1.x (vagrant supports up to vbox7.1.x at the time of writing) : [https://www.virtualbox.org/wiki/Downloads](https://www.virtualbox.org/wiki/Downloads)

        - Install the following vagrant plugins:

        ```
        vagrant.exe plugin install vagrant-reload vagrant-vbguest winrm winrm-fs winrm-elevated
        ```

    !!! warning "Disk space"
        The lab takes about 77GB (but you have to get the space for the vms vagrant images windows server 2016 (22GB) / windows server 2019 (14GB) / ubuntu 18.04 (502M))
        The total space needed for the lab is ~115 GB (depend on the lab you use and it will take more space if you take snapshots), be sure you have enough disk space before install.

    !!! warning "RAM"
        Depending on the lab you will need a lot of ram to run all the virtual machines. Be sure to have at least 20GB for GOAD-Light and 24GB for GOAD.

=== ":simple-vmware: Vmware Workstation"

    If you want to use vmware workstation as an hypervisor to create your vm.

    !!! tip
        Vmware workstation is now free for personal use !

    - VAGRANT

        If you want to create the lab on your windows computer you will need vagrant. Vagrant will be responsible to automate the process of vm download and creation.

        - Download and install visual c++ 2019   : [https://aka.ms/vs/17/release/vc_redist.x64.exe](https://aka.ms/vs/17/release/vc_redist.x64.exe)
        - Install vagrant : [https://developer.hashicorp.com/vagrant/install](https://developer.hashicorp.com/vagrant/install)

    - Vmware Workstation
        - Install vmware workstation : [https://support.broadcom.com/group/ecx/productdownloads?subfamily=VMware+Workstation+Pro](https://support.broadcom.com/group/ecx/productdownloads?subfamily=VMware+Workstation+Pro)

        !!! bug "vmware workstation install bug"
            if you got an error about groups and permission during vmware workstation install consider running this in an administrator cmd prompt:
            ```
            net localgroup /add "Users"
            net localgroup /add "Authenticated Users"
            ```

        - Install vagrant vmware utility : [https://developer.hashicorp.com/vagrant/install/vmware](https://developer.hashicorp.com/vagrant/install/vmware)

        - Install the following vagrant plugins:

        ```
        vagrant.exe plugin install vagrant-reload vagrant-vmware-desktop winrm winrm-fs winrm-elevated
        ```

    !!! warning "Disk space"
        The lab takes about 77GB (but you have to get the space for the vms vagrant images windows server 2016 (22GB) / windows server 2019 (14GB) / ubuntu 18.04 (502M))
        The total space needed for the lab is ~115 GB (depend on the lab you use and it will take more space if you take snapshots), be sure you have enough disk space before install.

    !!! warning "RAM"
        Depending on the lab you will need a lot of ram to run all the virtual machines. Be sure to have at least 20GB for GOAD-Light and 24GB for GOAD.


=== ":simple-amazon: Aws"
    Nothing to prepare on windows host, install and prepare WSL and next follow linux install from your WSL console : [see aws linux install](linux.md/#__tabbed_1_4)

=== ":material-microsoft-azure: Azure"
    Nothing to prepare on windows host, install and prepare WSL and next follow linux install from your WSL console [see azure linux install](linux.md/#__tabbed_1_3)

=== ":simple-proxmox: Promox"
    Not supported, you will have to create a provisioning machine on your proxmox and run dreadgoad from there ([see proxmox linux install](linux.md/#__tabbed_1_5))

=== "🏟️  Ludus"
    Not supported, you will have to act from your ludus server ([see ludus linux install](linux.md/#__tabbed_1_6))

## Install the CLI

=== "With WSL"
    Now your host environment is ready for virtual machine creation. Install WSL to run the `dreadgoad` CLI and Ansible.

    !!! info "wsl version"
        New Linux installations, installed using the wsl --install command, will be set to WSL 2 by default.
        The wsl --set-version command can be used to downgrade from WSL 2 to WSL 1 or to update previously installed Linux distributions from WSL 1 to WSL 2.
        To see whether your Linux distribution is set to WSL 1 or WSL 2, use the command: `wsl -l -v`.
        To change versions, use the command: `wsl --set-version <distro name> <wsl_version>` replacing <distro name> with the name of the Linux distribution that you want to update.
        As an example: `wsl --set-version Debian 1` will set your Debian distribution to use WSL 1.

    !!! tip "use wsl version1"
        by now wsl was tested successfully with version 1

    ### Install WSL

    - First install wsl on your environment [https://learn.microsoft.com/en-us/windows/wsl/install](https://learn.microsoft.com/en-us/windows/wsl/install)
    - Next go to the microsoft store and install debian (debian12)

    ### Install dreadgoad in WSL

    - Open debian console then :

        - Install Ansible dependencies
        ```bash
        sudo apt update
        sudo apt install python3 python3-pip ansible-core sshpass
        ```

        - Install the `dreadgoad` CLI
        ```bash
        # Download the latest Linux binary
        curl -LO https://github.com/dreadnode/DreadGOAD/releases/latest/download/dreadgoad-linux-amd64
        chmod +x dreadgoad-linux-amd64
        sudo mv dreadgoad-linux-amd64 /usr/local/bin/dreadgoad
        ```

        - Or build from source
        ```bash
        cd /mnt/c/whatever_folder_you_want
        git clone https://github.com/dreadnode/DreadGOAD.git
        cd DreadGOAD/cli
        go build -o dreadgoad .
        sudo mv dreadgoad /usr/local/bin/
        ```

    - Initialize and verify
    ```bash
    dreadgoad config init
    dreadgoad doctor
    ```

=== "Direct Windows Install"

    !!! info "For vmware or virtualbox only"
        This mode runs `dreadgoad` natively on Windows. Ansible provisioning still requires WSL or a remote provisioning machine.

    - Prerequisites:
        - :simple-git: [git](https://git-scm.com/downloads/win)

    - Download the Windows binary from the [DreadGOAD releases page](https://github.com/dreadnode/DreadGOAD/releases):
        ```
        curl -LO https://github.com/dreadnode/DreadGOAD/releases/latest/download/dreadgoad-windows-amd64.exe
        move dreadgoad-windows-amd64.exe C:\Users\%USERNAME%\bin\dreadgoad.exe
        ```

    - Add `C:\Users\%USERNAME%\bin` to your PATH if not already present.

    - Initialize and verify:
        ```
        dreadgoad config init
        dreadgoad doctor
        ```

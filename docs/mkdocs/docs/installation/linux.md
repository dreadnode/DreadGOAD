<!-- markdownlint-disable MD046 -->
# :material-linux: Linux

- First you will prepare your host for a hypervisor
- Second you will install the `dreadgoad` CLI

## Prepare your Provider

=== ":simple-virtualbox: Virtualbox"

    - Vagrant
        - In order to download vm and create them on virtualbox you need to install vagrant
        - [https://developer.hashicorp.com/vagrant/install#linux](https://developer.hashicorp.com/vagrant/install#linux)

    - Virtualbox
        - Install virtualbox
        ```bash
        sudo apt install virtualbox
        ```

    - Install vagrant plugins
    ```bash
    vagrant plugin install vagrant-reload vagrant-vbguest winrm winrm-fs winrm-elevated
    ```

    !!! warning "Disk space"
        The lab takes about 77GB (but you have to get the space for the vms vagrant images windows server 2016 (22GB) / windows server 2019 (14GB) / ubuntu 18.04 (502M))
        The total space needed for the lab is ~115 GB (depend on the lab you use and it will take more space if you take snapshots), be sure you have enough disk space before install.

    !!! warning "RAM"
        Depending on the lab you will need a lot of ram to run all the virtual machines. Be sure to have at least 20GB for GOAD-Light and 24GB for GOAD.

=== ":simple-vmware: Vmware workstation"

    !!! tip
        Vmware workstation is now free for personal use !

    - Vagrant
        - In order to download vm and create them on virtualbox you need to install vagrant
        - [https://developer.hashicorp.com/vagrant/install#linux](https://developer.hashicorp.com/vagrant/install#linux)

    - Vmware workstation
        - Install vmware workstation [https://support.broadcom.com/group/ecx/productdownloads?subfamily=VMware+Workstation+Pro](https://support.broadcom.com/group/ecx/productdownloads?subfamily=VMware+Workstation+Pro)

    - Install vagrant vmware utility : [https://developer.hashicorp.com/vagrant/install/vmware](https://developer.hashicorp.com/vagrant/install/vmware#linux)

    - Install the following vagrant plugins:
        ```
        vagrant plugin install vagrant-reload vagrant-vmware-desktop winrm winrm-fs winrm-elevated
        ```

    !!! warning "Disk space"
        The lab takes about 77GB (but you have to get the space for the vms vagrant images windows server 2016 (22GB) / windows server 2019 (14GB) / ubuntu 18.04 (502M))
        The total space needed for the lab is ~115 GB (depend on the lab you use and it will take more space if you take snapshots), be sure you have enough disk space before install.

    !!! warning "RAM"
        Depending on the lab you will need a lot of ram to run all the virtual machines. Be sure to have at least 20GB for GOAD-Light and 24GB for GOAD.

=== ":material-microsoft-azure: Azure"
    - Azure CLI
        - Install azure cli
            [https://learn.microsoft.com/en-us/cli/azure/install-azure-cli-linux](https://learn.microsoft.com/en-us/cli/azure/install-azure-cli-linux?pivots)
        - Connect to azure :
            ```bash
            az login
            ```
    - Terraform
        - The installation to Azure use terraform so you will have to install it: [https://developer.hashicorp.com/terraform/install](https://developer.hashicorp.com/terraform/install)


=== ":simple-amazon: Aws"
    - AWS CLI

        - Install aws cli
            [https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html#getting-started-install-instructions](https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html#getting-started-install-instructions)
        - Create an aws access key and secret for goad usage
            - Go to IAM > User > your user > Security credentials
            - Click the Create access key button
            - Create a group "[goad]" in credentials file ~/.aws/credentials
                ```
                [goad]
                aws_access_key_id = changeme
                aws_secret_access_key = changeme
                ```
            - Be sure to chmod 400 the file

            !!! warning "credentials in plain text"
                Storing credentials in plain text is always a bad idea, but aws cli work like that be sure to restrain the right access to this file

    - Terraform
        - The installation to Aws use terraform so you will have to install it: [https://developer.hashicorp.com/terraform/install](https://developer.hashicorp.com/terraform/install)

=== ":simple-proxmox: Proxmox"

    - Proxmox install is very complex and use a lot of steps
    - A complete guide to proxmox installation is available here : [https://mayfly277.github.io/categories/proxmox/](https://mayfly277.github.io/categories/proxmox/)

=== "🏟️  Ludus"

    - To add GOAD on Ludus please use dreadgoad directly on the server.
    - By now dreadgoad can work only directly on the server and not from a workstation client.

    - Install Ludus : [https://docs.ludus.cloud/docs/quick-start/install-ludus/](https://docs.ludus.cloud/docs/quick-start/install-ludus/)

    - Be sure to create an administrator user and keep his api key

    - Once your installation is complete on ludus server (debian 12) and your user is created do :

    ```bash
    git clone https://github.com/dreadnode/DreadGOAD.git
    cd DreadGOAD

    # Install the CLI (see "Install the CLI" section below)
    # Then:
    dreadgoad config init
    dreadgoad config set ludus.api_key <your_api_key>
    dreadgoad provision -p ludus
    ```

## Install the CLI

=== "Download binary"

    Download the latest release for your platform from the [DreadGOAD releases page](https://github.com/dreadnode/DreadGOAD/releases):

    ```bash
    # Example for Linux amd64 - adjust version and platform as needed
    curl -LO https://github.com/dreadnode/DreadGOAD/releases/latest/download/dreadgoad-linux-amd64
    chmod +x dreadgoad-linux-amd64
    sudo mv dreadgoad-linux-amd64 /usr/local/bin/dreadgoad
    ```

    Verify the installation:

    ```bash
    dreadgoad --version
    ```

=== "Build from source"

    Requires Go 1.21+ installed on your system.

    ```bash
    git clone https://github.com/dreadnode/DreadGOAD.git
    cd DreadGOAD/cli
    go build -o dreadgoad .
    sudo mv dreadgoad /usr/local/bin/
    ```

    Verify the installation:

    ```bash
    dreadgoad --version
    ```

=== "Go install"

    If you have Go installed:

    ```bash
    go install github.com/dreadnode/DreadGOAD/cli@latest
    ```

    Make sure `$GOPATH/bin` is in your `PATH`.

## Initial setup

Once the CLI is installed, initialize your configuration and verify dependencies:

```bash
# Create default configuration
dreadgoad config init

# Check all required dependencies are installed
dreadgoad doctor
```

!!! tip
    `dreadgoad doctor` checks for ansible-core, AWS CLI, Python (for Ansible), and required Ansible collections. Fix any issues it reports before proceeding with installation.

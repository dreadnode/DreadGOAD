# :material-ansible: provisioning

This page describe how the provisioning is done with goad.
The provisioning of the LABS is done with Ansible for all providers.

- First the GOAD install script create an instance folder in the workspace folder.

## Lab data

The data of each lab are stored in the json file : `ad/<lab>/data/config.json`, this file is loaded by each playbook to get all the lab variables (this is done by the data.yml playbook call by all the over playbooks)

## Extension data

If an extension need data it will be stored in `extensions/<extension>/data/config.json` but the loading must be done by extension install.yml playbook.

- Example with the exchange install.yml file :

```yaml
# read local configuration file
- name: "Read local config file"
  hosts: domain:extensions
  connection: local
  vars_files:
    - "../data/config.json"
  tasks:
    - name: merge lab variable with local config
      set_fact:
        lab: "{{ lab|combine(lab_extension, recursive=True) }}"
```

## Inventories

Ansible work with inventories. Inventories files contains all the hosts declaration and some variables.

- The lab inventory file (`ad/<lab>/data/inventory`) is not modified/moved and contain all the main variables and hosts association, this file stay as this and is not modified. It contains the lab building logic.

- The provider inventory file (`ad/<lab>/provider/<provider>/inventory`) is modified with the settings and copied into the workspace folder (`workspace/<instance_id>/inventory`) , this file contains variable specific to the provider and the host ip declaration

- The extension(s) inventory file(s) (`extensions/<extension>/inventory`) is modified with the settings and copied into the workspace folder (`workspace/<instance_id>/inventory_<extension>`) , this file contains variable specific to the extension and the extension host ip declaration

- The global inventory file `globalsettings.ini`contains some global variable with some user settings.


The inventory files are given to ansible in this order :

- lab inventory file
- workspace provider inventory file
- workspace extension(s) inventory file(s)
- globalsettings.ini file

The order is important as it determine the override order. hosts declarations are merged between all inventory and variables with the same name are override if the same variable is declared.

- Example : if i setup dns_server_forwarder=8.8.8.8 in the lab inventory file and dns_server_forwarder=1.1.1.1 in the globalsettings.ini file, the final value for ansible will be dns_server_forwarder=1.1.1.1

## playbooks

- Labs playbook are stored on the ansible/ folder
- Extension playbook is stored in `extension/<extension>/ansible/install.yml`
- The extension folder can call the main goad roles by using a special ansible.cfg file.

- Example of the exchange ansible.cfg file

```ini
[defaults]
...
; add default roles folder into roles_path
roles_path = ./roles:../../../ansible/roles
```

## Running Ansible from Docker

If you prefer not to install Ansible locally, you can provision from a Docker container:

```bash
# Build the container
docker build -t goadansible .

# Run provisioning
docker run -ti --rm --network host -h goadansible \
  -v $(pwd):/goad -w /goad/ansible goadansible \
  ansible-playbook \
    -i ../ad/<LAB>/data/inventory \
    -i ../ad/<LAB>/providers/<PROVIDER>/inventory \
    main.yml
```

`--network host` is required so the container can reach the lab VMs on the host-only network (e.g. `192.168.56.0/24`).

## Individual Playbooks

The `main.yml` playbook runs all steps in sequence. For debugging or partial re-provisioning, you can run each playbook individually. The order matters:

```bash
ANSIBLE_CMD="ansible-playbook -i ../ad/GOAD/data/inventory -i ../ad/GOAD/providers/virtualbox/inventory"
$ANSIBLE_CMD build.yml            # Install prerequisites and prepare VMs
$ANSIBLE_CMD ad-servers.yml       # Create main domains, child domain, enroll servers
$ANSIBLE_CMD ad-parent_domain.yml # Create parent domain
$ANSIBLE_CMD ad-child_domain.yml  # Create child domain
sleep 5m                          # Allow replication to settle
$ANSIBLE_CMD ad-members.yml       # Add child domain members
$ANSIBLE_CMD ad-trusts.yml        # Create trust relationships
$ANSIBLE_CMD ad-data.yml          # Import AD data (users, groups, OUs)
$ANSIBLE_CMD ad-gmsa.yml          # Configure gMSA
$ANSIBLE_CMD laps.yml             # Configure LAPS
$ANSIBLE_CMD ad-relations.yml     # Set ACE/ACL and cross-domain group relations
$ANSIBLE_CMD adcs.yml             # Install ADCS
$ANSIBLE_CMD ad-acl.yml           # Configure ACL attack paths
$ANSIBLE_CMD servers.yml          # Install IIS and MSSQL
$ANSIBLE_CMD security.yml         # Configure security settings (Defender, etc.)
$ANSIBLE_CMD vulnerabilities.yml  # Configure intentional vulnerabilities
$ANSIBLE_CMD reboot.yml           # Reboot all VMs
```

!!! tip
    If a playbook fails, you can usually just re-run it. Most transient failures are caused by Windows latency during installation. Wait a few minutes and retry.

## Vagrant VM Management

Common Vagrant commands for managing lab VMs:

```bash
vagrant up              # Start all VMs (or create if first run)
vagrant up <vmname>     # Start a specific VM
vagrant halt            # Stop all VMs
vagrant destroy         # Delete all VMs (irreversible)
vagrant snapshot push   # Save a snapshot of all VMs
vagrant snapshot pop    # Restore the last snapshot
```

!!! warning
    `vagrant snapshot pop` can break domain trust relationships between servers. After restoring a snapshot, run the `fix_trust.yml` playbook to re-establish trusts.

## Disabling the Vagrant User

All VMs are deployed with default credentials `vagrant:vagrant` from the base templates. To remove this backdoor after provisioning:

```bash
ansible-playbook -i ../ad/<LAB>/data/inventory -i ../ad/<LAB>/providers/<PROVIDER>/inventory disable_vagrant.yml
```

To re-enable (e.g. for maintenance):

```bash
ansible-playbook -i ../ad/<LAB>/data/inventory -i ../ad/<LAB>/providers/<PROVIDER>/inventory enable_vagrant.yml
```

## Labs build

- Instead of call a global main.yml playbook with all the different tasks to do the goad script call each playbook one by one.
- In this way, there is a fallback mechanism to retry each playbook 3 times before consider it as failed.
- The list and order of the playbooks played are stored in the playbooks.yml file at the start of the project.

#cloud-config
# Bootstraps an Ansible controller for DreadGOAD. Ansible + WinRM/PSRP deps
# live in /opt/ansible-venv so the system Python stays clean. Symlinks land
# in /usr/local/bin so the admin user picks them up without activation.

package_update: true
package_upgrade: false

packages:
  - python3-venv
  - python3-pip
  - git
  - jq
  - openssh-client

write_files:
  - path: /etc/profile.d/dreadgoad-controller.sh
    permissions: "0644"
    content: |
      export ANSIBLE_HOST_KEY_CHECKING=False
      export PATH="/opt/ansible-venv/bin:$PATH"

runcmd:
  - [bash, -lc, "python3 -m venv /opt/ansible-venv"]
  - [bash, -lc, "/opt/ansible-venv/bin/pip install --upgrade pip"]
  # NTLM is enough for the GOAD lab; the kerberos extra pulls pykerberos
  # which needs libkrb5-dev and a C toolchain to build. Add it back via a
  # follow-up apt install + pip install if a playbook actually requires it.
  - [bash, -lc, "/opt/ansible-venv/bin/pip install ansible-core pywinrm pypsrp"]
  - [bash, -lc, "ln -sf /opt/ansible-venv/bin/ansible /usr/local/bin/ansible"]
  - [bash, -lc, "ln -sf /opt/ansible-venv/bin/ansible-playbook /usr/local/bin/ansible-playbook"]
  - [bash, -lc, "ln -sf /opt/ansible-venv/bin/ansible-galaxy /usr/local/bin/ansible-galaxy"]
%{ for collection in collections ~}
  - [bash, -lc, "sudo -u ${admin_user} /opt/ansible-venv/bin/ansible-galaxy collection install ${collection}"]
%{ endfor ~}
  - [bash, -lc, "install -d -o ${admin_user} -g ${admin_user} -m 0755 /home/${admin_user}/playbooks"]

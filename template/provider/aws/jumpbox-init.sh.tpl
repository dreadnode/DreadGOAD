#!/bin/bash
usermod -l "${username}" ubuntu  
usermod -d "/home/${username}" -m ${username}
sed -i "s/ubuntu/${username}/" /etc/sudoers.d/90-cloud-init-users

# Install ansible and set up symbolic link
apt update
apt install -y ansible
mkdir -p /home/${username}/.local/bin
ln -s /usr/bin/ansible-playbook /home/${username}/.local/bin/ansible-playbook
chown -R ${username}:${username} /home/${username}/.local
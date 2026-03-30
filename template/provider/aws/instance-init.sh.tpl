#!/bin/bash
# shellcheck disable=SC2154
usermod -l "${username}" ubuntu
usermod -d "/home/${username}" -m "${username}"
sed -i "s/ubuntu/${username}/" /etc/sudoers.d/90-cloud-init-users
echo "${username}":"${password}" | chpasswd

#cloud-config
# Deploy SSH public key for the Kali attack box user. The marketplace image
# ships a pre-existing "kali" user that Azure's guest agent does not always
# reconcile correctly, so we handle it explicitly here.
#
# Order of operations: bootcmd → write_files → runcmd
# bootcmd ensures ~/.ssh exists before write_files drops authorized_keys,
# then package_update + packages install scoring/recon deps,
# then runcmd fixes ownership and creates tool wrappers.

bootcmd:
  - [install, -d, -o, "${admin_user}", -m, "0700", "/home/${admin_user}/.ssh"]

package_update: true

packages:
  - netexec
  - python3-impacket
  - impacket-scripts
  - dnsutils
  - python3-pip

write_files:
  - path: /home/${admin_user}/.ssh/authorized_keys
    content: |
      ${public_key}
    permissions: "0600"
  - path: /usr/local/bin/secretsdump.py
    content: |
      #!/bin/sh
      exec impacket-secretsdump "$@"
    permissions: "0755"

runcmd:
  - [chown, "${admin_user}", "/home/${admin_user}/.ssh/authorized_keys"]

#cloud-config
# Deploy SSH public key for the Kali attack box user. The marketplace image
# ships a pre-existing "kali" user that Azure's guest agent does not always
# reconcile correctly, so we handle it explicitly here.
#
# Order of operations: bootcmd → write_files → runcmd
# bootcmd ensures ~/.ssh exists before write_files drops authorized_keys,
# then runcmd fixes ownership (write_files runs as root).

bootcmd:
  - [install, -d, -o, "${admin_user}", -m, "0700", "/home/${admin_user}/.ssh"]

write_files:
  - path: /home/${admin_user}/.ssh/authorized_keys
    content: |
      ${public_key}
    permissions: "0600"

runcmd:
  - [chown, "${admin_user}", "/home/${admin_user}/.ssh/authorized_keys"]

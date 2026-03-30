# WAZUH extension

- Extension Name: wazuh
- Description: Add wazuh free EDR server and agent on all the domain computers + soc fortress rules (https://github.com/socfortress/Wazuh-Rules)
- Machine name : {{lab_name}}-WAZUH
- Compatible with labs : *

## prerequisites

On ludus prepare template :

```bash
ludus templates add -d ubuntu-22.04-x64-server
ludus templates build
```

## Install

```bash
instance_id> install_extension wazuh
```


## credits

- https://github.com/aleemladha (https://github.com/Orange-Cyberdefense/GOAD/pull/215)

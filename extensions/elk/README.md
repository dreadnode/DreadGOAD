# ELK extension

- Extension Name: elk
- Description: Add an ELK to the current lab
- Machine name : {{lab_name}}-ELK
- Compatible with labs : *

## prerequisites

On ludus prepare template :

```bash
ludus templates add -d ubuntu-22.04-x64-server
ludus templates build
```

## Install

```bash
instance_id> install_extension elk
```

- machine: {{lab_name}}-ELK
- filebeat agent domain computer machines


## Uninstall

- Not implemented yet

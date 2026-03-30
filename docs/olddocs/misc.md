## MISC commands

### Force replication (no more used)

- On dragonstone play as domain admin user :

```bash
repadmin /replicate kingslanding.sevenkingdoms.local dragonstone.sevenkingdoms.local dc=sevenkingdoms,dc=local /full
```

### vagrant useful commands (vm management)

- start all lab vms :

```bash
vagrant up
```

- start only one vm :

```bash
vagrant up <vmname>
```

- stop all the lab vm :

```bash
vagrant halt
```

- drop all the lab vm (because you want to recreate all) (carrefull : this will erase all your lab instance)

```bash
vagrant destroy
```

- snapshot the lab (https://www.vagrantup.com/docs/cli/snapshot)

```bash
vagrant snapshot push
```

- restore the lab snapshot (this could break servers relationship, reset servers passwords with fix_trust.yml playbook)

```bash
vagrant snapshot pop
```

### ansible commands (provisioning management)

#### Play only an ansible part

- only play shares of member_server.yml :

```bash
ansible-playbook member_server.yml --tags "data,shares"
```

#### Play only on some server

```bash
ansible-playbook -l dc2 domain_controller.yml
```

#### Add some vulns

```bash
ansible-playbook vulnerabilities.yml
```

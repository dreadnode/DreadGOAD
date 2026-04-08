# DRACARYS

![DRACARYS](../img/dracarys_logo.png)

- DRACARYS is written as a training challenge where GOAD was written as a lab with a maximum of vulns.
- You should find your way in to get domain admin on the domain dracarys.lab
- Using vagrant user is prohibited of course ^^
- Starting point is on lx01 : `<ip_range>.12`
- Obviously do not cheat by looking at the passwords and flags in the recipe files, the lab must start without user to full compromise.
- Install :

```bash
dreadgoad -l DRACARYS -p virtualbox install
```

- Once install finishes, disable the vagrant user to avoid using it :

```bash
dreadgoad -l DRACARYS -p virtualbox disable-vagrant
```

- Now reboot all the machines to avoid unintended secrets stored :

```bash
dreadgoad -l DRACARYS -p virtualbox stop
dreadgoad -l DRACARYS -p virtualbox start
```

And you are ready to play ! :)

- If you need to re-enable vagrant

```bash
dreadgoad -l DRACARYS -p virtualbox enable-vagrant
```

- If you want to create a write up of the chall, no problem, have fun. Please ping me on X (@M4yFly) or Discord, i will be happy to read it :)

!!! tip
    Be sure to get your arsenal up to date

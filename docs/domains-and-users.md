# DreadGOAD - Domains, Hosts & Users

## Network Overview

The GOAD lab consists of **3 Active Directory domains** across **2 forests** with a bidirectional trust between them.

```text
Forest: sevenkingdoms.local          Forest: essos.local
├── sevenkingdoms.local (root)       └── essos.local (root)
│   └── north.sevenkingdoms.local         DC: meereen (DC03)
│       (child domain)                    Server: braavos (SRV03)
│       DC: winterfell (DC02)
│       Server: castelblack (SRV02)
│
└── DC: kingslanding (DC01)

Trust: sevenkingdoms.local <──bidirectional──> essos.local
```

## Hosts & IP Addresses

| Host | Hostname | Domain | Role |
| ------ | ---------- | -------- | ------ |
| DC01 | kingslanding | sevenkingdoms.local | Domain Controller (parent) |
| DC02 | winterfell | north.sevenkingdoms.local | Domain Controller (child) |
| DC03 | meereen | essos.local | Domain Controller |
| SRV02 | castelblack | north.sevenkingdoms.local | Member Server (IIS, MSSQL, WebDAV) |
| SRV03 | braavos | essos.local | Member Server (MSSQL, WebDAV, ADCS) |

### Services per Host

| Host | Services |
| ------ | ---------- |
| DC01 (kingslanding) | ADCS, Defender ON |
| DC02 (winterfell) | LLMNR, NBT-NS, SMB shares, Defender ON |
| DC03 (meereen) | ADCS custom templates, LAPS DC, NTLM downgrade, Defender ON |
| SRV02 (castelblack) | IIS, MSSQL (+ SSMS), WebDAV, SMB shares, Defender OFF |
| SRV03 (braavos) | MSSQL, WebDAV, LAPS, SMB shares, RunAsPPL, Defender ON |

---

## Domain 1: sevenkingdoms.local

**Forest:** sevenkingdoms.local
**NetBIOS:** SEVENKINGDOMS
**DC:** kingslanding (DC01)
**Domain Admin Password:** Set during provisioning

### Users (sevenkingdoms)

| Username | Password | Groups | Description |
| ---------- | ---------- | -------- | ------------- |
| robert.baratheon | `iamthekingoftheworld` | Baratheon, Domain Admins, Small Council, Protected Users | Local admin on DC01 |
| cersei.lannister | `il0vejaime` | Lannister, Baratheon, Domain Admins, Small Council | Local admin on DC01 |
| tywin.lannister | `powerkingftw135` | Lannister | - |
| jaime.lannister | `cersei` | Lannister | - |
| tyron.lannister | `Alc00L&S3x` | Lannister | - |
| joffrey.baratheon | `1killerlion` | Baratheon, Lannister | - |
| renly.baratheon | `lorastyrell` | Baratheon, Small Council | Account is sensitive (cannot be delegated) |
| stannis.baratheon | `Drag0nst0ne` | Baratheon, Small Council | - |
| petyer.baelish | `@littlefinger@` | Small Council | - |
| lord.varys | `_W1sper_$` | Small Council | GenericAll on Domain Admins |
| maester.pycelle | `MaesterOfMaesters` | Small Council | - |

### Groups (sevenkingdoms)

| Group | Type | Managed By |
| ------- | ------ | ------------ |
| Lannister | Global | tywin.lannister |
| Baratheon | Global | robert.baratheon |
| Small Council | Global | - |
| DragonStone | Global | - |
| KingsGuard | Global | - |
| DragonRider | Global | - |
| AcrossTheNarrowSea | Domain Local | - |

### ACL Attack Paths (sevenkingdoms)

```text
tywin.lannister ──ForceChangePassword──> jaime.lannister
jaime.lannister ──GenericWrite──> joffrey.baratheon
joffrey.baratheon ──WriteDacl──> tyron.lannister
tyron.lannister ──Self-Membership──> Small Council
Small Council ──WriteMembership──> DragonStone
DragonStone ──WriteOwner──> KingsGuard
KingsGuard ──GenericAll──> stannis.baratheon
stannis.baratheon ──GenericAll──> kingslanding$ (DC01)
lord.varys ──GenericAll──> Domain Admins
AcrossTheNarrowSea ──GenericAll──> kingslanding$ (DC01)
renly.baratheon ──WriteDACL──> OU=Crownlands
```

---

## Domain 2: north.sevenkingdoms.local (child domain)

**Forest:** sevenkingdoms.local
**NetBIOS:** NORTH
**DC:** winterfell (DC02)
**Parent Domain:** sevenkingdoms.local
**Domain Admin Password:** Set during provisioning

### Users (north)

| Username | Password | Groups | Description |
| ---------- | ---------- | -------- | ------------- |
| eddard.stark | `FightP3aceAndHonor!` | Stark, Domain Admins | Local admin on DC02 |
| catelyn.stark | `robbsansabradonaryarickon` | Stark | Local admin on DC02 |
| robb.stark | `sexywolfy` | Stark | Local admin on DC02, autologon creds on DC02 |
| arya.stark | `Needle` | Stark | MSSQL impersonate dbo on castelblack |
| sansa.stark | `345ertdfg` | Stark | SPN: HTTP/eyrie.north.sevenkingdoms.local |
| brandon.stark | `iseedeadpeople` | Stark | MSSQL impersonate jon.snow on castelblack |
| rickon.stark | `Winter2022` | Stark | - |
| hodor | `hodor` | Stark | Brainless Giant |
| jon.snow | `iknownothing` | Stark, Night Watch | MSSQL sysadmin on castelblack, SPN: HTTP/thewall |
| samwell.tarly | `Heartsbane` | Night Watch | Password in description, MSSQL impersonate sa |
| jeor.mormont | `_L0ngCl@w_` | Night Watch, Mormont | Local admin on SRV02 (castelblack) |
| sql_svc | `YouWillNotKerboroast1ngMeeeeee` | - | SPNs: MSSQLSvc/castelblack:1433 |

### Groups (north)

| Group | Type | Managed By |
| ------- | ------ | ------------ |
| Stark | Global | eddard.stark |
| Night Watch | Global | jeor.mormont |
| Mormont | Global | jeor.mormont |
| AcrossTheSea | Domain Local | - |

### ACL Attack Paths (north)

```text
NT AUTHORITY\ANONYMOUS LOGON ──ReadProperty + GenericExecute──> DC=North (anonymous enumeration)
```

---

## Domain 3: essos.local

**Forest:** essos.local
**NetBIOS:** ESSOS
**DC:** meereen (DC03)
**Trust:** Bidirectional with sevenkingdoms.local
**Domain Admin Password:** Set during provisioning

### Users (essos)

| Username | Password | Groups | Description |
| ---------- | ---------- | -------- | ------------- |
| daenerys.targaryen | `BurnThemAll!` | Targaryen, Domain Admins | Local admin on DC03 |
| viserys.targaryen | `GoldCrown` | Targaryen | - |
| khal.drogo | `horse` | Dothraki | Local admin on SRV03, MSSQL sysadmin on braavos |
| jorah.mormont | `H0nnor!` | Targaryen | LAPS reader, MSSQL impersonate sa on braavos |
| missandei | `fr3edom` | - | GenericAll on khal.drogo |
| drogon | `Dracarys` | Dragons | - |
| sql_svc | `YouWillNotKerboroast1ngMeeeeee` | - | SPNs: MSSQLSvc/braavos:1433 |

### Groups (essos)

| Group | Type | Managed By |
| ------- | ------ | ------------ |
| Targaryen | Global | viserys.targaryen |
| Dothraki | Global | khal.drogo |
| Dragons | Global | - |
| QueenProtector | Global | - (members: Dragons -> Domain Admins) |
| DragonsFriends | Domain Local | daenerys.targaryen |
| Spys | Domain Local | - (LAPS reader) |

### Cross-Domain Memberships

| Group | External Members |
| ------- | ----------------- |
| DragonsFriends | sevenkingdoms.local\tyron.lannister, essos.local\daenerys.targaryen |
| Spys | sevenkingdoms.local\Small Council |
| AcrossTheNarrowSea (sevenkingdoms) | essos.local\daenerys.targaryen |

### ACL Attack Paths (essos)

```text
khal.drogo ──GenericAll──> viserys.targaryen
Spys ──GenericAll──> jorah.mormont
khal.drogo ──GenericAll──> ESC4 certificate template
viserys.targaryen ──WriteProperty──> jorah.mormont
DragonsFriends ──GenericWrite──> braavos$ (SRV03)
missandei ──GenericAll──> khal.drogo
gmsaDragon$ ──GenericAll──> drogon
```

### gMSA

| Name | FQDN | SPNs |
| ------ | ------ | ------ |
| gmsaDragon | gmsaDragon.essos.local | HTTP/braavos, HTTP/braavos.essos.local |

---

## MSSQL Linked Servers

```text
castelblack.north.sevenkingdoms.local (SRV02)
    └──linked──> braavos.essos.local (SRV03)
                 Login: jon.snow -> sa (password: sa_P@ssw0rd!Ess0s)

braavos.essos.local (SRV03)
    └──linked──> castelblack.north.sevenkingdoms.local (SRV02)
                 Login: khal.drogo -> sa (password: Sup1_sa_P@ssw0rd!)
```

## MSSQL Service Accounts

| Host | SA Password | Service Account | Sysadmins |
| ------ | ------------- | ----------------- | ----------- |
| SRV02 (castelblack) | `Sup1_sa_P@ssw0rd!` | sql_svc | NORTH\jon.snow |
| SRV03 (braavos) | `sa_P@ssw0rd!Ess0s` | sql_svc | ESSOS\khal.drogo |

## MSSQL Impersonation

| Host | User | Can Impersonate |
| ------ | ------ | ----------------- |
| SRV02 | NORTH\samwell.tarly | sa |
| SRV02 | NORTH\brandon.stark | NORTH\jon.snow |
| SRV02 | NORTH\arya.stark | dbo (master), dbo (msdb) |
| SRV03 | ESSOS\jorah.mormont | sa |

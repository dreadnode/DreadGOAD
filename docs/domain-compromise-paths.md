# Domain Compromise Paths

This document enumerates the **provisioned ways to fully compromise DreadGOAD** — paths that culminate in Domain Admin (or domain-equivalent: DCSync, krbtgt/golden ticket, Enterprise Admin, or full control of a DC) in any domain in the lab.

Everything here is derived strictly from the lab's provisioning, **not** generic GOAD knowledge:

- `ad/GOAD/data/config.json` — accounts, ACLs, group memberships, MSSQL links, ADCS template/CA flags, trusts (line cites below reference this file unless noted)
- `ad/GOAD/data/inventory` — ADCS host group
- `ad/GOAD/scripts/*.ps1` — poisoning/relay/delegation attack-enabling scheduled tasks
- `ansible/roles/vulns_*` and `ansible/roles/adcs_templates/tasks/main.yml` — vulnerable-config roles and template publishing
- `ansible/playbooks/adcs.yml` — CA web-enrollment defaults

> **Variant deployments:** if deployed with `variant: true`, every credential, SPN, ACL edge, and template ACL is randomized. Re-read the variant's own `data/config.json` for the live names, and `mapping.json` for the old-to-new translation. Specific names are deliberately not quoted here, since each regeneration produces a fresh identity. The path **graph is isomorphic** and the counts below hold, but all names and passwords differ.

## At a glance

The lab's vulnerabilities form a web rather than a single chain, so a red agent can
reach domain compromise by many different routes. Three of them, highlighted:

![One goal, a different path every run: initial access fanning out through LLMNR
poisoning, Kerberoasting, AS-REP roasting, password spray, PetitPotam, NTLM relay,
hash cracking, shadow credentials, DCSync, ADCS ESC1, RBCD, and golden ticket, all
converging on domain compromise](moving-target-attack-paths.png)

The same three routes as concrete kill chains against the stock GOAD names, each
step tagged with its MITRE technique ID, converging on Domain Admin, then a golden
ticket, then forest compromise:

![Paths to Domain Admin across three DreadGOAD runs: run 1 LLMNR poison to hash
crack to PsExec to DCSync; run 2 LDAP enum to AS-REP roast to shadow credentials to
ESC7 forge; run 3 service scan to PetitPotam to NTLM relay to DCSync](paths-to-domain-admin.png)

## Lab topology recap

- Two forests, three domains: `sevenkingdoms.local` (forest root, DC01 kingslanding) with child `north.sevenkingdoms.local` (DC02 winterfell), and `essos.local` (separate forest, DC03 meereen)
- Member servers: SRV02 castelblack (north), SRV03 braavos (essos)
- Bidirectional forest trust between `sevenkingdoms.local` and `essos.local`
- ADCS CAs on DC01 (SEVENKINGDOMS-CA) and SRV03 (ESSOS-CA); vulnerable templates published on DC03

**Key fact — no dead-end domain.** Any single domain compromise cascades to the entire lab:

- **north → sevenkingdoms** always works via child→parent (forge a krbtgt golden ticket with the `-519` Enterprise Admins ExtraSID; `raiseChild.py`).
- The **two forests own each other**: essos→7K (daenerys ∈ `AcrossTheNarrowSea` + SID-history), and 7K→essos (`tyron` / Small Council nested into essos `DragonsFriends` / `Spys` → braavos → gmsaDragon$ → drogon).

So owning **any one DC = owning the whole lab.** The counts below measure the number of independent *starting primitives* that reach your first DA.

---

## Two counting views

The count depends entirely on granularity:

| View | Total | Counting rule |
|------|-------|---------------|
| **Distinct paths** | **29** | One distinct provisioned primitive (or minimal chain) ending in domain compromise = one path. Foothold/capture *variants* are listed underneath, not multiplied. |
| **Full permutations (maximalist)** | **~133** | Every capture vector and every named foothold counted as a separate way. Sensitive to sub-rules (see below). |

The maximalist number is **dominated by ADCS**: 49 of the ~133 come from 7 "any Domain User" essos templates × 7 named essos accounts. Sub-rule sensitivity:

- **133** — full maximalist
- **91** — collapse the 7 any-user ADCS templates to one row each (133 − 49 + 7)
- **128** — don't double-count the 5 cross-referenced footholds reused in the gMSA→drogon routes

---

## View 1: The 29 distinct paths

### North (north.sevenkingdoms.local / winterfell DC02) — 6 paths

| # | Label | Technique class | Starting foothold | End state | Provisioned by |
|---|---|---|---|---|---|
| N1 | robb.stark → local admin on DC02 | NTLM poisoning / cleartext recovery → DCSync | network position | DCSync north / krbtgt | robb ∈ DC02 Administrators (`:35-39`); LLMNR+NBT-NS (`:53`); 3 capture vectors |
| N2 | eddard.stark relay/coerce | NTLM relay + coercion | unsigned SMB target + listener | north DA | `ntlm_relay.ps1` — bot runs **as eddard.stark (DA) every 5m** to `\\Meren\Private` |
| N3 | samwell.tarly GPO abuse | GPO abuse | samwell.tarly (`Heartsbane`, in description `:516`) | SYSTEM on DC02 → north DA | `gpo_abuse.ps1` grants `GpoEditDeleteModifySecurity` on StarkWallpaper GPO linked to `DC=north` |
| N4 | jon.snow constrained delegation (protocol transition) | Constrained delegation (S4U2self+proxy) | jon.snow (`iknownothing`; Kerberoastable `:509`) | impersonate any user to `CIFS/winterfell` → north DA | `constrained_delegation_use_any.ps1` |
| N5 | sansa.stark unconstrained delegation | Unconstrained delegation + DC coercion | sansa.stark (`345ertdfg`; Kerberoastable `:472`) | capture winterfell$ TGT → DCSync → north DA | `unconstrained_delegation_user.ps1` |
| N6 | castelblack$ constrained delegation (kerberos-only) | Constrained delegation (no protocol transition) | SYSTEM on castelblack | delegate to `HTTP/winterfell` → north DA | `constrained_delegation_kerb_only.ps1` |

**N1 capture vectors** (all yield `robb.stark:sexywolfy`, a DC local admin):

- **Responder/LLMNR** — `responder.ps1`, bot as robb every **2m** to `\\Bravos\private`
- **RDP cleartext** — `rdp_scheduler.ps1` (`connect_bot`, every **1m**) + stored `TERMSRV/castelblack` cred (`:58-65`)
- **Autologon** — registry autologon creds for robb on DC02 (`:66-71`)

`eddard.stark` and `catelyn.stark` are also DC02 local admins (`:36-37`) — direct DCSync if their creds are recovered.

**N6 foothold note:** castelblack SYSTEM is reachable via MSSQL — `brandon.stark` (AS-REP, `asrep_roasting.ps1`) → impersonate `jon.snow` → jon.snow is MSSQL sysadmin on castelblack (`:143-148`) → `xp_cmdshell` → SeImpersonate → SYSTEM.

### Sevenkingdoms (sevenkingdoms.local / kingslanding DC01, forest root) — 7 paths

| # | Label | Technique class | Starting foothold | End state | Config cite |
|---|---|---|---|---|---|
| S1 | The Tywin ACL chain | ACL abuse chain → RBCD on DC | tywin.lannister (`powerkingftw135`) | DA/EA | `:593-600` |
| S2 | lord.varys → Domain Admins | ACL abuse (single edge) | lord.varys (`_W1sper_$`) | DA | `:602` GenericAll on Domain Admins (+ `:603` AdminSDHolder) |
| S3 | stannis → kingslanding$ | ACL → RBCD/shadow-cred on DC | stannis.baratheon (`Drag0nst0ne`) | DA | `:600` GenericAll on kingslanding$ |
| S4 | renly → Crownlands OU | ACL abuse (OU DACL) | renly.baratheon (`lorastyrell`) | DA (controls robert/cersei) | `:604` WriteDacl on `OU=Crownlands` |
| S5 | AcrossTheNarrowSea → kingslanding$ | Cross-forest group + ACL/RBCD | essos\daenerys (essos DA) | DA | `:601` GenericAll on kingslanding$; member `:588-590` |
| S6 | ESC8 on SEVENKINGDOMS-CA | ADCS ESC8 (web-enroll relay) | any domain creds + DC coercion | DC cert → DCSync → DA | CA on dc01 (`inventory:88-90`); web enroll default true (`adcs.yml`) |
| S7 | ESC10 cert-mapping | ADCS ESC10 (weak cert binding) | a write-UPN primitive (e.g. from S1/S2) | impersonate DA via Schannel/PKINIT | dc01 `adcs_esc10_case1`+`case2` (`:22`) |

### Essos (essos.local / meereen DC03) — 12 paths

Essos DA via the gMSA→drogon nested-group route and via ADCS. Nesting that makes drogon effectively DA: `Dragons → QueenProtector → Domain Admins` (`:273-286`), drogon ∈ Dragons (`:371-378`).

| # | Label | Technique class | Starting foothold | End state | Config cite |
|---|---|---|---|---|---|
| E1 | braavos → gmsaDragon$ → drogon | gMSA read + nested group | braavos local admin | essos DA | gMSA retrievable by braavos host (`:307-313`); `:322` gmsaDragon$ GenericAll on drogon |
| E2 | ESC1 | ADCS ESC1 (enrollee-supplies-subject) | any essos Domain User | cert as daenerys → DA | `adcs_templates`; `:189` |
| E3 | ESC2 | ADCS ESC2 (Any-Purpose EKU) | any essos Domain User | cert as DA | `:189` |
| E4 | ESC3 / ESC3-CRA | ADCS ESC3 (enrollment agent) | any essos Domain User | enroll-on-behalf-of DA | `:189` |
| E5 | ESC4 | ADCS ESC4 (template DACL) → ESC1 | khal.drogo (`horse`) | reconfigure template → cert as DA | `:318` GenericAll on ESC4 template |
| E6 | ESC6 | ADCS ESC6 (CA SAN flag) | any essos Domain User | SAN-spoof DA | `vulns_adcs_esc6` sets `EDITF_ATTRIBUTESUBJECTALTNAME2` (`:221`); needs meereen `StrongCertificateBindingEnforcement=0` (`:188`) |
| E7 | ESC7 | ADCS ESC7 (ManageCA) | viserys.targaryen (`GoldCrown`) | approve/enable → cert as DA | `:191-194` |
| E8 | ESC9 | ADCS ESC9 (no-security-extension + UPN write) | missandei/khal | UPN-swap → cert as DA | `:189`; write via `:321`/`:323`/`:316`; needs meereen `StrongCertificateBindingEnforcement` ≤ 1 (`:188`) |
| E9 | ESC11 | ADCS ESC11 (ICertPassage RPC relay) | coercion + relay | DC cert → DCSync | `vulns_adcs_esc11` clears `IF_ENFORCEENCRYPTICERTREQUEST` (`:221`) |
| E10 | ESC13 | ADCS ESC13 (issuance-policy → group) | any essos Domain User | cert grants `greatmaster` = local admin DC03 → DA | `:196-201`; greatmaster ∈ DC03 Administrators (`:181`) |
| E11 | ESC15 (EKUwu) | ADCS ESC15 (v1 enrollee-supplies-subject) | any essos Domain User | add client-auth app-policy → cert as DA | `vulns_adcs_esc15` grants Domain Users Enroll on "Web Server" |
| E12 | ESC8 on ESSOS-CA | ADCS ESC8 (web-enroll relay) | any domain creds + coerce meereen | DC cert → DA | CA on srv03 (`inventory:88-90`); web enroll default true |

**E6 and E8 depend on the essos KDC, not on the CA.** Both authenticate with a
weakly mapped certificate, so meereen's `StrongCertificateBindingEnforcement`
decides them: 0 for E6 (its certificate carries the requester's SID, which
Compatibility mode still validates), 0 or 1 for E8 (its certificate carries no
SID at all). The value is unset by default and, since KB5014754 in February
2025, an unset value means Full Enforcement, which closes both. GOAD therefore
pins it on meereen via `adcs_esc10_case1` (`:188`). The pin on kingslanding
(`:22`) does not help here: it is a different forest, with no CA and no
vulnerable templates to enrol against.

**E1 routes to braavos local admin** (→ SYSTEM → read gmsaDragon$ → control drogon): khal.drogo direct local admin (`:213`) and MSSQL sysadmin (`:226-227`); jorah.mormont LAPS reader (`:254-257`); `DragonsFriends → GenericWrite braavos$`→ RBCD (`:320`).

### Cross-domain / trust hops — 4 additional paths

(S5, essos→sevenkingdoms, is counted under sevenkingdoms.)

| # | Label | Technique class | From → To | Mechanism |
|---|---|---|---|---|
| C1 | north child → sevenkingdoms parent | Intra-forest child→parent (golden ticket + ExtraSID EA) | north DA → 7K DA/EA | parent/child trust; forge krbtgt with `...-519`; `raiseChild.py` |
| C2 | essos → sevenkingdoms via SID history | Cross-forest SID-history injection | essos DA → 7K EA | `sidhistory.ps1`: `netdom ... /enablesidhistory:yes` — SID filtering disabled on forest trust |
| C3 | tyron.lannister → essos | Cross-forest group + RBCD + gMSA | 7K tyron → essos DA | tyron ∈ essos `DragonsFriends` (`:298-302`) → GenericWrite braavos$ (`:320`) → SYSTEM → gmsaDragon$ → drogon |
| C4 | Small Council → essos via Spys/LAPS | Cross-forest group + LAPS read | 7K Small Council → essos DA | Small Council ∈ essos `Spys` (`:303-305`); Spys = LAPS reader (`:254-257`) → braavos admin → drogon |

### 29-path totals

**By domain (independent native ways):** north 6 · sevenkingdoms 7 · essos 12 · cross-domain hops 4 = **29**

**By technique category:**

| Category | Count | Paths |
|---|---|---|
| ADCS (ESC1/2/3/4/6/7/8/9/10/11/13/15) | 13 | S6, S7, E2–E12 |
| ACL abuse (chains + single edges) | 5 | S1, S2, S3, S4, E5 |
| Delegation (constrained ×2, unconstrained) | 3 | N4, N5, N6 |
| NTLM relay / coercion | 1 | N2 |
| NTLM poisoning / cleartext → local-admin-on-DC | 1 | N1 (3 capture vectors) |
| GPO abuse | 1 | N3 |
| gMSA + nested group → DA | 1 | E1 |
| Trust abuse (child→parent, SID history, cross-forest group) | 4 | C1, C2, C3, C4 (+ S5) |

---

## View 2: The ~133 permutations (maximalist)

Counting rule: one row per genuinely distinct provisioned artifact **or** distinct named credential/foothold that can initiate the step. Interchangeable tools (rpcclient vs ldapsearch vs nxc) are **not** separate rows; separately-provisioned secrets (Responder bot vs autologon registry vs cmdkey blob) **are**.

### Grand total by group

| Group | Rows |
|---|---|
| A — Initial access / credential discovery | 12 |
| B — Poisoning & relay (N1 artifacts + relay targets) | 10 |
| C — North-DA convergence (DCSync node) | 1 |
| D — MSSQL impersonation / linked-server | 13 |
| E — ACL chains (each edge = an entry credential) | 20 |
| F — Delegation | 3 |
| G — ADCS (incl. 49 any-user permutations) | 59 |
| H — Trusts | 7 |
| I — LAPS | 2 |
| J — gMSA→drogon routes | 6 |
| **Grand total (bounded maximalist)** | **133** |

### Group detail

**A — Initial access / credential discovery (12):** samwell description disclosure anon (`:424-425` + `:516`) and authenticated (`:511-518`); hodor spray (`:492-499`); brandon.stark AS-REP north (`:474-482`, `asrep_roasting.ps1`); missandei AS-REP essos (`:362-369`, `asrep_roasting2.ps1`); jon.snow Kerberoast (`:501-509`); sansa Kerberoast (`:464-472`); sql_svc Kerberoast north (`:529-537`), essos (`:380-389`), cross-forest password reuse (`:532` vs `:383`); north anon enum ReadProperty (`:424`) and GenericExecute (`:425`).

**B — Poisoning & relay (10):** N1 yields a DC02 admin cred via 4 separately-provisioned robb artifacts — LLMNR/NBT-NS (`responder.ps1`, `:53`), cmdkey blob (`:58-65`), scheduled-task stored password (`rdp_scheduler.ps1`), registry autologon (`:66-71`) — plus eddard (`:36`) and catelyn (`:37`) as direct DC02 admins. eddard (north DA) relay (`ntlm_relay.ps1`, `:48`) to: winterfell SMB → DCSync, castelblack SMB, braavos SMB, and LDAP/LDAPS RBCD (`vulns_no_ldap_signing` / `_no_ldap_integrity` / `_no_ldap_channel_binding`).

**C — North-DA convergence (1):** DC02 local admin → secretsdump → eddard NT → DCSync north (`:34-39`). Initiator multiplicity already counted in B.

**D — MSSQL (13):** castelblack (`:140-170`) — samwell→sa, brandon→jon.snow, jon.snow direct sysadmin, arya→dbo (master), arya→dbo (msdb), sa direct, sql_svc identity. braavos (`:223-241`) — khal.drogo sysadmin, jorah→sa, sa direct, sql_svc identity. Linked-server cross-forest hops — castelblack(jon.snow)→braavos sa (`:162-169`), braavos(khal.drogo)→castelblack sa (`:233-240`).

**E — ACL chains (20):** sevenkingdoms chain `:592-604` — 8 edges (tywin→jaime→joffrey→tyron→Small Council→DragonStone→KingsGuard→stannis→kingslanding$), plus varys→Domain Admins (`:602`), varys→AdminSDHolder (`:603`), renly→Crownlands (`:604`), AcrossTheNarrowSea→kingslanding$ (`:601`). essos chain `:315-323` — 8 edges (missandei→khal, khal→viserys, Spys→jorah, viserys→jorah, missandei→viserys, khal→ESC4 template, DragonsFriends→braavos$, gmsaDragon$→drogon).

**F — Delegation (3):** sansa unconstrained (`unconstrained_delegation_user.ps1`); jon.snow constrained use-any-protocol (`constrained_delegation_use_any.ps1`); jon.snow constrained kerberos-only (`constrained_delegation_kerb_only.ps1`).

**G — ADCS (59):** **49** = 7 any-user templates (ESC1, ESC2, ESC3, ESC3-CRA, ESC6, ESC13, ESC15) × 7 named essos users (daenerys, viserys, khal.drogo, jorah.mormont, missandei, drogon, sql_svc) (`:188-189`, `:221`, `:325-389`); + ESC4 (khal only, `:318`); + ESC7 (viserys only, `:191-194`); + ESC9 (4 write-holders: khal, missandei, Spys, viserys; `:316-323`); + ESC8 braavos relay (1); + ESC11 braavos relay (1); + ESC10 dc01 case1+case2 (2, `:22`). ESC10 case 1 is also pinned on meereen (`:188`), which is what keeps E6 and E8 alive at all; its essos abuse paths reuse the same four UPN-write holders already counted under ESC9, so the total is unchanged.

**H — Trusts (7):** child→parent golden+ExtraSID 519, child→parent inter-realm TGT, raiseChild.py one-shot (`:392-540`, trust `:396`); tyron→DragonsFriends→essos (`:298-302`); daenerys→AcrossTheNarrowSea→kingslanding$ (`:587-591`, `:601`); Small Council→Spys→jorah (`:303-305`, `:317`); daenerys→DragonsFriends→braavos$ (`:299-302`, `:320`).

**I — LAPS (2):** braavos LAPS read by jorah.mormont (`:254-257`, `use_laps` `:211`) and by Spys (→ Small Council cross-forest, incl. tyron).

**J — gMSA→drogon (6):** gMSA password readable only from braavos host (`:312`); braavos SYSTEM unlocks gmsaDragon$ → GenericAll drogon (`:322`). Routes to braavos admin/SYSTEM: khal direct local admin (`:213-215`), khal MSSQL sysadmin (`:226-228`), jorah LAPS (`:254-257`), DragonsFriends RBCD (`:320`), jorah→sa (`:230`), braavos sa direct (`:224`). Five of these reuse footholds counted in D/E/I (subtract 5 → 128 if not double-counting).

---

## Bounded vs. open-ended

**Genuinely open-ended (no real ceiling):**

- **ADCS any-user templates (G's 49):** enroll right is granted to essos **Domain Users**, so the true multiplier is *every current and future essos account, gMSA, and service account* — add a user, add 7 paths. 49 is the named-foothold floor. Cross-forest principals are excluded by default (stock templates grant essos Domain Users, not Authenticated Users), so the forest trust does not inflate this set.
- **hodor spray / anonymous enum (A):** one named hit, but the attack surface is the whole weak-policy directory (complexity off, min length 5; `password_policy` role).
- **ESC8 / ESC11 (braavos web/RPC relay):** initiator = any principal able to coerce a DC; counted as 1 each but driveable from any authenticated foothold.

**Bounded / closed sets:** N1's 6 DC02-admin vectors (`:34-39`); MSSQL impersonation blocks (`:143-160`, `:226-232`); every ACL edge (explicit ACEs `:592-604` / `:315-323`); delegation; ESC4/ESC7; trusts; LAPS — all closed named-principal sets.

---

## Verification pointers

- The eddard relay task and the robb Responder task both live on **DC02 (winterfell)**, not kingslanding (`ntlm_relay.ps1`, `responder.ps1`).
- ESC8 viability depends on `ca_web_enrollment` defaulting true (`ansible/playbooks/adcs.yml`); no `ca_web_enrollment: false` is set for either CA, so both DC01 and braavos expose web enrollment.
- If a path doesn't work live, suspect provisioning: check the corresponding role (`ansible/roles/vulns_acls`, `vulns_adcs_esc*`, `adcs_templates`, `gmsa`, `trusts`) and run `dreadgoad validate`.

## Source files

- `ad/GOAD/data/config.json`
- `ad/GOAD/data/inventory`
- `ad/GOAD/scripts/{responder,ntlm_relay,rdp_scheduler,gpo_abuse,constrained_delegation_use_any,constrained_delegation_kerb_only,unconstrained_delegation_user,asrep_roasting,asrep_roasting2,sidhistory}.ps1`
- `ansible/roles/adcs_templates/tasks/main.yml`
- `ansible/roles/vulns_adcs_esc{6,7,11,13,15,10_case1,10_case2}/tasks/main.yml`
- `ansible/roles/{vulns_autologon,vulns_credentials,vulns_anonymous_enum,vulns_no_ldap_signing}/`
- `ansible/playbooks/adcs.yml`
- Variant: `ad/GOAD-variant-1/data/config.json`

See also: [Vulnerability catalog](GOAD-vulnerabilities-comprehensive.md) · [Domains and users](domains-and-users.md)

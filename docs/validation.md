# GOAD Vulnerability Validation

This document describes how to validate that your GOAD deployment has all the documented vulnerabilities properly configured.

## Overview

The GOAD validation system checks that all 50+ vulnerabilities documented in [`GOAD-vulnerabilities-comprehensive.md`](./GOAD-vulnerabilities-comprehensive.md) are properly configured in your AWS deployment. This ensures the lab is ready for penetration testing training.

## Quick Start

### Run Full Validation

```bash
# Validate staging environment (default)
dreadgoad validate

# Validate a specific environment
dreadgoad validate --env dev

# Enable verbose output
dreadgoad validate --verbose

# Initial validation without failing on errors
dreadgoad validate --env staging --no-fail
```

### Run Quick Validation

For a faster sanity check of critical vulnerabilities:

```bash
dreadgoad validate --quick
dreadgoad validate --quick --env dev
```

## What Gets Validated

The validation script checks the following categories of vulnerabilities:

### 1. **Credential Discovery** (10 checks)

- ✓ Passwords in user description fields (samwell.tarly)
- ✓ Username=password combinations (hodor)
- ✓ Weak password policies
- ✓ Password spray vulnerabilities

### 2. **Kerberos Attack Vectors** (12 checks)

- ✓ AS-REP Roasting accounts (brandon.stark, missandei)
- ✓ Kerberoasting targets (jon.snow, sql_svc)
- ✓ Service Principal Names configured
- ✓ Kerberos user enumeration possible

### 3. **Network Misconfigurations** (8 checks)

- ✓ SMB signing disabled on CASTELBLACK and BRAAVOS
- ✓ LLMNR/NBT-NS enabled
- ✓ NTLM relay opportunities
- ✓ Anonymous SMB session access

### 4. **Delegation Attacks** (6 checks)

- ✓ Unconstrained delegation (sansa.stark)
- ✓ Constrained delegation (jon.snow)
- ✓ Resource-Based Constrained Delegation setup
- ✓ Machine Account Quota = 10

### 5. **MSSQL Configurations** (8 checks)

- ✓ MSSQL services running on CASTELBLACK and BRAAVOS
- ✓ Impersonation permissions (samwell.tarly → sa, arya.stark → dbo)
- ✓ MSSQL admin accounts (jon.snow, khal.drogo)
- ✓ Trusted links between servers

### 6. **ADCS Vulnerabilities** (15+ checks)

- ✓ ADCS installed on BRAAVOS
- ✓ ADCS Web Enrollment configured (ESC8)
- ✓ Vulnerable certificate templates (ESC1, ESC2, ESC3, ESC4, ESC6, etc.)
- ✓ Certificate mapping misconfigurations

### 7. **ACL Abuse** (20+ checks)

- ✓ ForceChangePassword permissions
- ✓ GenericWrite on users/computers
- ✓ WriteDacl permissions
- ✓ WriteOwner on groups
- ✓ GPO abuse permissions
- ✓ Complete ACL attack chains

### 8. **Domain Trusts** (4 checks)

- ✓ Parent-child trust (sevenkingdoms ↔ north)
- ✓ Forest trust (sevenkingdoms ↔ essos)
- ✓ Cross-forest group memberships
- ✓ SID history enabled

### 9. **Services & Miscellaneous** (10 checks)

- ✓ IIS running on CASTELBLACK
- ✓ Print Spooler service status
- ✓ LDAP signing not enforced
- ✓ WebClient service configuration

## Output Format

### Console Output

The script provides color-coded console output:

```text
==========================================
GOAD Vulnerability Validation
==========================================
Environment: dev
Inventory: ./dev-inventory
Output: /tmp/goad-validation-20241215-134500.json

ℹ Discovering instances...
✓ Found DC01: i-0123456789abcdef0
✓ Found DC02: i-0123456789abcdef1
✓ Found DC03: i-0123456789abcdef2
✓ Found SRV02: i-0123456789abcdef3
✓ Found SRV03: i-0123456789abcdef4

==========================================
1. Credential Discovery Vulnerabilities
==========================================
ℹ Checking for passwords in user descriptions...
✓ samwell.tarly has password in description

==========================================
2. Kerberos Attack Vectors
==========================================
ℹ Checking AS-REP Roasting accounts...
✓ brandon.stark has DoesNotRequirePreAuth enabled
✗ missandei does NOT have PreAuth disabled
⚠ jon.snow SPNs configured but not optimal

...

==========================================
Validation Summary
==========================================
Total Checks:    87
Passed:          73
Failed:          8
Warnings:        6

Success Rate: 84%

Results saved to: /tmp/goad-validation-20241215-134500.json
==========================================
```

### JSON Output

Results are also saved to a JSON file for programmatic analysis:

```json
{
  "validation_date": "2024-12-15T13:45:00Z",
  "environment": "dev",
  "summary": {
    "total_checks": 87,
    "passed": 73,
    "failed": 8,
    "warnings": 6
  },
  "checks": [
    {
      "category": "credential_discovery",
      "name": "password_in_description",
      "status": "pass",
      "details": "samwell.tarly has password 'Heartsbane' in description",
      "user": "samwell.tarly",
      "domain": "north.sevenkingdoms.local"
    },
    ...
  ]
}
```

## Exit Codes

- **0**: All checks passed (or only warnings)
- **1**: One or more checks failed

## Validation Checklist

Use this checklist to track validation progress:

### Critical Vulnerabilities (Must Pass)

- [ ] All 5 servers running and accessible
- [ ] All 3 domains configured correctly
- [ ] All expected users present (46+ users)
- [ ] SMB signing disabled on SRV02 and SRV03
- [ ] MSSQL running on both servers
- [ ] ADCS installed on BRAAVOS
- [ ] Domain trusts configured

### High Priority Vulnerabilities

- [ ] AS-REP Roasting: brandon.stark, missandei
- [ ] Kerberoasting: jon.snow, sql_svc
- [ ] Password in description: samwell.tarly
- [ ] Unconstrained delegation: sansa.stark
- [ ] Constrained delegation: jon.snow
- [ ] Machine Account Quota = 10

### Medium Priority Vulnerabilities

- [ ] MSSQL impersonation permissions
- [ ] MSSQL trusted links
- [ ] ADCS vulnerable templates (ESC1-15)
- [ ] ACL permission chains
- [ ] Print Spooler enabled
- [ ] IIS file upload vulnerability

### Lower Priority (Nice to Have)

- [ ] LLMNR/NBT-NS enabled
- [ ] LAPS configuration
- [ ] GPO abuse permissions
- [ ] Cross-forest group memberships
- [ ] Bot accounts configured

## Troubleshooting

### Common Issues

#### 1. "Could not find all required domain controllers"

**Cause**: Instances not running or SSM not accessible

**Solution**:

```bash
# Check instance status
dreadgoad lab status

# Verify SSM agent is running
aws ssm describe-instance-information --filters "Key=tag:Name,Values=*goad*"
```

#### 2. "Permission denied" errors

**Cause**: Script not executable or AWS credentials not configured

**Solution**:

```bash
# Make script executable
chmod +x scripts/validate-goad-vulns.sh

# Check AWS credentials
aws sts get-caller-identity
```

#### 3. Script hangs at "Discovering instances..." or times out

**Cause**: AWS CLI calls can be slow, especially when querying multiple instances

**Solution**:

```bash
# Option 1: Run with --no-fail to see progress
dreadgoad validate --env staging --no-fail --verbose

# Option 2: Test AWS CLI connectivity first
time aws ec2 describe-instances --region <your-region> --max-results 5
```

**Note**: The script may take 1-2 minutes to complete due to multiple AWS API calls. This is normal.

#### 4. "Timeout" errors during SSM commands

**Cause**: SSM commands taking too long

**Solution**:

- Increase sleep time in script (currently 5 seconds)
- Check network connectivity to instances
- Verify Windows Remote Management service running

#### 5. Many checks showing "WARN" or "FAIL"

**Cause**: Vulnerabilities not fully provisioned

**Solution**:

```bash
# Re-run vulnerability provisioning
dreadgoad provision --plays vulnerabilities.yml

# Or provision specific vulnerability roles
dreadgoad provision --plays vulnerabilities.yml --limit dc02
```

## Advanced Usage

### Validate Specific Categories

Modify the script to run only specific validation sections:

```bash
# Edit the script and comment out sections you don't need
vim scripts/validate-goad-vulns.sh
```

### Custom Output Location

```bash
dreadgoad validate --output /path/to/custom-report.json
```

### Integrate with CI/CD

Use the validation script in your CI/CD pipeline:

```yaml
# Example GitHub Actions workflow
- name: Validate GOAD Deployment
  run: |
    dreadgoad validate --env staging
  continue-on-error: false
```

## Manual Validation

If automated validation fails, you can manually verify vulnerabilities:

### 1. SSM into a Domain Controller

```bash
aws ssm start-session --target <instance-id> --region <your-region>
```

### 2. Run PowerShell Checks

```powershell
# Check AS-REP Roasting
Get-ADUser -Filter * -Properties DoesNotRequirePreAuth |
  Where-Object {$_.DoesNotRequirePreAuth -eq $true}

# Check Kerberoasting
Get-ADUser -Filter * -Properties ServicePrincipalName |
  Where-Object {$_.ServicePrincipalName}

# Check SMB Signing
Get-SmbServerConfiguration

# Check delegation
Get-ADUser -Filter * -Properties TrustedForDelegation,TrustedToAuthForDelegation

# Check Machine Account Quota
$domain = Get-ADDomain
$dn = "CN=Directory Service,CN=Windows NT,CN=Services,CN=Configuration,$($domain.DistinguishedName)"
Get-ADObject $dn -Properties ms-DS-MachineAccountQuota |
  Select-Object -ExpandProperty ms-DS-MachineAccountQuota
```

## Related Documentation

- [`GOAD-vulnerabilities-comprehensive.md`](./GOAD-vulnerabilities-comprehensive.md) - Complete vulnerability catalog
- [`cli.md`](./cli.md) - CLI usage and configuration reference
- [GOAD Official Docs](https://github.com/Orange-Cyberdefense/GOAD) - Upstream documentation
- [Mayfly's Walkthrough Series](https://mayfly277.github.io/categories/goad/) - Attack technique guides

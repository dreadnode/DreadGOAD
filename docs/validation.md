# GOAD Vulnerability Validation

This document describes how to validate that your GOAD deployment has all the documented vulnerabilities properly configured.

## Overview

The GOAD validation system checks that all 50+ vulnerabilities documented in [`GOAD-vulnerabilities-comprehensive.md`](./GOAD-vulnerabilities-comprehensive.md) are properly configured in your AWS deployment. This ensures the lab is ready for penetration testing training.

## Quick Start

### Run Full Validation

```bash
cd /Users/l/dreadnode/DreadOps/apps/DreadGOAD

# Validate dev environment (default)
task validate-vulns

# Validate staging environment
task validate-vulns ENV=staging

# Enable verbose output
task validate-vulns VERBOSE=true

# Initial validation without failing on errors
task validate-vulns ENV=staging FAIL_ON_ERROR=false

# Run script directly (useful for debugging)
ENV=staging REGION=us-west-1 INVENTORY_FILE=./staging-inventory \
  VERBOSE=true FAIL_ON_ERROR=false \
  ./scripts/validate-goad-vulns.sh
```

### Run Quick Validation

For a faster sanity check of critical vulnerabilities:

```bash
task validate-vulns-quick
task validate-vulns-quick ENV=staging
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

```
==========================================
GOAD Vulnerability Validation
==========================================
Environment: dev
Inventory: ./dev-inventory
Output: /tmp/goad-validation-20241215-134500.json

ℹ Discovering instances...
✓ Found DC01: i-028f18fd2e04f3ecc
✓ Found DC02: i-01fa0b5af9fef7c4c
✓ Found DC03: i-0045ac57f8e3d3a65
✓ Found SRV02: i-05e32c1deb99b7aa7
✓ Found SRV03: i-0dc7ce34249756c31

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
task -y aws:list-running-instances

# Verify SSM agent is running
aws ssm describe-instance-information --filters "Key=tag:Name,Values=*dreadgoad*"

# Test instance discovery manually
aws ec2 describe-instances \
  --filters "Name=tag:Name,Values=*dreadgoad*" "Name=instance-state-name,Values=running" \
  --region us-west-1 \
  --query 'Reservations[*].Instances[*].[InstanceId,Tags[?Key==`Name`].Value|[0]]' \
  --output table
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
# Option 1: Run with FAIL_ON_ERROR=false to see progress
task validate-vulns ENV=staging FAIL_ON_ERROR=false

# Option 2: Run script directly with all parameters
ENV=staging REGION=us-west-1 INVENTORY_FILE=./staging-inventory \
  VERBOSE=true FAIL_ON_ERROR=false \
  ./scripts/validate-goad-vulns.sh

# Option 3: Test AWS CLI connectivity first
time aws ec2 describe-instances --region us-west-1 --max-results 5

# If AWS CLI is slow, check:
# - Network connectivity
# - AWS credentials are valid
# - Region is correct
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
task provision PLAYS=vulnerabilities.yml

# Or provision specific vulnerability roles
task provision PLAYS=vulnerabilities.yml LIMIT=dc02
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
task validate-vulns OUTPUT=/path/to/custom-report.json
```

### Integrate with CI/CD

Use the validation script in your CI/CD pipeline:

```yaml
# Example GitHub Actions workflow
- name: Validate GOAD Deployment
  run: |
    cd apps/DreadGOAD
    task validate-vulns ENV=staging
  continue-on-error: false
```

### Generate Reports

```bash
# Run validation and generate HTML report
task validate-vulns OUTPUT=/tmp/report.json
python3 scripts/generate-html-report.py /tmp/report.json > /tmp/report.html
```

## Manual Validation

If automated validation fails, you can manually verify vulnerabilities:

### 1. SSM into a Domain Controller

```bash
aws ssm start-session --target i-028f18fd2e04f3ecc --region us-west-1
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

## Next Steps

After validation:

1. **Fix Failed Checks**: Use Ansible to reconfigure any failed vulnerabilities
2. **Document Findings**: Update deployment notes with validation results
3. **Test Exploitation**: Verify vulnerabilities are exploitable with actual attack tools
4. **Regular Validation**: Run validation after any infrastructure changes

## Related Documentation

- [`GOAD-vulnerabilities-comprehensive.md`](./GOAD-vulnerabilities-comprehensive.md) - Complete vulnerability catalog
- [`taskfile.md`](./taskfile.md) - Task usage documentation
- [GOAD Official Docs](https://github.com/Orange-Cyberdefense/GOAD) - Upstream documentation
- [Mayfly's Walkthrough Series](https://mayfly277.github.io/categories/goad/) - Attack technique guides

## Support

For issues with validation:

1. Check the validation script logs
2. Verify AWS credentials and permissions
3. Ensure all instances are running
4. Review Ansible provisioning logs
5. Check the comprehensive vulnerability documentation

---

**Last Updated**: December 15, 2024

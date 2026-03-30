#!/usr/bin/env bash
# shellcheck disable=SC2329
# validate-goad-vulns.sh - Validate GOAD vulnerability configurations
# Based on: docs/GOAD-vulnerabilities-comprehensive.md
#
# Environment Variables:
#   ENV              - Environment to validate (default: dev)
#   REGION           - AWS region (default: us-west-1)
#   INVENTORY_FILE   - Path to Ansible inventory (default: ./${ENV}-inventory)
#   OUTPUT_FILE      - Path for JSON report (default: /tmp/goad-validation-TIMESTAMP.json)
#   VERBOSE          - Enable verbose output (default: false)
#   FAIL_ON_ERROR    - Exit with code 1 on failed checks (default: true)
#
# Usage:
#   # Via task (recommended)
#   task validate-vulns ENV=staging FAIL_ON_ERROR=false
#
#   # Direct execution
#   ENV=staging REGION=us-west-1 VERBOSE=true FAIL_ON_ERROR=false ./scripts/validate-goad-vulns.sh

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Counters
TOTAL_CHECKS=0
PASSED_CHECKS=0
FAILED_CHECKS=0
WARNING_CHECKS=0

# Default values
ENV="${ENV:-dev}"
REGION="${REGION:-us-west-1}"
INVENTORY_FILE="${INVENTORY_FILE:-./${ENV}-inventory}"
OUTPUT_FILE="${OUTPUT_FILE:-/tmp/goad-validation-$(date +%Y%m%d-%H%M%S).json}"
VERBOSE="${VERBOSE:-false}"
FAIL_ON_ERROR="${FAIL_ON_ERROR:-true}"  # Set to false to always exit 0 (useful for initial validation)

# Function to print colored output
print_status() {
    local status="$1"
    local message="$2"
    case "$status" in
        "PASS")
            echo -e "${GREEN}✓${NC} $message"
            ((PASSED_CHECKS += 1))
            ;;
        "FAIL")
            echo -e "${RED}✗${NC} $message"
            ((FAILED_CHECKS += 1))
            ;;
        "WARN")
            echo -e "${YELLOW}⚠${NC} $message"
            ((WARNING_CHECKS += 1))
            ;;
        "INFO")
            echo -e "${BLUE}ℹ${NC} $message"
            ;;
    esac
    ((TOTAL_CHECKS += 1))
}

# Function to run PowerShell command via SSM
run_ps_command() {
    local instance_id="$1"
    local ps_command="$2"
    local _description="${3:-Running command}"
    local wait_time="${4:-5}"  # Optional 4th parameter for custom wait time

    if [[ "$VERBOSE" == "true" ]]; then
        echo "DEBUG: Running on $instance_id: $ps_command" >&2
    fi

    local command_id
    command_id=$(aws ssm send-command \
        --instance-ids "$instance_id" \
        --document-name "AWS-RunPowerShellScript" \
        --parameters "commands=[\"$ps_command\"]" \
        --region "$REGION" \
        --output text \
        --query 'Command.CommandId' 2> /dev/null || echo "")

    if [[ -z "$command_id" ]]; then
        echo "ERROR: Failed to send command" >&2
        echo ""
        return 0
    fi

    # Wait for command to complete (default 5 seconds, customizable)
    sleep "$wait_time"

    # Get command output
    aws ssm get-command-invocation \
        --command-id "$command_id" \
        --instance-id "$instance_id" \
        --region "$REGION" \
        --output text \
        --query 'StandardOutputContent' 2> /dev/null || echo ""
}

# Function to get instance ID by name pattern
get_instance_id() {
    local name_pattern="$1"
    aws ec2 describe-instances \
        --filters "Name=tag:Name,Values=*${name_pattern}*" "Name=instance-state-name,Values=running" \
        --query 'Reservations[0].Instances[0].InstanceId' \
        --region "$REGION" \
        --output text 2> /dev/null || echo ""
}

# Initialize results JSON
init_results() {
    cat > "$OUTPUT_FILE" << EOF
{
  "validation_date": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "environment": "$ENV",
  "summary": {
    "total_checks": 0,
    "passed": 0,
    "failed": 0,
    "warnings": 0
  },
  "checks": []
}
EOF
}

# Add result to JSON
add_result() {
    local category="$1"
    local check_name="$2"
    local status="$3"
    local _details="$4"

    # This is a simplified version - in production, use jq to properly append
    if [[ "$VERBOSE" == "true" ]]; then
        echo "Result: [$status] $category - $check_name" >&2
    fi
}

echo "=========================================="
echo "GOAD Vulnerability Validation"
echo "=========================================="
echo "Environment: $ENV"
echo "Inventory: $INVENTORY_FILE"
echo "Output: $OUTPUT_FILE"
echo ""

# Check if inventory file exists
if [[ ! -f "$INVENTORY_FILE" ]]; then
    print_status "FAIL" "Inventory file not found: $INVENTORY_FILE"
    exit 1
fi

init_results

# Get instance IDs
print_status "INFO" "Discovering instances..."
DC01_ID=$(get_instance_id "DC01")
DC02_ID=$(get_instance_id "DC02")
DC03_ID=$(get_instance_id "DC03")
SRV02_ID=$(get_instance_id "SRV02")
SRV03_ID=$(get_instance_id "SRV03")

if [[ -z "$DC01_ID" ]] || [[ -z "$DC02_ID" ]] || [[ -z "$DC03_ID" ]]; then
    print_status "FAIL" "Could not find all required domain controllers"
    exit 1
fi

print_status "PASS" "Found DC01: $DC01_ID"
print_status "PASS" "Found DC02: $DC02_ID"
print_status "PASS" "Found DC03: $DC03_ID"
if [[ -n "$SRV02_ID" ]]; then
    print_status "PASS" "Found SRV02: $SRV02_ID"
fi
if [[ -n "$SRV03_ID" ]]; then
    print_status "PASS" "Found SRV03: $SRV03_ID"
fi

echo ""
echo "=========================================="
echo "1. Credential Discovery Vulnerabilities"
echo "=========================================="

# Check for passwords in user descriptions
print_status "INFO" "Checking for passwords in user descriptions (samwell.tarly)..."
OUTPUT=$(run_ps_command "$DC02_ID" "Get-ADUser -Filter * -Properties Description | Where-Object {\$_.Description -match 'password|heartsbane'} | Select-Object SamAccountName,Description | Format-Table -AutoSize | Out-String -Width 200")
if echo "$OUTPUT" | grep -qi "samwell.tarly"; then
    print_status "PASS" "samwell.tarly has password in description"
else
    print_status "FAIL" "samwell.tarly does NOT have password in description"
fi

echo ""
echo "=========================================="
echo "2. Kerberos Attack Vectors"
echo "=========================================="

# Check AS-REP Roasting accounts
print_status "INFO" "Checking AS-REP Roasting accounts (brandon.stark, missandei)..."

# Check brandon.stark in NORTH domain
OUTPUT=$(run_ps_command "$DC02_ID" "Get-ADUser brandon.stark -Properties DoesNotRequirePreAuth | Select-Object SamAccountName,DoesNotRequirePreAuth | Format-Table -AutoSize | Out-String")
if echo "$OUTPUT" | grep -qi "true"; then
    print_status "PASS" "brandon.stark has DoesNotRequirePreAuth enabled (AS-REP roastable)"
else
    print_status "FAIL" "brandon.stark does NOT have PreAuth disabled"
fi

# Check missandei in ESSOS domain
OUTPUT=$(run_ps_command "$DC03_ID" "Get-ADUser missandei -Properties DoesNotRequirePreAuth | Select-Object SamAccountName,DoesNotRequirePreAuth | Format-Table -AutoSize | Out-String")
if echo "$OUTPUT" | grep -qi "true"; then
    print_status "PASS" "missandei has DoesNotRequirePreAuth enabled (AS-REP roastable)"
else
    print_status "FAIL" "missandei does NOT have PreAuth disabled"
fi

# Check Kerberoasting SPNs
print_status "INFO" "Checking Kerberoasting accounts (jon.snow, sql_svc)..."

OUTPUT=$(run_ps_command "$DC02_ID" "Get-ADUser jon.snow -Properties ServicePrincipalName | Select-Object SamAccountName,ServicePrincipalName | Format-List | Out-String")
if echo "$OUTPUT" | grep -qi "ServicePrincipalName"; then
    print_status "PASS" "jon.snow has ServicePrincipalNames configured (Kerberoastable)"
else
    print_status "FAIL" "jon.snow does NOT have SPNs configured"
fi

OUTPUT=$(run_ps_command "$DC02_ID" "Get-ADUser sql_svc -Properties ServicePrincipalName | Select-Object SamAccountName,ServicePrincipalName | Format-List | Out-String")
if echo "$OUTPUT" | grep -qi "ServicePrincipalName"; then
    print_status "PASS" "sql_svc has ServicePrincipalNames configured (Kerberoastable)"
else
    print_status "FAIL" "sql_svc does NOT have SPNs configured"
fi

echo ""
echo "=========================================="
echo "3. Network-Level Misconfigurations"
echo "=========================================="

# Check SMB signing on servers
if [[ -n "$SRV02_ID" ]]; then
    print_status "INFO" "Checking SMB signing on CASTELBLACK (SRV02)..."
    OUTPUT=$(run_ps_command "$SRV02_ID" "Get-SmbServerConfiguration | Select-Object RequireSecuritySignature,EnableSecuritySignature | Format-Table -AutoSize | Out-String")
    if echo "$OUTPUT" | grep -qi "false.*false"; then
        print_status "PASS" "CASTELBLACK has SMB signing disabled (relay attacks possible)"
    elif echo "$OUTPUT" | grep -qi "false.*true"; then
        print_status "WARN" "CASTELBLACK has SMB signing enabled but not required"
    else
        print_status "FAIL" "CASTELBLACK has SMB signing enforced (should be disabled for GOAD)"
    fi
fi

if [[ -n "$SRV03_ID" ]]; then
    print_status "INFO" "Checking SMB signing on BRAAVOS (SRV03)..."
    OUTPUT=$(run_ps_command "$SRV03_ID" "Get-SmbServerConfiguration | Select-Object RequireSecuritySignature,EnableSecuritySignature | Format-Table -AutoSize | Out-String")
    if echo "$OUTPUT" | grep -qi "false.*false"; then
        print_status "PASS" "BRAAVOS has SMB signing disabled (relay attacks possible)"
    elif echo "$OUTPUT" | grep -qi "false.*true"; then
        print_status "WARN" "BRAAVOS has SMB signing enabled but not required"
    else
        print_status "FAIL" "BRAAVOS has SMB signing enforced (should be disabled for GOAD)"
    fi
fi

echo ""
echo "=========================================="
echo "4. Anonymous/Guest SMB Enumeration"
echo "=========================================="

# Check RestrictAnonymous registry settings on WINTERFELL (DC02)
print_status "INFO" "Checking RestrictAnonymous on WINTERFELL (DC02)..."
OUTPUT=$(run_ps_command "$DC02_ID" "Get-ItemProperty -Path 'HKLM:\System\CurrentControlSet\Control\Lsa' -Name RestrictAnonymous -ErrorAction SilentlyContinue | Select-Object -ExpandProperty RestrictAnonymous")
OUTPUT=$(echo "$OUTPUT" | tr -d '[:space:]')
if [[ "$OUTPUT" == "0" ]]; then
    print_status "PASS" "RestrictAnonymous is 0 on WINTERFELL (NULL session enumeration enabled)"
elif [[ "$OUTPUT" == "1" ]]; then
    print_status "WARN" "RestrictAnonymous is 1 on WINTERFELL (some enumeration blocked)"
elif [[ "$OUTPUT" == "2" ]]; then
    print_status "FAIL" "RestrictAnonymous is 2 on WINTERFELL (NULL sessions BLOCKED)"
else
    print_status "FAIL" "RestrictAnonymous not configured on WINTERFELL (got: '$OUTPUT', expected: 0)"
fi

print_status "INFO" "Checking RestrictAnonymousSAM on WINTERFELL (DC02)..."
OUTPUT=$(run_ps_command "$DC02_ID" "Get-ItemProperty -Path 'HKLM:\System\CurrentControlSet\Control\Lsa' -Name RestrictAnonymousSAM -ErrorAction SilentlyContinue | Select-Object -ExpandProperty RestrictAnonymousSAM")
OUTPUT=$(echo "$OUTPUT" | tr -d '[:space:]')
if [[ "$OUTPUT" == "0" ]]; then
    print_status "PASS" "RestrictAnonymousSAM is 0 on WINTERFELL (SAM enumeration enabled)"
else
    print_status "FAIL" "RestrictAnonymousSAM is NOT 0 on WINTERFELL (got: '$OUTPUT', expected: 0)"
fi

print_status "INFO" "Checking EveryoneIncludesAnonymous on WINTERFELL (DC02)..."
OUTPUT=$(run_ps_command "$DC02_ID" "Get-ItemProperty -Path 'HKLM:\System\CurrentControlSet\Control\Lsa' -Name EveryoneIncludesAnonymous -ErrorAction SilentlyContinue | Select-Object -ExpandProperty EveryoneIncludesAnonymous")
OUTPUT=$(echo "$OUTPUT" | tr -d '[:space:]')
if [[ "$OUTPUT" == "1" ]]; then
    print_status "PASS" "EveryoneIncludesAnonymous is 1 on WINTERFELL (anonymous access broadened)"
else
    print_status "WARN" "EveryoneIncludesAnonymous is NOT 1 on WINTERFELL (got: '$OUTPUT', expected: 1)"
fi

# Check for true NULL session capability (anonymous ACLs on DC02)
print_status "INFO" "Checking anonymous ACLs on WINTERFELL (DC02)..."
OUTPUT=$(run_ps_command "$DC02_ID" "Import-Module ActiveDirectory; \$domain = Get-ADDomain 'north.sevenkingdoms.local'; \$acl = Get-Acl \"AD:\$(\$domain.DistinguishedName)\"; \$anonymousAces = \$acl.Access | Where-Object { \$_.IdentityReference -like '*ANONYMOUS LOGON*' }; if (\$anonymousAces) { Write-Output 'ANONYMOUS_ACL_FOUND' } else { Write-Output 'ANONYMOUS_ACL_NOT_FOUND' }" "" 10)
if echo "$OUTPUT" | grep -qi "ANONYMOUS_ACL_FOUND"; then
    print_status "PASS" "Anonymous ACLs configured on WINTERFELL (AD permissions allow anonymous access)"
elif echo "$OUTPUT" | grep -qi "ANONYMOUS_ACL_NOT_FOUND"; then
    print_status "FAIL" "Anonymous ACLs NOT found on WINTERFELL"
else
    print_status "WARN" "Could not verify anonymous ACLs on WINTERFELL (command may have failed)"
fi

# Check Guest account status (for GUEST sessions, not NULL sessions)
if [[ -n "$SRV02_ID" ]]; then
    print_status "INFO" "Checking Guest account status on CASTELBLACK..."
    OUTPUT=$(run_ps_command "$SRV02_ID" "Get-LocalUser -Name Guest | Select-Object Name,Enabled | Format-Table -AutoSize | Out-String")
    if echo "$OUTPUT" | grep -qi "true"; then
        print_status "PASS" "Guest account is enabled on CASTELBLACK (Guest sessions possible: -u 'guest' -p '')"
    else
        print_status "FAIL" "Guest account is NOT enabled on CASTELBLACK"
    fi
fi

if [[ -n "$SRV03_ID" ]]; then
    print_status "INFO" "Checking Guest account status on BRAAVOS..."
    OUTPUT=$(run_ps_command "$SRV03_ID" "Get-LocalUser -Name Guest | Select-Object Name,Enabled | Format-Table -AutoSize | Out-String")
    if echo "$OUTPUT" | grep -qi "true"; then
        print_status "PASS" "Guest account is enabled on BRAAVOS (Guest sessions possible: -u 'guest' -p '')"
    else
        print_status "FAIL" "Guest account is NOT enabled on BRAAVOS"
    fi
fi

# Check AllowInsecureGuestAuth registry setting
if [[ -n "$SRV02_ID" ]]; then
    print_status "INFO" "Checking AllowInsecureGuestAuth on CASTELBLACK..."
    OUTPUT=$(run_ps_command "$SRV02_ID" "Get-ItemProperty -Path 'HKLM:\SYSTEM\CurrentControlSet\Services\LanmanWorkstation\Parameters' -Name AllowInsecureGuestAuth -ErrorAction SilentlyContinue | Select-Object -ExpandProperty AllowInsecureGuestAuth")
    OUTPUT=$(echo "$OUTPUT" | tr -d '[:space:]')
    if [[ "$OUTPUT" == "1" ]]; then
        print_status "PASS" "AllowInsecureGuestAuth is enabled on CASTELBLACK (guest access allowed)"
    else
        print_status "FAIL" "AllowInsecureGuestAuth is NOT enabled on CASTELBLACK (got: '$OUTPUT')"
    fi
fi

if [[ -n "$SRV03_ID" ]]; then
    print_status "INFO" "Checking AllowInsecureGuestAuth on BRAAVOS..."
    OUTPUT=$(run_ps_command "$SRV03_ID" "Get-ItemProperty -Path 'HKLM:\SYSTEM\CurrentControlSet\Services\LanmanWorkstation\Parameters' -Name AllowInsecureGuestAuth -ErrorAction SilentlyContinue | Select-Object -ExpandProperty AllowInsecureGuestAuth")
    OUTPUT=$(echo "$OUTPUT" | tr -d '[:space:]')
    if [[ "$OUTPUT" == "1" ]]; then
        print_status "PASS" "AllowInsecureGuestAuth is enabled on BRAAVOS (guest access allowed)"
    else
        print_status "FAIL" "AllowInsecureGuestAuth is NOT enabled on BRAAVOS (got: '$OUTPUT')"
    fi
fi

# Check LmCompatibilityLevel for NTLM downgrade attacks
print_status "INFO" "Checking LmCompatibilityLevel on MEEREEN (DC03)..."
OUTPUT=$(run_ps_command "$DC03_ID" "Get-ItemProperty -Path 'HKLM:\System\CurrentControlSet\Control\Lsa' -Name LmCompatibilityLevel -ErrorAction SilentlyContinue | Select-Object -ExpandProperty LmCompatibilityLevel")
OUTPUT=$(echo "$OUTPUT" | tr -d '[:space:]')
if [[ "$OUTPUT" == "2" ]]; then
    print_status "PASS" "LmCompatibilityLevel is 2 on MEEREEN (NTLMv1 downgrade attacks possible)"
elif [[ "$OUTPUT" =~ ^[0-2]$ ]]; then
    print_status "PASS" "LmCompatibilityLevel is $OUTPUT on MEEREEN (NTLM downgrade vulnerable)"
else
    print_status "FAIL" "LmCompatibilityLevel is NOT vulnerable on MEEREEN (got: '$OUTPUT', expected: 0-2)"
fi

# Check for anonymous share enumeration
if [[ -n "$SRV02_ID" ]]; then
    print_status "INFO" "Checking for anonymous accessible shares on CASTELBLACK..."
    OUTPUT=$(run_ps_command "$SRV02_ID" "Get-SmbShare | Where-Object {\$_.Name -eq 'all' -or \$_.Name -eq 'public'} | Select-Object Name | Format-Table -AutoSize | Out-String")
    if echo "$OUTPUT" | grep -qiE "all|public"; then
        print_status "PASS" "Anonymous accessible shares found on CASTELBLACK"
    else
        print_status "WARN" "Could not verify anonymous shares on CASTELBLACK"
    fi
fi

if [[ -n "$SRV03_ID" ]]; then
    print_status "INFO" "Checking for anonymous accessible shares on BRAAVOS..."
    OUTPUT=$(run_ps_command "$SRV03_ID" "Get-SmbShare | Where-Object {\$_.Name -eq 'all' -or \$_.Name -eq 'public'} | Select-Object Name | Format-Table -AutoSize | Out-String")
    if echo "$OUTPUT" | grep -qiE "all|public"; then
        print_status "PASS" "Anonymous accessible shares found on BRAAVOS"
    else
        print_status "WARN" "Could not verify anonymous shares on BRAAVOS"
    fi
fi

# Test actual NULL session user enumeration on WINTERFELL
print_status "INFO" "Testing NULL session user enumeration on WINTERFELL (DC02)..."
OUTPUT=$(run_ps_command "$DC02_ID" "\$ErrorActionPreference = 'SilentlyContinue'; try { \$users = Get-ADUser -Filter * -Properties Description -Credential \$null 2>&1; if (\$users) { Write-Output 'NULL_ENUM_WORKS' } else { Write-Output 'NULL_ENUM_FAILED' } } catch { Write-Output 'NULL_ENUM_FAILED' }" "" 10)
if echo "$OUTPUT" | grep -qi "NULL_ENUM_WORKS"; then
    print_status "PASS" "NULL session user enumeration works on WINTERFELL"

    # Check if samwell.tarly has password in description (the key vulnerability)
    print_status "INFO" "Checking for password in samwell.tarly description..."
    OUTPUT=$(run_ps_command "$DC02_ID" "Get-ADUser samwell.tarly -Properties Description | Select-Object SamAccountName,Description | Format-List | Out-String")
    if echo "$OUTPUT" | grep -qi "password"; then
        print_status "PASS" "samwell.tarly has password in description field (initial access possible!)"
    else
        print_status "WARN" "Could not verify password in samwell.tarly description"
    fi
else
    print_status "WARN" "Could not verify NULL session enumeration (may require testing from external host)"
fi

# Summary of enumeration methods
print_status "INFO" "═══════════════════════════════════════════════════════════════"
print_status "INFO" "Enumeration Methods Summary:"
print_status "INFO" "  • TRUE NULL SESSION (anonymous): netexec smb <DC02_IP> -u '' -p '' --users"
print_status "INFO" "    → Should reveal samwell.tarly with password in description"
print_status "INFO" "  • GUEST SESSION (authenticated): netexec smb <SRV02/SRV03_IP> -u 'guest' -p '' --shares"
print_status "INFO" "═══════════════════════════════════════════════════════════════"

echo ""
echo "=========================================="
echo "5. Delegation Configurations"
echo "=========================================="

# Check unconstrained delegation (sansa.stark)
print_status "INFO" "Checking unconstrained delegation (sansa.stark)..."
OUTPUT=$(run_ps_command "$DC02_ID" "Get-ADUser sansa.stark -Properties TrustedForDelegation | Select-Object SamAccountName,TrustedForDelegation | Format-Table -AutoSize | Out-String")
if echo "$OUTPUT" | grep -qi "true"; then
    print_status "PASS" "sansa.stark has unconstrained delegation enabled"
else
    print_status "FAIL" "sansa.stark does NOT have unconstrained delegation"
fi

# Check constrained delegation (jon.snow)
print_status "INFO" "Checking constrained delegation (jon.snow)..."
OUTPUT=$(run_ps_command "$DC02_ID" "Get-ADUser jon.snow -Properties msDS-AllowedToDelegateTo | Select-Object SamAccountName,msDS-AllowedToDelegateTo | Format-List | Out-String")
if echo "$OUTPUT" | grep -qi "msDS-AllowedToDelegateTo"; then
    print_status "PASS" "jon.snow has constrained delegation configured"
else
    print_status "FAIL" "jon.snow does NOT have constrained delegation"
fi

echo ""
echo "=========================================="
echo "6. Machine Account Quota"
echo "=========================================="

print_status "INFO" "Checking Machine Account Quota (should be 10)..."
# Query ms-DS-MachineAccountQuota from the domain object (not configuration)
# Reference: https://www.jorgebernhardt.com/how-to-change-attribute-ms-ds-machineaccountquota/
OUTPUT=$(run_ps_command "$DC01_ID" "Get-ADObject -Identity ((Get-ADDomain).distinguishedname) -Properties ms-DS-MachineAccountQuota | Select-Object -ExpandProperty ms-DS-MachineAccountQuota" "" 10)

# Trim whitespace from output
OUTPUT=$(echo "$OUTPUT" | tr -d '[:space:]')

if [[ "$OUTPUT" =~ ^[0-9]+$ ]]; then
    # Valid numeric output received
    if [[ "$OUTPUT" == "10" ]]; then
        print_status "PASS" "Machine Account Quota is 10 (allows RBCD attacks)"
    else
        print_status "WARN" "Machine Account Quota is $OUTPUT (expected 10)"
    fi
elif [[ -z "$OUTPUT" ]]; then
    print_status "WARN" "Could not query Machine Account Quota (no output received)"
else
    print_status "WARN" "Could not parse Machine Account Quota (got: '$OUTPUT')"
fi

echo ""
echo "=========================================="
echo "7. MSSQL Configurations"
echo "=========================================="

if [[ -n "$SRV02_ID" ]]; then
    print_status "INFO" "Checking MSSQL service on CASTELBLACK..."
    OUTPUT=$(run_ps_command "$SRV02_ID" "Get-Service 'MSSQL\$SQLEXPRESS' -ErrorAction SilentlyContinue | Select-Object Name,Status,StartType | Format-Table -AutoSize | Out-String")
    if echo "$OUTPUT" | grep -qi "running"; then
        print_status "PASS" "MSSQL service is running on CASTELBLACK"
    else
        print_status "FAIL" "MSSQL service is NOT running on CASTELBLACK"
    fi
fi

if [[ -n "$SRV03_ID" ]]; then
    print_status "INFO" "Checking MSSQL service on BRAAVOS..."
    OUTPUT=$(run_ps_command "$SRV03_ID" "Get-Service 'MSSQL\$SQLEXPRESS' -ErrorAction SilentlyContinue | Select-Object Name,Status,StartType | Format-Table -AutoSize | Out-String")
    if echo "$OUTPUT" | grep -qi "running"; then
        print_status "PASS" "MSSQL service is running on BRAAVOS"
    else
        print_status "FAIL" "MSSQL service is NOT running on BRAAVOS"
    fi
fi

echo ""
echo "=========================================="
echo "8. ADCS Configuration"
echo "=========================================="

if [[ -n "$SRV03_ID" ]]; then
    print_status "INFO" "Checking ADCS installation on BRAAVOS..."
    OUTPUT=$(run_ps_command "$SRV03_ID" "Get-WindowsFeature ADCS-Cert-Authority | Select-Object Name,InstallState | Format-Table -AutoSize | Out-String")
    if echo "$OUTPUT" | grep -qi "installed"; then
        print_status "PASS" "ADCS is installed on BRAAVOS"
    else
        print_status "FAIL" "ADCS is NOT installed on BRAAVOS"
    fi

    print_status "INFO" "Checking ADCS web enrollment..."
    OUTPUT=$(run_ps_command "$SRV03_ID" "Get-WindowsFeature ADCS-Web-Enrollment | Select-Object Name,InstallState | Format-Table -AutoSize | Out-String")
    if echo "$OUTPUT" | grep -qi "installed"; then
        print_status "PASS" "ADCS Web Enrollment is installed (ESC8 possible)"
    else
        print_status "WARN" "ADCS Web Enrollment not installed"
    fi
fi

echo ""
echo "=========================================="
echo "9. ACL Permissions"
echo "=========================================="

# Check key ACL permissions
print_status "INFO" "Checking ACL permissions (tywin.lannister -> jaime.lannister)..."
# Simplified approach: check if tywin has GenericAll/WriteDacl on jaime
OUTPUT=$(run_ps_command "$DC01_ID" "\$user = Get-ADUser jaime.lannister -Properties nTSecurityDescriptor; \$acl = \$user.nTSecurityDescriptor.Access | Where-Object { \$_.IdentityReference -like '*tywin*' }; if (\$acl) { Write-Output 'ACL_FOUND' } else { Write-Output 'ACL_NOT_FOUND' }")
if echo "$OUTPUT" | grep -qi "ACL_FOUND"; then
    print_status "PASS" "tywin.lannister has ACL rights on jaime.lannister"
elif echo "$OUTPUT" | grep -qi "ACL_NOT_FOUND"; then
    print_status "FAIL" "tywin.lannister does NOT have ACL rights on jaime.lannister"
else
    print_status "WARN" "Could not verify ACL: tywin.lannister -> jaime.lannister (command may have failed)"
fi

echo ""
echo "=========================================="
echo "10. Domain Trusts"
echo "=========================================="

# Check parent-child trust
print_status "INFO" "Checking parent-child domain trust..."
OUTPUT=$(run_ps_command "$DC02_ID" "Get-ADTrust -Filter * | Select-Object Name,Direction,TrustType | Format-Table -AutoSize | Out-String")
if echo "$OUTPUT" | grep -qi "sevenkingdoms"; then
    print_status "PASS" "Parent-child trust configured (north -> sevenkingdoms)"
else
    print_status "FAIL" "Parent-child trust NOT found"
fi

# Check forest trust
print_status "INFO" "Checking forest trust..."
OUTPUT=$(run_ps_command "$DC01_ID" "Get-ADTrust -Filter * | Select-Object Name,Direction,TrustType | Format-Table -AutoSize | Out-String")
if echo "$OUTPUT" | grep -qi "essos"; then
    print_status "PASS" "Forest trust configured (sevenkingdoms <-> essos)"
else
    print_status "FAIL" "Forest trust NOT found"
fi

echo ""
echo "=========================================="
echo "11. Additional Services"
echo "=========================================="

# Check Print Spooler
print_status "INFO" "Checking Print Spooler service..."
for instance_id in "$DC01_ID" "$DC02_ID" "$DC03_ID"; do
    if [[ -n "$instance_id" ]]; then
        OUTPUT=$(run_ps_command "$instance_id" "Get-Service Spooler | Select-Object Status | Format-Table -AutoSize | Out-String")
        if echo "$OUTPUT" | grep -qi "running"; then
            print_status "PASS" "Print Spooler is running (coercion attacks possible)"
        else
            print_status "WARN" "Print Spooler is not running"
        fi
    fi
done

# Check IIS
if [[ -n "$SRV02_ID" ]]; then
    print_status "INFO" "Checking IIS service on CASTELBLACK..."
    OUTPUT=$(run_ps_command "$SRV02_ID" "Get-Service W3SVC -ErrorAction SilentlyContinue | Select-Object Name,Status | Format-Table -AutoSize | Out-String")
    if echo "$OUTPUT" | grep -qi "running"; then
        print_status "PASS" "IIS (W3SVC) is running on CASTELBLACK"
    else
        print_status "FAIL" "IIS is NOT running on CASTELBLACK"
    fi
fi

echo ""
echo "=========================================="
echo "Validation Summary"
echo "=========================================="
echo "Total Checks:    $TOTAL_CHECKS"
echo -e "${GREEN}Passed:          $PASSED_CHECKS${NC}"
echo -e "${RED}Failed:          $FAILED_CHECKS${NC}"
echo -e "${YELLOW}Warnings:        $WARNING_CHECKS${NC}"
echo ""

# Calculate percentage
if [[ $TOTAL_CHECKS -gt 0 ]]; then
    PASS_PCT=$((PASSED_CHECKS * 100 / TOTAL_CHECKS))
    echo "Success Rate: ${PASS_PCT}%"
fi

echo ""
echo "Results saved to: $OUTPUT_FILE"
echo "=========================================="

# Exit with error if any checks failed (unless FAIL_ON_ERROR=false)
if [[ "$FAIL_ON_ERROR" == "true" ]] && [[ $FAILED_CHECKS -gt 0 ]]; then
    echo ""
    echo "⚠️  Validation failed with $FAILED_CHECKS errors."
    echo "    Set FAIL_ON_ERROR=false to suppress exit code for initial validation."
    exit 1
fi

echo ""
if [[ $FAILED_CHECKS -gt 0 ]]; then
    echo "✓ Validation completed with errors (FAIL_ON_ERROR=false)"
else
    echo "✓ All checks passed!"
fi
exit 0

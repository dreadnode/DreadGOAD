#!/bin/bash
# Display content of files related to specific playbooks
# Required env vars: PLAYBOOK, ENV

set -euo pipefail

if [ -z "${PLAYBOOK:-}" ]; then
    echo "Please specify a playbook to get files for."
    echo "Usage: task get-files PLAYBOOK=<playbook>"
    echo ""
    echo "Available playbooks:"
    echo "  build, ad-servers, ad-parent-domain, ad-child-domain,"
    echo "  ad-members, ad-trusts, ad-data, ad-gmsa, laps,"
    echo "  ad-relations, adcs, ad-acl, servers, security, vulnerabilities"
    exit 1
fi

declare -a files

case "${PLAYBOOK}" in
    build)
        files=( 
            "ansible/build.yml"
            "ansible/roles/common/tasks/main.yml"
            "ansible/roles/settings/keyboard/tasks/main.yml"
            "ansible/roles/settings/no_updates/tasks/main.yml"
            "ansible/roles/settings/updates/tasks/default.yml"
            "ad/GOAD/data/${ENV}-config.json"
            "ansible/data.yml"
        )
        ;;
    ad-servers)
        files=(
            "ansible/ad-servers.yml"
            "ansible/roles/settings/admin_password/tasks/main.yml"
            "ansible/roles/settings/hostname/tasks/main.yml"
            "ad/GOAD/data/${ENV}-config.json"
            "ansible/data.yml"
        )
        ;;
    ad-parent-domain)
        files=(
            "ansible/ad-parent_domain.yml"
            "ansible/roles/domain_controller/tasks/main.yml"
            "${ENV}-inventory"
            "ad/GOAD/data/${ENV}-config.json"
            "ansible/data.yml"
        )
        ;;
    ad-child-domain)
        files=(
            "ansible/ad-child_domain.yml"
            "ansible/roles/child_domain/tasks/main.yml"
            "ansible/roles/dns_conditional_forwarder/tasks/main.yml"
            "ansible/roles/parent_child_dns/tasks/main.yml"
            "${ENV}-inventory"
            "ad/GOAD/data/${ENV}-config.json"
            "ansible/data.yml"
        )
        ;;
    ad-members)
        files=(
            "ansible/ad-members.yml"
            "ansible/roles/member_server/tasks/main.yml"
            "ansible/roles/commonwkstn/tasks/main.yml"
            "${ENV}-inventory"
            "ad/GOAD/data/${ENV}-config.json"
            "ansible/data.yml"
        )
        ;;
    ad-trusts)
        files=(
            "ansible/ad-trusts.yml"
            "ansible/roles/settings/disable_nat_adapter/tasks/main.yml"
            "ansible/roles/dns_conditional_forwarder/tasks/main.yml"
            "ansible/roles/trusts/tasks/main.yml"
            "ansible/roles/settings/enable_nat_adapter/tasks/main.yml"
            "ansible/roles/dc_dns_conditional_forwarder/tasks/main.yml"
            "${ENV}-inventory"
            "ad/GOAD/data/${ENV}-config.json"
            "ansible/data.yml"
        )
        ;;
    ad-data)
        files=(
            "ansible/ad-data.yml"
            "ansible/roles/password_policy/tasks/main.yml"
            "ansible/roles/ad/tasks/main.yml"
            "ansible/roles/ad/tasks/users.yml"
            "ansible/roles/ad/tasks/groups.yml"
            "ansible/roles/ad/tasks/ou.yml"
            "ansible/roles/settings/copy_files/tasks/main.yml"
            "ansible/roles/move_to_ou/tasks/main.yml"
            "${ENV}-inventory"
            "ad/GOAD/data/${ENV}-config.json"
            "ansible/data.yml"
        )
        ;;
    ad-gmsa)
        files=(
            "ansible/ad-gmsa.yml"
            "ansible/roles/gmsa/tasks/main.yml"
            "ansible/roles/gmsa_hosts/tasks/main.yml"
            "${ENV}-inventory"
            "ad/GOAD/data/${ENV}-config.json"
            "ansible/data.yml"
        )
        ;;
    laps)
        files=(
            "ansible/laps.yml"
            "ansible/roles/laps/dc/tasks/main.yml"
            "ansible/roles/laps/dc/vars/main.yml"
            "ansible/roles/laps/dc/tasks/move_server_to_ou.yml"
            "ansible/roles/laps/dc/tasks/install.yml"
            "ansible/roles/laps/dc/defaults/main.yml"
            "${ENV}-inventory"
            "ad/GOAD/data/${ENV}-config.json"
            "ansible/data.yml"
        )
        ;;
    ad-relations)
        files=(
            "ansible/ad-relations.yml"
            "ansible/roles/settings/adjust_rights/tasks/main.yml"
            "ansible/roles/settings/user_rights/tasks/main.yml"
            "ansible/roles/groups_domains/tasks/main.yml"
            "${ENV}-inventory"
            "ad/GOAD/data/${ENV}-config.json"
            "ansible/data.yml"
        )
        ;;
    adcs)
        files=(
            "ansible/adcs.yml"
            "ansible/roles/adcs/tasks/main.yml"
            "ansible/roles/adcs_templates/tasks/main.yml"
            "ansible/roles/adcs_templates/files/ESC1.json"
            "ansible/roles/adcs_templates/files/ESC2.json"
            "ansible/roles/adcs_templates/files/ESC3-CRA.json"
            "ansible/roles/adcs_templates/files/ESC3.json"
            "ansible/roles/adcs_templates/files/ESC4.json"
            "ansible/roles/adcs_templates/files/ADCSTemplate/ADCSTemplate.psd1"
            "ansible/roles/adcs_templates/files/ADCSTemplate/DSCResources/COMMUNITY_ADCSTemplate/COMMUNITY_ADCSTemplate.psm1"
            "ansible/roles/adcs_templates/files/ADCSTemplate/DSCResources/COMMUNITY_ADCSTemplate/COMMUNITY_ADCSTemplate.schema.mof"
            "ansible/roles/adcs_templates/files/ADCSTemplate/ADCSTemplate.psm1"
            "${ENV}-inventory"
            "ad/GOAD/data/${ENV}-config.json"
            "ansible/data.yml"
        )
        ;;
    ad-acl)
        files=(
            "ansible/ad-acl.yml"
            "ansible/roles/acl/tasks/main.yml"
            "${ENV}-inventory"
            "ad/GOAD/data/${ENV}-config.json"
            "ansible/data.yml"
        )
        ;;
    servers)
        files=(
            "ansible/servers.yml"
            "ansible/roles/iis/tasks/main.yml"
            "ansible/roles/iis/files/index.html"
            "ansible/roles/mssql/tasks/main.yml"
            "ansible/roles/mssql/defaults/main.yml"
            "ansible/roles/mssql/files/sql_conf.ini.MSSQL_2019.j2"
            "ansible/roles/mssql/files/sql_conf.ini.MSSQL_2022.j2"
            "ansible/roles/mssql_link/tasks/main.yml"
            "ansible/roles/mssql_link/tasks/logins.yml"
            "ansible/roles/mssql_ssms/tasks/main.yml"
            "ansible/roles/webdav/tasks/main.yml"
            "${ENV}-inventory"
            "ad/GOAD/data/${ENV}-config.json"
            "ansible/data.yml"
        )
        ;;
    security)
        files=(
            "ansible/security.yml"
            "ansible/roles/settings/windows_defender/tasks/main.yml"
            "ansible/roles/security/account_is_sensitive/tasks/main.yml"
            "ansible/roles/security/powershell_restrict/tasks/main.yml"
            "ansible/roles/security/enable_run_as_ppl/tasks/main.yml"
            "${ENV}-inventory"
            "ad/GOAD/data/${ENV}-config.json"
            "ansible/data.yml"
        )
        ;;
    vulnerabilities)
        files=(
            "ansible/vulnerabilities.yml"
            "ansible/roles/vulns/schedule/tasks/main.yml"
            "ansible/roles/vulns/autologon/tasks/main.yml"
            "ansible/roles/vulns/openshares/tasks/main.yml"
            "ansible/roles/vulns/disable_firewall/tasks/main.yml"
            "ansible/roles/vulns/ntlmdowngrade/tasks/main.yml"
            "ansible/roles/vulns/enable_credssp_client/tasks/main.yml"
            "ansible/roles/vulns/administrator_folder/tasks/main.yml"
            "ansible/roles/vulns/acls/tasks/main.yml"
            "ansible/roles/vulns/smbv1/tasks/main.yml"
            "ansible/roles/vulns/enable_llmnr/tasks/main.yml"
            "ansible/roles/vulns/adcs_templates/tasks/main.yml"
            "ansible/roles/vulns/adcs_templates/files/ADCSTemplate/ADCSTemplate.psd1"
            "ansible/roles/vulns/adcs_templates/files/ADCSTemplate/ADCSTemplate.psm1"
            "ansible/roles/vulns/adcs_templates/files/ADCSTemplate/Examples/Tanium.json"
            "ansible/roles/vulns/adcs_templates/files/ADCSTemplate/Examples/Demo.ps1"
            "ansible/roles/vulns/adcs_templates/files/ADCSTemplate/Examples/Build-ADCS.ps1"
            "ansible/roles/vulns/adcs_templates/files/ADCSTemplate/Examples/PowerShellCMS.json"
            "ansible/roles/vulns/adcs_templates/files/ADCSTemplate/DSCResources/COMMUNITY_ADCSTemplate/COMMUNITY_ADCSTemplate.psm1"
            "ansible/roles/vulns/adcs_templates/files/ADCSTemplate/DSCResources/COMMUNITY_ADCSTemplate/COMMUNITY_ADCSTemplate.schema.mof"
            "ansible/roles/vulns/permissions/tasks/main.yml"
            "ansible/roles/vulns/enable_nbt-ns/tasks/main.yml"
            "ansible/roles/vulns/directory/tasks/main.yml"
            "ansible/roles/vulns/files/tasks/main.yml"
            "ansible/roles/vulns/enable_credssp_server/tasks/main.yml"
            "ansible/roles/vulns/shares/tasks/main.yml"
            "ansible/roles/vulns/mssql/tasks/main.yml"
            "ansible/roles/vulns/credentials/tasks/main.yml"
            "${ENV}-inventory"
            "ad/GOAD/data/${ENV}-config.json"
            "ansible/data.yml"
        )
        ;;
    *)
        echo "Unknown playbook: ${PLAYBOOK}"
        echo "Available playbooks:"
        echo "  build, ad-servers, ad-parent-domain, ad-child-domain,"
        echo "  ad-members, ad-trusts, ad-data, ad-gmsa, laps,"
        echo "  ad-relations, adcs, ad-acl, servers, security, vulnerabilities"
        exit 1
        ;;
esac

# Display the files
for file in "${files[@]}"; do
    if [ -f "$file" ]; then
        echo -e "\n==== $file ===="
        cat "$file"
    else
        echo -e "\n==== $file [FILE NOT FOUND] ===="
    fi
done
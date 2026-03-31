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
            "playbooks/build.yml"
            "roles/common/tasks/main.yml"
            "roles/settings_keyboard/tasks/main.yml"
            "roles/settings_no_updates/tasks/main.yml"
            "roles/settings_updates/tasks/default.yml"
            "ad/GOAD/data/${ENV}-config.json"
            "playbooks/data.yml"
        )
        ;;
    ad-servers)
        files=(
            "playbooks/ad-servers.yml"
            "roles/settings_admin_password/tasks/main.yml"
            "roles/settings_hostname/tasks/main.yml"
            "ad/GOAD/data/${ENV}-config.json"
            "playbooks/data.yml"
        )
        ;;
    ad-parent-domain)
        files=(
            "playbooks/ad-parent_domain.yml"
            "roles/domain_controller/tasks/main.yml"
            "${ENV}-inventory"
            "ad/GOAD/data/${ENV}-config.json"
            "playbooks/data.yml"
        )
        ;;
    ad-child-domain)
        files=(
            "playbooks/ad-child_domain.yml"
            "roles/child_domain/tasks/main.yml"
            "roles/dns_conditional_forwarder/tasks/main.yml"
            "roles/parent_child_dns/tasks/main.yml"
            "${ENV}-inventory"
            "ad/GOAD/data/${ENV}-config.json"
            "playbooks/data.yml"
        )
        ;;
    ad-members)
        files=(
            "playbooks/ad-members.yml"
            "roles/member_server/tasks/main.yml"
            "roles/commonwkstn/tasks/main.yml"
            "${ENV}-inventory"
            "ad/GOAD/data/${ENV}-config.json"
            "playbooks/data.yml"
        )
        ;;
    ad-trusts)
        files=(
            "playbooks/ad-trusts.yml"
            "roles/settings_disable_nat_adapter/tasks/main.yml"
            "roles/dns_conditional_forwarder/tasks/main.yml"
            "roles/trusts/tasks/main.yml"
            "roles/settings_enable_nat_adapter/tasks/main.yml"
            "roles/dc_dns_conditional_forwarder/tasks/main.yml"
            "${ENV}-inventory"
            "ad/GOAD/data/${ENV}-config.json"
            "playbooks/data.yml"
        )
        ;;
    ad-data)
        files=(
            "playbooks/ad-data.yml"
            "roles/password_policy/tasks/main.yml"
            "roles/ad/tasks/main.yml"
            "roles/ad/tasks/users.yml"
            "roles/ad/tasks/groups.yml"
            "roles/ad/tasks/ou.yml"
            "roles/settings_copy_files/tasks/main.yml"
            "roles/move_to_ou/tasks/main.yml"
            "${ENV}-inventory"
            "ad/GOAD/data/${ENV}-config.json"
            "playbooks/data.yml"
        )
        ;;
    ad-gmsa)
        files=(
            "playbooks/ad-gmsa.yml"
            "roles/gmsa/tasks/main.yml"
            "roles/gmsa_hosts/tasks/main.yml"
            "${ENV}-inventory"
            "ad/GOAD/data/${ENV}-config.json"
            "playbooks/data.yml"
        )
        ;;
    laps)
        files=(
            "playbooks/laps.yml"
            "roles/laps_dc/tasks/main.yml"
            "roles/laps_dc/vars/main.yml"
            "roles/laps_dc/tasks/move_server_to_ou.yml"
            "roles/laps_dc/tasks/install.yml"
            "roles/laps_dc/defaults/main.yml"
            "${ENV}-inventory"
            "ad/GOAD/data/${ENV}-config.json"
            "playbooks/data.yml"
        )
        ;;
    ad-relations)
        files=(
            "playbooks/ad-relations.yml"
            "roles/settings_adjust_rights/tasks/main.yml"
            "roles/settings_user_rights/tasks/main.yml"
            "roles/groups_domains/tasks/main.yml"
            "${ENV}-inventory"
            "ad/GOAD/data/${ENV}-config.json"
            "playbooks/data.yml"
        )
        ;;
    adcs)
        files=(
            "playbooks/adcs.yml"
            "roles/adcs/tasks/main.yml"
            "roles/adcs_templates/tasks/main.yml"
            "roles/adcs_templates/files/ESC1.json"
            "roles/adcs_templates/files/ESC2.json"
            "roles/adcs_templates/files/ESC3-CRA.json"
            "roles/adcs_templates/files/ESC3.json"
            "roles/adcs_templates/files/ESC4.json"
            "roles/adcs_templates/files/ADCSTemplate/ADCSTemplate.psd1"
            "roles/adcs_templates/files/ADCSTemplate/DSCResources/COMMUNITY_ADCSTemplate/COMMUNITY_ADCSTemplate.psm1"
            "roles/adcs_templates/files/ADCSTemplate/DSCResources/COMMUNITY_ADCSTemplate/COMMUNITY_ADCSTemplate.schema.mof"
            "roles/adcs_templates/files/ADCSTemplate/ADCSTemplate.psm1"
            "${ENV}-inventory"
            "ad/GOAD/data/${ENV}-config.json"
            "playbooks/data.yml"
        )
        ;;
    ad-acl)
        files=(
            "playbooks/ad-acl.yml"
            "roles/acl/tasks/main.yml"
            "${ENV}-inventory"
            "ad/GOAD/data/${ENV}-config.json"
            "playbooks/data.yml"
        )
        ;;
    servers)
        files=(
            "playbooks/servers.yml"
            "roles/iis/tasks/main.yml"
            "roles/iis/files/index.html"
            "roles/mssql/tasks/main.yml"
            "roles/mssql/defaults/main.yml"
            "roles/mssql/files/sql_conf.ini.MSSQL_2019.j2"
            "roles/mssql/files/sql_conf.ini.MSSQL_2022.j2"
            "roles/mssql_link/tasks/main.yml"
            "roles/mssql_link/tasks/logins.yml"
            "roles/mssql_ssms/tasks/main.yml"
            "roles/webdav/tasks/main.yml"
            "${ENV}-inventory"
            "ad/GOAD/data/${ENV}-config.json"
            "playbooks/data.yml"
        )
        ;;
    security)
        files=(
            "playbooks/security.yml"
            "roles/settings_windows_defender/tasks/main.yml"
            "roles/security_account_is_sensitive/tasks/main.yml"
            "roles/security_powershell_restrict/tasks/main.yml"
            "roles/security_enable_run_as_ppl/tasks/main.yml"
            "${ENV}-inventory"
            "ad/GOAD/data/${ENV}-config.json"
            "playbooks/data.yml"
        )
        ;;
    vulnerabilities)
        files=(
            "playbooks/vulnerabilities.yml"
            "roles/vulns_schedule/tasks/main.yml"
            "roles/vulns_autologon/tasks/main.yml"
            "roles/vulns_openshares/tasks/main.yml"
            "roles/vulns_disable_firewall/tasks/main.yml"
            "roles/vulns_ntlmdowngrade/tasks/main.yml"
            "roles/vulns_enable_credssp_client/tasks/main.yml"
            "roles/vulns_administrator_folder/tasks/main.yml"
            "roles/vulns_acls/tasks/main.yml"
            "roles/vulns_smbv1/tasks/main.yml"
            "roles/vulns_enable_llmnr/tasks/main.yml"
            "roles/vulns_adcs_templates/tasks/main.yml"
            "roles/vulns_adcs_templates/files/ADCSTemplate/ADCSTemplate.psd1"
            "roles/vulns_adcs_templates/files/ADCSTemplate/ADCSTemplate.psm1"
            "roles/vulns_adcs_templates/files/ADCSTemplate/Examples/Tanium.json"
            "roles/vulns_adcs_templates/files/ADCSTemplate/Examples/Demo.ps1"
            "roles/vulns_adcs_templates/files/ADCSTemplate/Examples/Build-ADCS.ps1"
            "roles/vulns_adcs_templates/files/ADCSTemplate/Examples/PowerShellCMS.json"
            "roles/vulns_adcs_templates/files/ADCSTemplate/DSCResources/COMMUNITY_ADCSTemplate/COMMUNITY_ADCSTemplate.psm1"
            "roles/vulns_adcs_templates/files/ADCSTemplate/DSCResources/COMMUNITY_ADCSTemplate/COMMUNITY_ADCSTemplate.schema.mof"
            "roles/vulns_permissions/tasks/main.yml"
            "roles/vulns_enable_nbt_ns/tasks/main.yml"
            "roles/vulns_directory/tasks/main.yml"
            "roles/vulns_files/tasks/main.yml"
            "roles/vulns_enable_credssp_server/tasks/main.yml"
            "roles/vulns_shares/tasks/main.yml"
            "roles/vulns_mssql/tasks/main.yml"
            "roles/vulns_credentials/tasks/main.yml"
            "${ENV}-inventory"
            "ad/GOAD/data/${ENV}-config.json"
            "playbooks/data.yml"
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

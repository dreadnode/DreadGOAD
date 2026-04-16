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
            "ansible/playbooks/build.yml"
            "ansible/roles/common/tasks/main.yml"
            "ansible/roles/settings_keyboard/tasks/main.yml"
            "ansible/roles/settings_no_updates/tasks/main.yml"
            "ansible/roles/settings_updates/tasks/main.yml"
            "ad/GOAD/data/${ENV}-config.json"
            "ansible/playbooks/data.yml"
        )
        ;;
    ad-servers)
        files=(
            "ansible/playbooks/ad-servers.yml"
            "ansible/roles/settings_admin_password/tasks/main.yml"
            "ansible/roles/settings_hostname/tasks/main.yml"
            "ad/GOAD/data/${ENV}-config.json"
            "ansible/playbooks/data.yml"
        )
        ;;
    ad-parent-domain)
        files=(
            "ansible/playbooks/ad-parent_domain.yml"
            "ansible/roles/domain_controller/tasks/main.yml"
            "${ENV}-inventory"
            "ad/GOAD/data/${ENV}-config.json"
            "ansible/playbooks/data.yml"
        )
        ;;
    ad-child-domain)
        files=(
            "ansible/playbooks/ad-child_domain.yml"
            "ansible/roles/child_domain/tasks/main.yml"
            "ansible/roles/dns_conditional_forwarder/tasks/main.yml"
            "ansible/roles/parent_child_dns/tasks/main.yml"
            "${ENV}-inventory"
            "ad/GOAD/data/${ENV}-config.json"
            "ansible/playbooks/data.yml"
        )
        ;;
    ad-members)
        files=(
            "ansible/playbooks/ad-members.yml"
            "ansible/roles/member_server/tasks/main.yml"
            "ansible/roles/commonwkstn/tasks/main.yml"
            "${ENV}-inventory"
            "ad/GOAD/data/${ENV}-config.json"
            "ansible/playbooks/data.yml"
        )
        ;;
    ad-trusts)
        files=(
            "ansible/playbooks/ad-trusts.yml"
            "ansible/roles/settings_disable_nat_adapter/tasks/main.yml"
            "ansible/roles/dns_conditional_forwarder/tasks/main.yml"
            "ansible/roles/trusts/tasks/main.yml"
            "ansible/roles/settings_enable_nat_adapter/tasks/main.yml"
            "ansible/roles/dc_dns_conditional_forwarder/tasks/main.yml"
            "${ENV}-inventory"
            "ad/GOAD/data/${ENV}-config.json"
            "ansible/playbooks/data.yml"
        )
        ;;
    ad-data)
        files=(
            "ansible/playbooks/ad-data.yml"
            "ansible/roles/password_policy/tasks/main.yml"
            "ansible/roles/ad/tasks/main.yml"
            "ansible/roles/ad/tasks/users.yml"
            "ansible/roles/ad/tasks/groups.yml"
            "ansible/roles/ad/tasks/ou.yml"
            "ansible/roles/settings_copy_files/tasks/main.yml"
            "ansible/roles/move_to_ou/tasks/main.yml"
            "${ENV}-inventory"
            "ad/GOAD/data/${ENV}-config.json"
            "ansible/playbooks/data.yml"
        )
        ;;
    ad-gmsa)
        files=(
            "ansible/playbooks/ad-gmsa.yml"
            "ansible/roles/gmsa/tasks/main.yml"
            "ansible/roles/gmsa_hosts/tasks/main.yml"
            "${ENV}-inventory"
            "ad/GOAD/data/${ENV}-config.json"
            "ansible/playbooks/data.yml"
        )
        ;;
    laps)
        files=(
            "ansible/playbooks/laps.yml"
            "ansible/roles/laps_dc/tasks/main.yml"
            "ansible/roles/laps_dc/vars/main.yml"
            "ansible/roles/laps_dc/tasks/move_server_to_ou.yml"
            "ansible/roles/laps_dc/tasks/install.yml"
            "ansible/roles/laps_dc/defaults/main.yml"
            "${ENV}-inventory"
            "ad/GOAD/data/${ENV}-config.json"
            "ansible/playbooks/data.yml"
        )
        ;;
    ad-relations)
        files=(
            "ansible/playbooks/ad-relations.yml"
            "ansible/roles/settings_adjust_rights/tasks/main.yml"
            "ansible/roles/settings_user_rights/tasks/main.yml"
            "ansible/roles/groups_domains/tasks/main.yml"
            "${ENV}-inventory"
            "ad/GOAD/data/${ENV}-config.json"
            "ansible/playbooks/data.yml"
        )
        ;;
    adcs)
        files=(
            "ansible/playbooks/adcs.yml"
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
            "ansible/playbooks/data.yml"
        )
        ;;
    ad-acl)
        files=(
            "ansible/playbooks/ad-acl.yml"
            "ansible/roles/acl/tasks/main.yml"
            "${ENV}-inventory"
            "ad/GOAD/data/${ENV}-config.json"
            "ansible/playbooks/data.yml"
        )
        ;;
    servers)
        files=(
            "ansible/playbooks/servers.yml"
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
            "ansible/playbooks/data.yml"
        )
        ;;
    security)
        files=(
            "ansible/playbooks/security.yml"
            "ansible/roles/settings_windows_defender/tasks/main.yml"
            "ansible/roles/security_account_is_sensitive/tasks/main.yml"
            "ansible/roles/security_powershell_restrict/tasks/main.yml"
            "ansible/roles/security_enable_run_as_ppl/tasks/main.yml"
            "${ENV}-inventory"
            "ad/GOAD/data/${ENV}-config.json"
            "ansible/playbooks/data.yml"
        )
        ;;
    vulnerabilities)
        files=(
            "ansible/playbooks/vulnerabilities.yml"
            "ansible/roles/vulns_schedule/tasks/main.yml"
            "ansible/roles/vulns_autologon/tasks/main.yml"
            "ansible/roles/vulns_openshares/tasks/main.yml"
            "ansible/roles/vulns_disable_firewall/tasks/main.yml"
            "ansible/roles/vulns_ntlmdowngrade/tasks/main.yml"
            "ansible/roles/vulns_enable_credssp_client/tasks/main.yml"
            "ansible/roles/vulns_administrator_folder/tasks/main.yml"
            "ansible/roles/vulns_acls/tasks/main.yml"
            "ansible/roles/vulns_smbv1/tasks/main.yml"
            "ansible/roles/vulns_enable_llmnr/tasks/main.yml"
            "ansible/roles/vulns_adcs_templates/tasks/main.yml"
            "ansible/roles/vulns_adcs_templates/files/ADCSTemplate/ADCSTemplate.psd1"
            "ansible/roles/vulns_adcs_templates/files/ADCSTemplate/ADCSTemplate.psm1"
            "ansible/roles/vulns_adcs_templates/files/ADCSTemplate/Examples/Tanium.json"
            "ansible/roles/vulns_adcs_templates/files/ADCSTemplate/Examples/Demo.ps1"
            "ansible/roles/vulns_adcs_templates/files/ADCSTemplate/Examples/Build-ADCS.ps1"
            "ansible/roles/vulns_adcs_templates/files/ADCSTemplate/Examples/PowerShellCMS.json"
            "ansible/roles/vulns_adcs_templates/files/ADCSTemplate/DSCResources/COMMUNITY_ADCSTemplate/COMMUNITY_ADCSTemplate.psm1"
            "ansible/roles/vulns_adcs_templates/files/ADCSTemplate/DSCResources/COMMUNITY_ADCSTemplate/COMMUNITY_ADCSTemplate.schema.mof"
            "ansible/roles/vulns_permissions/tasks/main.yml"
            "ansible/roles/vulns_enable_nbt_ns/tasks/main.yml"
            "ansible/roles/vulns_directory/tasks/main.yml"
            "ansible/roles/vulns_files/tasks/main.yml"
            "ansible/roles/vulns_enable_credssp_server/tasks/main.yml"
            "ansible/roles/vulns_shares/tasks/main.yml"
            "ansible/roles/vulns_mssql/tasks/main.yml"
            "ansible/roles/vulns_credentials/tasks/main.yml"
            "${ENV}-inventory"
            "ad/GOAD/data/${ENV}-config.json"
            "ansible/playbooks/data.yml"
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

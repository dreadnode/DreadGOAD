#!/usr/bin/env bash
set -e 

ENV=dev

build_play() {
    for file in ansible/build.yml \
                ansible/roles/common/tasks/main.yml \
                ansible/roles/settings/keyboard/tasks/main.yml \
                ansible/roles/settings/no_updates/tasks/main.yml \
                ansible/roles/settings/updates/tasks/default.yml \
                ad/GOAD/data/config.json \
                ansible/data.yml; do
    echo -e "\n==== $file ===="
    cat "$file"
    done
}

ad_servers_play() {
    for file in ansible/ad-servers.yml \
                ansible/roles/settings/admin_password/tasks/main.yml \
                ansible/roles/settings/hostname/tasks/main.yml \
                ad/GOAD/data/config.json \
                ansible/data.yml; do
    echo -e "\n==== $file ===="
    cat "$file"
    done
}

ad_parent_domain_play() {
    for file in ansible/ad-parent_domain.yml \
                ansible/roles/domain_controller/tasks/main.yml \
                $ENV-inventory \
                ad/GOAD/data/config.json \
                ansible/data.yml; do
    echo -e "\n==== $file ===="
    cat "$file"
    done
}

ad_child_domain_play() {
    for file in ansible/ad-child_domain.yml \
                ansible/roles/child_domain/tasks/main.yml \
                ansible/roles/dns_conditional_forwarder/tasks/main.yml \
                ansible/roles/parent_child_dns/tasks/main.yml \
                $ENV-inventory \
                ad/GOAD/data/config.json \
                ansible/data.yml; do
    echo -e "\n==== $file ===="
    cat "$file"
    done
}

ad_members_play() {
    for file in ansible/ad-members.yml \
                ansible/roles/member_server/tasks/main.yml \
                ansible/roles/commonwkstn/tasks/main.yml \
                $ENV-inventory \
                ad/GOAD/data/config.json \
                ansible/data.yml; do
    echo -e "\n==== $file ===="
    cat "$file"
    done
}

ad_trusts_play() {
    for file in ansible/ad-trusts.yml \
                ansible/roles/settings/disable_nat_adapter/tasks/main.yml \
                ansible/roles/dns_conditional_forwarder/tasks/main.yml \
                ansible/roles/trusts/tasks/main.yml \
                ansible/roles/settings/enable_nat_adapter/tasks/main.yml \
                ansible/roles/dc_dns_conditional_forwarder/tasks/main.yml \
                $ENV-inventory \
                ad/GOAD/data/config.json \
                ansible/data.yml; do
    echo -e "\n==== $file ===="
    cat "$file"
    done
}

ad_data_play() {
    for file in ansible/ad-data.yml \
                ansible/roles/password_policy/tasks/main.yml \
                ansible/roles/ad/tasks/main.yml \
                ansible/roles/ad/tasks/users.yml \
                ansible/roles/ad/tasks/groups.yml \
                ansible/roles/ad/tasks/ou.yml \
                ansible/roles/settings/copy_files/tasks/main.yml \
                ansible/roles/move_to_ou/tasks/main.yml \
                $ENV-inventory \
                ad/GOAD/data/config.json \
                ansible/data.yml; do
    echo -e "\n==== $file ===="
    cat "$file"
    done
}

ad_gmsa_play() {
    for file in ansible/ad-gmsa.yml \
                ansible/roles/gmsa/tasks/main.yml \
                ansible/roles/gmsa_hosts/tasks/main.yml \
                $ENV-inventory \
                ad/GOAD/data/config.json \
                ansible/data.yml; do
    echo -e "\n==== $file ===="
    cat "$file"
    done
}

laps_play() {
    for file in ansible/laps.yml \
                ansible/roles/laps/dc/tasks/main.yml \
                ansible/roles/laps/dc/vars/main.yml \
                ansible/roles/laps/dc/tasks/move_server_to_ou.yml \
                ansible/roles/laps/dc/tasks/install.yml \
                ansible/roles/laps/dc/defaults/main.yml \
                $ENV-inventory \
                ad/GOAD/data/config.json \
                ansible/data.yml; do
    echo -e "\n==== $file ===="
    cat "$file"
    done
}

ad_relations_play() {
    for file in ansible/ad-relations.yml \
                ansible/roles/settings/adjust_rights/tasks/main.yml \
                ansible/roles/settings/user_rights/tasks/main.yml \
                ansible/roles/groups_domains/tasks/main.yml \
                $ENV-inventory \
                ad/GOAD/data/config.json \
                ansible/data.yml; do
    echo -e "\n==== $file ===="
    cat "$file"
    done
}

adcs_play() {
    for file in ansible/adcs.yml \
                ansible/roles/adcs/tasks/main.yml \
                ansible/roles/adcs_templates/tasks/main.yml \
                ansible/roles/adcs_templates/files/ESC1.json \
                ansible/roles/adcs_templates/files/ESC2.json \
                ansible/roles/adcs_templates/files/ESC3-CRA.json \
                ansible/roles/adcs_templates/files/ESC3.json \
                ansible/roles/adcs_templates/files/ESC4.json \
                ansible/roles/adcs_templates/files/ADCSTemplate/ADCSTemplate.psd1 \
                ansible/roles/adcs_templates/files/ADCSTemplate/DSCResources/COMMUNITY_ADCSTemplate/COMMUNITY_ADCSTemplate.psm1 \
                ansible/roles/adcs_templates/files/ADCSTemplate/DSCResources/COMMUNITY_ADCSTemplate/COMMUNITY_ADCSTemplate.schema.mof \
                ansible/roles/adcs_templates/files/ADCSTemplate/ADCSTemplate.psm1 \
                $ENV-inventory \
                ad/GOAD/data/config.json \
                ansible/data.yml; do
    echo -e "\n==== $file ===="
    cat "$file"
    done
}

ad_acl_play () {
    for file in ansible/ad-acl.yml \
                ansible/roles/acl/tasks/main.yml \
                $ENV-inventory \
                ad/GOAD/data/config.json \
                ansible/data.yml; do
    echo -e "\n==== $file ===="
    cat "$file"
    done 
}

servers_play() {
    for file in ansible/servers.yml \
                ansible/roles/iis/tasks/main.yml \
                ansible/roles/iis/files/index.html \
                ansible/roles/mssql/tasks/main.yml \
                ansible/roles/mssql/defaults/main.yml \
                ansible/roles/mssql/files/sql_conf.ini.MSSQL_2019.j2 \
                ansible/roles/mssql/files/sql_conf.ini.MSSQL_2022.j2 \
                ansible/roles/mssql_link/tasks/main.yml \
                ansible/roles/mssql_link/tasks/logins.yml \
                ansible/roles/mssql_ssms/tasks/main.yml \
                ansible/roles/webdav/tasks/main.yml \
                $ENV-inventory \
                ad/GOAD/data/config.json \
                ansible/data.yml; do
    echo -e "\n==== $file ===="
    cat "$file"
    done 
}

vulnerabilities_play() {
    for file in ansible/vulnerabilities.yml \
                ansible/roles/vulns/schedule/tasks/main.yml \
                ansible/roles/vulns/autologon/tasks/main.yml \
                ansible/roles/vulns/openshares/tasks/main.yml \
                ansible/roles/vulns/disable_firewall/tasks/main.yml \
                ansible/roles/vulns/ntlmdowngrade/tasks/main.yml \
                ansible/roles/vulns/enable_credssp_client/tasks/main.yml \
                ansible/roles/vulns/administrator_folder/tasks/main.yml \
                ansible/roles/vulns/acls/tasks/main.yml \
                ansible/roles/vulns/smbv1/tasks/main.yml \
                ansible/roles/vulns/enable_llmnr/tasks/main.yml \
                ansible/roles/vulns/adcs_templates/tasks/main.yml \
                ansible/roles/vulns/adcs_templates/files/ADCSTemplate/ADCSTemplate.psd1 \
                ansible/roles/vulns/adcs_templates/files/ADCSTemplate/ADCSTemplate.psm1 \
                ansible/roles/vulns/adcs_templates/files/ADCSTemplate/Examples/Tanium.json \
                ansible/roles/vulns/adcs_templates/files/ADCSTemplate/Examples/Demo.ps1 \
                ansible/roles/vulns/adcs_templates/files/ADCSTemplate/Examples/Build-ADCS.ps1 \
                ansible/roles/vulns/adcs_templates/files/ADCSTemplate/Examples/PowerShellCMS.json \
                ansible/roles/vulns/adcs_templates/files/ADCSTemplate/DSCResources/COMMUNITY_ADCSTemplate/COMMUNITY_ADCSTemplate.psm1 \
                ansible/roles/vulns/adcs_templates/files/ADCSTemplate/DSCResources/COMMUNITY_ADCSTemplate/COMMUNITY_ADCSTemplate.schema.mof \
                ansible/roles/vulns/permissions/tasks/main.yml \
                ansible/roles/vulns/enable_nbt-ns/tasks/main.yml \
                ansible/roles/vulns/directory/tasks/main.yml \
                ansible/roles/vulns/files/tasks/main.yml \
                ansible/roles/vulns/enable_credssp_server/tasks/main.yml \
                ansible/roles/vulns/shares/tasks/main.yml \
                ansible/roles/vulns/mssql/tasks/main.yml \
                ansible/roles/vulns/credentials/tasks/main.yml \
                $ENV-inventory \
                ad/GOAD/data/config.json \
                ansible/data.yml; do
    echo -e "\n==== $file ===="
    cat "$file"
    done 

}

# build_play
# ad_servers_play
# ad_parent_domain_play
# ad_child_domain_play
# ad_members_play
ad_trusts_play
# ad_data_play
# ad_gmsa_play
# laps_play
# ad_relations_play
# adcs_play
# ad_acl_play
# servers_play
# vulnerabilities_play
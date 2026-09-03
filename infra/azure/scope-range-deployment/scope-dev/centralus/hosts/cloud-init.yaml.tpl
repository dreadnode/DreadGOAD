#cloud-config
fqdn: ${hostname}.range.test
manage_etc_hosts: true
package_update: true
packages:
  - ca-certificates
  - curl
  - jq
  - python3
  - python3-apt
  - unzip
write_files:
  - path: /etc/range-hosts
    permissions: "0644"
    content: |
      10.50.10.10 kali01 kali01.range.test
      10.50.10.20 web01 web01.range.test wordpress.range.test cloud.range.test
      10.50.10.30 data01 data01.range.test postgres.range.test mariadb.range.test mongodb.range.test redis.range.test
      10.50.10.40 dev01 dev01.range.test git.range.test jenkins.range.test registry.range.test
      10.50.10.50 storage01 storage01.range.test s3.range.test files.range.test
      10.50.10.60 services01 services01.range.test dns.range.test ldap.range.test mq.range.test mail.range.test
runcmd:
  - [sh, -c, "grep -q 'BEGIN SCOPE-RANGE' /etc/hosts || { printf '\\n# BEGIN SCOPE-RANGE\\n' >> /etc/hosts; cat /etc/range-hosts >> /etc/hosts; printf '# END SCOPE-RANGE\\n' >> /etc/hosts; }"]

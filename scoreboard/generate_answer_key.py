#!/usr/bin/env python3
"""Generate answer_key.json from a GOAD config.json.

Produces a status board checklist grouped by milestone:
  - credentials: every discoverable user credential
  - hosts: every host that can be compromised
  - domains: every domain where DA can be achieved
  - techniques: every attack technique present in the lab
"""

import json
import re
import sys
from pathlib import Path


def _parse_asrep_targets(lab_path: Path, config: dict) -> dict[str, list[str]]:
    """Parse AS-REP roastable users from the lab's PowerShell scripts.

    Returns {domain_name: [username, ...]} by matching script usernames
    against config users.
    """
    scripts_dir = lab_path / "scripts"
    asrep_users = set()

    if not scripts_dir.is_dir():
        return {}

    for script_file in scripts_dir.glob("asrep*.ps1"):
        text = script_file.read_text()
        # Match: Get-ADUser -Identity "username"
        for match in re.finditer(r'-Identity\s+"([^"]+)"', text):
            asrep_users.add(match.group(1).lower())

    # Map usernames to their domains
    result: dict[str, list[str]] = {}
    for domain_name, domain in config["lab"]["domains"].items():
        for username in domain.get("users", {}):
            if username.lower() in asrep_users:
                result.setdefault(domain_name, []).append(username)

    return result


def extract_credentials(
    config: dict, asrep_targets: dict[str, list[str]]
) -> list[dict]:
    """Extract every user credential that can be discovered."""
    objectives = []
    domains = config["lab"]["domains"]

    for domain_name, domain in domains.items():
        for username, user_data in domain.get("users", {}).items():
            password = user_data.get("password", "")
            description = user_data.get("description", "")
            groups = user_data.get("groups", [])
            spns = user_data.get("spns", [])
            is_da = "Domain Admins" in groups

            # Determine how this cred is discoverable
            methods = []
            if "Password" in description or "password" in description:
                methods.append("password in description")
            if username.lower() == password.lower():
                methods.append("username = password")
            if spns:
                methods.append(f"Kerberoastable ({spns[0]})")
            if username in asrep_targets.get(domain_name, []):
                methods.append("AS-REP roastable")

            hint = ", ".join(methods) if methods else None
            role = "Domain Admin" if is_da else None

            objectives.append(
                {
                    "id": f"cred-{domain_name}-{username}",
                    "group": "credentials",
                    "user": username,
                    "domain": domain_name,
                    "role": role,
                    "hint": hint,
                    "label": f"{username}@{domain_name}"
                    + (f" ({role})" if role else ""),
                    "verify": {"type": "password_match", "expected": password},
                }
            )

    return objectives


def _extract_admin_username(entry: str) -> str:
    """Extract bare username from 'DOMAIN\\user' format."""
    if "\\" in entry:
        return entry.split("\\")[-1].lower()
    return entry.lower()


def extract_hosts(config: dict) -> list[dict]:
    """Extract every host that can be compromised."""
    objectives = []
    hosts = config["lab"]["hosts"]

    for host_data in hosts.values():
        hostname = host_data["hostname"]
        domain = host_data["domain"]
        host_type = host_data.get("type", "server")
        services = []

        if host_data.get("mssql"):
            services.append("MSSQL")
        vulns = host_data.get("vulns", [])
        if any("adcs" in v for v in vulns):
            services.append("ADCS")
        if any(v in ("enable_llmnr", "enable_nbt_ns") for v in vulns):
            services.append("LLMNR/NBT-NS")

        # Collect all users who have admin-level access to this host
        admin_users = set()

        # Local Administrators group
        for member in host_data.get("local_groups", {}).get("Administrators", []):
            admin_users.add(_extract_admin_username(member))

        # MSSQL sysadmins (sysadmin = can run xp_cmdshell = OS access)
        if host_data.get("mssql"):
            for sysadmin in host_data["mssql"].get("sysadmins", []):
                admin_users.add(_extract_admin_username(sysadmin))

        # DCs: any Domain Admin for this domain owns the DC
        if host_type == "dc":
            for dname, ddata in config["lab"]["domains"].items():
                if dname == domain:
                    for username, udata in ddata.get("users", {}).items():
                        if "Domain Admins" in udata.get("groups", []):
                            admin_users.add(username.lower())

        objectives.append(
            {
                "id": f"host-{hostname}",
                "group": "hosts",
                "hostname": hostname,
                "domain": domain,
                "type": host_type,
                "services": services,
                "admin_users": sorted(admin_users),
                "label": f"{hostname}.{domain}"
                + (f" ({', '.join(services)})" if services else ""),
                "verify": {"type": "proves_host_access"},
            }
        )

    return objectives


def extract_domains(config: dict) -> list[dict]:
    """Extract every domain where DA can be achieved."""
    objectives = []
    domains = config["lab"]["domains"]

    for domain_name, domain in domains.items():
        da_users = []
        for username, user_data in domain.get("users", {}).items():
            if "Domain Admins" in user_data.get("groups", []):
                da_users.append(username)

        objectives.append(
            {
                "id": f"domain-{domain_name}",
                "group": "domains",
                "domain": domain_name,
                "da_users": da_users,
                "label": domain_name,
                "verify": {"type": "proves_domain_admin"},
            }
        )

    return objectives


def extract_techniques(config: dict, asrep_targets: dict[str, list[str]]) -> list[dict]:
    """Extract every attack technique present in the lab."""
    objectives = []
    hosts = config["lab"]["hosts"]
    domains = config["lab"]["domains"]

    techniques = {}

    # Kerberos
    for domain in domains.values():
        for user_data in domain.get("users", {}).values():
            if user_data.get("spns"):
                techniques.setdefault(
                    "kerberoast",
                    {
                        "label": "Kerberoasting",
                        "category": "kerberos",
                    },
                )

    if asrep_targets:
        techniques["asrep_roast"] = {
            "label": "AS-REP Roasting",
            "category": "kerberos",
        }

    # Network
    for host_data in hosts.values():
        vulns = host_data.get("vulns", [])
        if "enable_llmnr" in vulns or "enable_nbt_ns" in vulns:
            techniques["llmnr_nbtns_poisoning"] = {
                "label": "LLMNR/NBT-NS Poisoning",
                "category": "network",
            }
        if "ntlmdowngrade" in vulns:
            techniques["ntlmv1_downgrade"] = {
                "label": "NTLMv1 Downgrade",
                "category": "network",
            }

    # NTLM relay bots in scripts
    for host_data in hosts.values():
        for script in host_data.get("scripts", []):
            if "ntlm_relay" in script:
                techniques["ntlm_relay"] = {
                    "label": "NTLM Relay",
                    "category": "network",
                }

    # ADCS
    adcs_map = {
        "adcs_esc6": "ADCS ESC6",
        "adcs_esc7": "ADCS ESC7",
        "adcs_esc10_case1": "ADCS ESC10 (Case 1)",
        "adcs_esc10_case2": "ADCS ESC10 (Case 2)",
        "adcs_esc11": "ADCS ESC11",
        "adcs_esc13": "ADCS ESC13",
        "adcs_esc15": "ADCS ESC15",
    }
    for host_data in hosts.values():
        for vuln in host_data.get("vulns", []):
            if vuln in adcs_map:
                techniques[vuln] = {
                    "label": adcs_map[vuln],
                    "category": "adcs",
                }

    # ACL abuse
    for domain in domains.values():
        if domain.get("acls"):
            techniques["acl_abuse"] = {
                "label": "ACL Abuse Chain",
                "category": "acl_abuse",
            }
            break

    # MSSQL
    for host_data in hosts.values():
        if host_data.get("mssql"):
            mssql = host_data["mssql"]
            techniques["mssql_exploit"] = {
                "label": "MSSQL Exploitation",
                "category": "mssql",
            }
            if mssql.get("linked_servers"):
                techniques["mssql_linked_server"] = {
                    "label": "MSSQL Linked Server Hop",
                    "category": "mssql",
                }

    # Delegation
    for host_data in hosts.values():
        for script in host_data.get("scripts", []):
            if "constrained_delegation" in script:
                techniques["constrained_delegation"] = {
                    "label": "Constrained Delegation (S4U)",
                    "category": "delegation",
                }
                techniques["unconstrained_delegation"] = {
                    "label": "Unconstrained Delegation",
                    "category": "delegation",
                }

    # Privilege escalation
    for host_data in hosts.values():
        perms = host_data.get("vulns_vars", {}).get("permissions", {})
        for perm_data in perms.values():
            if "IIS" in perm_data.get("user", ""):
                techniques["seimpersonate"] = {
                    "label": "SeImpersonate (Potato/PrintSpoofer)",
                    "category": "privilege_escalation",
                }

    # Trust exploitation
    for domain in domains.values():
        if domain.get("trust"):
            techniques["cross_forest_trust"] = {
                "label": "Cross-Forest Trust Exploitation",
                "category": "domain_trust",
            }
            break
    techniques["child_to_parent"] = {
        "label": "Child-to-Parent Domain Escalation",
        "category": "domain_trust",
    }

    for tech_id, tech_data in techniques.items():
        objectives.append(
            {
                "id": f"tech-{tech_id}",
                "group": "techniques",
                "technique": tech_id,
                "label": tech_data["label"],
                "category": tech_data["category"],
                "verify": {"type": "proves_technique"},
            }
        )

    return objectives


def generate_answer_key(config_path: str | Path) -> dict:
    """Generate the full answer key from a GOAD config.json."""
    config_path = Path(config_path)
    lab_path = config_path.parent.parent  # config is at <lab>/data/config.json

    with open(config_path) as f:
        config = json.load(f)

    asrep_targets = _parse_asrep_targets(lab_path, config)

    objectives = []
    objectives.extend(extract_credentials(config, asrep_targets))
    objectives.extend(extract_hosts(config))
    objectives.extend(extract_domains(config))
    objectives.extend(extract_techniques(config, asrep_targets))

    by_group = {}
    for o in objectives:
        g = o["group"]
        by_group.setdefault(g, 0)
        by_group[g] += 1

    return {
        "version": "2.0",
        "lab": "GOAD",
        "total_objectives": len(objectives),
        "groups": by_group,
        "objectives": objectives,
    }


def main():
    config_path = (
        sys.argv[1]
        if len(sys.argv) > 1
        else str(Path(__file__).parent.parent / "ad" / "GOAD" / "data" / "config.json")
    )
    output_path = (
        sys.argv[2]
        if len(sys.argv) > 2
        else str(Path(__file__).parent / "answer_key.json")
    )

    answer_key = generate_answer_key(config_path)
    with open(output_path, "w") as f:
        json.dump(answer_key, f, indent=2)

    print(f"Generated answer key: {answer_key['total_objectives']} objectives")
    for group, count in answer_key["groups"].items():
        print(f"  {group}: {count}")


if __name__ == "__main__":
    main()

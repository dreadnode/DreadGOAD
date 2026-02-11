#!/usr/bin/env python3
"""
GOAD Variant Generator

Creates a graph-isomorphic copy of GOAD with randomized names while preserving
structure, relationships, vulnerabilities, and attack paths.
"""

from __future__ import annotations

import json
import re
import secrets
import shutil
from pathlib import Path
from typing import List, Tuple
import argparse

# Support both package import and direct script execution
try:
    from .name_generator import NameGenerator
except ImportError:
    from name_generator import NameGenerator


class GOADVariantGenerator:
    """Generate GOAD variants with randomized entity names."""

    def __init__(self, source_path: str, target_path: str, variant_name: str = "variant-1") -> None:
        self.source_path = Path(source_path)
        self.target_path = Path(target_path)
        self.variant_name = variant_name
        self.name_gen = NameGenerator()

        # Mappings for all entity types
        self.mappings = {
            "domains": {},
            "netbios": {},
            "hosts": {},
            "users": {},
            "passwords": {},
            "groups": {},
            "ous": {},
            "acls": {},
            "misc": {}
        }

        # Ordered replacement list (longest first to avoid substring issues)
        self.replacements: List[Tuple[str, str]] = []

        # Structured password lookup: new_username -> new_password
        # Built during map_passwords() to fix collisions where a password
        # string equals a username (e.g., hodor/hodor) and global text
        # replacement would corrupt the password field.
        self.user_password_map: dict[str, str] = {}

        # Track preserved names (service accounts, etc.)
        self.preserved_users = {"sql_svc"}

    def load_config(self) -> dict:
        """Load the main GOAD config.json file."""
        config_path = self.source_path / "data" / "config.json"
        with open(config_path, 'r') as f:
            return json.load(f)

    def save_mappings(self) -> None:
        """Save mappings to JSON file for reference."""
        output_path = self.target_path / "mapping.json"
        with open(output_path, 'w') as f:
            json.dump(self.mappings, f, indent=2)
        print(f"Mappings saved to {output_path}")

    def map_domains(self, config: dict) -> None:
        """Map domain names and NetBIOS names."""
        # Identify domain hierarchy
        # Root: sevenkingdoms.local
        # Child: north.sevenkingdoms.local
        # External: essos.local

        root_domain = "sevenkingdoms.local"
        child_domain = "north.sevenkingdoms.local"
        external_domain = "essos.local"

        # Generate new domain names
        root_new = self.name_gen.generate_domain_name()
        root_full = f"{root_new}.local"

        child_prefix = self.name_gen.generate_subdomain_name()
        child_full = f"{child_prefix}.{root_full}"

        external_new = self.name_gen.generate_domain_name()
        external_full = f"{external_new}.local"

        # Map domains
        self.mappings['domains'][root_domain] = root_full
        self.mappings['domains'][child_domain] = child_full
        self.mappings['domains'][external_domain] = external_full

        # Map NetBIOS names
        self.mappings['netbios']['SEVENKINGDOMS'] = root_new.upper()
        self.mappings['netbios']['NORTH'] = child_prefix.upper()
        self.mappings['netbios']['ESSOS'] = external_new.upper()

        # Also map lowercase and capitalized versions for DN paths
        self.mappings['netbios']['sevenkingdoms'] = root_new.lower()
        self.mappings['netbios']['north'] = child_prefix.lower()
        self.mappings['netbios']['essos'] = external_new.lower()
        self.mappings['netbios']['Sevenkingdoms'] = root_new.capitalize()
        self.mappings['netbios']['North'] = child_prefix.capitalize()
        self.mappings['netbios']['Essos'] = external_new.capitalize()

        print(f"Domain mappings:")
        print(f"  {root_domain} -> {root_full}")
        print(f"  {child_domain} -> {child_full}")
        print(f"  {external_domain} -> {external_full}")

    # Known typos/aliases in upstream GOAD source that differ from canonical hostnames
    HOSTNAME_ALIASES = {
        "braavos": ["Bravos"],   # responder.ps1 uses \\Bravos\private
        "meereen": ["Meren"],    # ntlm_relay.ps1 uses \\Meren\Private
    }

    def map_hosts(self, config: dict) -> None:
        """Map host identifiers and hostnames."""
        hosts = config['lab']['hosts']

        for host_id, host_info in hosts.items():
            old_hostname = host_info['hostname']
            new_hostname = self.name_gen.generate_hostname()

            old_domain = host_info['domain']
            new_domain = self.mappings['domains'][old_domain]

            old_fqdn = f"{old_hostname}.{old_domain}"
            new_fqdn = f"{new_hostname}.{new_domain}"

            self.mappings['hosts'][host_id] = {
                "old_hostname": old_hostname,
                "new_hostname": new_hostname,
                "old_fqdn": old_fqdn,
                "new_fqdn": new_fqdn,
                "old_domain": old_domain,
                "new_domain": new_domain
            }

            # Also map computer accounts (hostname$)
            self.mappings['misc'][f"{old_hostname}$"] = f"{new_hostname}$"

            # Map uppercase version (for linked server names, CA servers, etc.)
            self.mappings['misc'][old_hostname.upper()] = new_hostname.upper()

            # Map capitalized version (for city names, descriptions, etc.)
            self.mappings['misc'][old_hostname.capitalize()] = new_hostname.capitalize()

            # Map known typos/aliases from upstream GOAD source
            for alias in self.HOSTNAME_ALIASES.get(old_hostname, []):
                self.mappings['misc'][alias] = new_hostname.capitalize()

            print(f"  {host_id}: {old_hostname} -> {new_hostname}")

    def map_users(self, config: dict) -> None:
        """Map usernames and their firstname/surname components."""
        domains = config['lab']['domains']

        for _, domain_info in domains.items():
            users = domain_info.get('users', {})

            for username, user_info in users.items():
                # Preserve service accounts
                if username in self.preserved_users:
                    self.mappings['users'][username] = username
                    print(f"  {username} -> {username} (preserved)")
                    continue

                # Generate new username
                new_username = self.name_gen.generate_username()
                self.mappings['users'][username] = new_username

                # Map firstname and surname separately (handle case variations)
                if 'firstname' in user_info:
                    firstname = user_info['firstname']
                    new_firstname = new_username.split('.')[0]

                    # Map exact case
                    self.mappings['misc'][firstname] = new_firstname

                    # Also map lowercase version (for file paths, etc.)
                    if not firstname.islower() and firstname != 'sql':
                        self.mappings['misc'][firstname.lower()] = new_firstname.lower()

                    # Also map capitalized version if original is lowercase
                    if firstname.islower() and firstname != 'sql':
                        self.mappings['misc'][firstname.capitalize()] = new_firstname.capitalize()

                if 'surname' in user_info:
                    surname = user_info['surname']
                    if surname != '-':  # Don't map placeholder surnames
                        new_surname = new_username.split('.')[1] if '.' in new_username else new_username

                        # Map exact case
                        self.mappings['misc'][surname] = new_surname

                        # Also map capitalized version if original is lowercase
                        if surname.islower():
                            self.mappings['misc'][surname.capitalize()] = new_surname.capitalize()

                print(f"  {username} -> {new_username}")

    def map_groups(self, config: dict) -> None:
        """Map group names."""
        domains = config['lab']['domains']

        for _, domain_info in domains.items():
            groups_section = domain_info.get('groups', {})

            for group_type in ['universal', 'global', 'domainlocal']:
                groups = groups_section.get(group_type, {})

                for group_name in groups.keys():
                    # Skip built-in groups
                    if group_name in ['Domain Admins', 'Protected Users']:
                        self.mappings['groups'][group_name] = group_name
                        continue

                    new_group_name = self.name_gen.generate_group_name()
                    self.mappings['groups'][group_name] = new_group_name
                    print(f"  {group_name} -> {new_group_name}")

    def map_ous(self, config: dict) -> None:
        """Map organizational unit names."""
        domains = config['lab']['domains']

        for _, domain_info in domains.items():
            ous = domain_info.get('organisation_units', {})

            for ou_name in ous.keys():
                new_ou_name = self.name_gen.generate_ou_name()
                self.mappings['ous'][ou_name] = new_ou_name
                print(f"  {ou_name} -> {new_ou_name}")

    def map_cities(self, config: dict) -> None:
        """Map city names in user records."""
        domains = config['lab']['domains']
        cities = set()

        # Collect all unique cities
        for _, domain_info in domains.items():
            users = domain_info.get('users', {})
            for _, user_info in users.items():
                city = user_info.get('city', '')
                if city and city != '-':
                    cities.add(city)

        # Map to generic city names
        city_names = [
            'Boston', 'Chicago', 'Dallas', 'Denver', 'Houston',
            'Phoenix', 'Seattle', 'Portland', 'Austin', 'Atlanta',
            'Miami', 'Philadelphia', 'San Diego', 'San Francisco', 'New York'
        ]

        used_cities = set()
        for old_city in sorted(cities):
            # Pick a unique city
            available = [c for c in city_names if c not in used_cities]
            if available:
                new_city = secrets.choice(available)
                used_cities.add(new_city)
            else:
                # Fallback if we run out
                new_city = f"City{len(used_cities) + 1}"

            self.mappings['misc'][old_city] = new_city
            print(f"  {old_city} -> {new_city}")

    def map_passwords(self, config: dict) -> None:
        """Map passwords with equivalent complexity."""
        domains = config['lab']['domains']
        hosts = config['lab']['hosts']

        # Collect all unique passwords
        passwords = set()

        # Domain passwords
        for _, domain_info in domains.items():
            if 'domain_password' in domain_info:
                passwords.add(domain_info['domain_password'])

        # User passwords
        for _, domain_info in domains.items():
            users = domain_info.get('users', {})
            for username, user_info in users.items():
                if 'password' in user_info:
                    passwords.add(user_info['password'])

        # Host local admin passwords
        for host_id, host_info in hosts.items():
            if 'local_admin_password' in host_info:
                passwords.add(host_info['local_admin_password'])

        # MSSQL passwords
        for host_id, host_info in hosts.items():
            if 'mssql' in host_info:
                mssql = host_info['mssql']
                if 'sa_password' in mssql:
                    passwords.add(mssql['sa_password'])

                # Linked server passwords
                if 'linked_servers' in mssql:
                    for ls_name, ls_info in mssql['linked_servers'].items():
                        for mapping in ls_info.get('users_mapping', []):
                            if 'remote_password' in mapping:
                                passwords.add(mapping['remote_password'])

        # Vulnerability-specific passwords
        for host_id, host_info in hosts.items():
            if 'vulns_vars' in host_info:
                vulns_vars = host_info['vulns_vars']
                if 'credentials' in vulns_vars:
                    for cred_key, cred_info in vulns_vars['credentials'].items():
                        if 'secret' in cred_info:
                            passwords.add(cred_info['secret'])
                        if 'runas_password' in cred_info:
                            passwords.add(cred_info['runas_password'])
                if 'autologon' in vulns_vars:
                    for auto_key, auto_info in vulns_vars['autologon'].items():
                        if 'password' in auto_info:
                            passwords.add(auto_info['password'])

        # Generate new passwords
        for password in passwords:
            new_password = self.name_gen.generate_password(password)
            self.mappings['passwords'][password] = new_password
            print(f"  {password[:20]}... -> {new_password[:20]}...")

        # Build structured user→password lookup so fix_passwords() can
        # correct collisions where global text replacement corrupts
        # password fields (e.g., password "hodor" replaced as username).
        for _, domain_info in domains.items():
            users = domain_info.get('users', {})
            for username, user_info in users.items():
                if 'password' in user_info:
                    new_username = self.mappings['users'].get(username, username)
                    old_password = user_info['password']
                    new_password = self.mappings['passwords'].get(old_password, old_password)
                    self.user_password_map[new_username] = new_password

    def map_gmsa_accounts(self, config: dict) -> None:
        """Map gMSA account names."""
        domains = config['lab']['domains']

        for _, domain_info in domains.items():
            gmsa_section = domain_info.get('gmsa', {})

            for _, gmsa_info in gmsa_section.items():
                if 'gMSA_Name' in gmsa_info:
                    old_gmsa_name = gmsa_info['gMSA_Name']
                    new_gmsa_name = self.name_gen.generate_gmsa_name()

                    # Map the base name
                    self.mappings['misc'][old_gmsa_name] = new_gmsa_name

                    # Map the account name with $ suffix
                    self.mappings['misc'][f"{old_gmsa_name}$"] = f"{new_gmsa_name}$"

                    print(f"  {old_gmsa_name} -> {new_gmsa_name}")

    def map_acls(self) -> None:
        """Map ACL identifiers by parsing and reconstructing."""
        # ACL identifiers like "GenericAll_khal_viserys" need to be reconstructed
        # with new usernames
        pass  # Will be handled during replacement building

    def generate_mappings(self) -> None:
        """Extract entities and create all mappings."""
        print("\n=== Generating Mappings ===\n")

        config = self.load_config()

        print("\nMapping domains...")
        self.map_domains(config)

        print("\nMapping hosts...")
        self.map_hosts(config)

        print("\nMapping users...")
        self.map_users(config)

        print("\nMapping groups...")
        self.map_groups(config)

        print("\nMapping OUs...")
        self.map_ous(config)

        print("\nMapping passwords...")
        self.map_passwords(config)

        print("\nMapping gMSA accounts...")
        self.map_gmsa_accounts(config)

        print("\nMapping cities...")
        self.map_cities(config)

        print("\n=== Mapping Generation Complete ===\n")

    def build_ordered_replacements(self) -> None:
        """
        Build ordered list of replacements.

        Order (longest first):
        1. FQDNs
        2. Domain-qualified usernames (DOMAIN\\user and domain.local\\user)
        3. DN paths
        4. SPNs
        5. Computer accounts with $
        6. Domain names (child before parent)
        7. Usernames
        8. Groups
        9. OUs
        10. Passwords
        11. NetBIOS names
        """
        print("\n=== Building Ordered Replacements ===\n")

        replacements = []

        # 1. Build FQDNs for all hosts (longest first)
        for host_id, host_map in self.mappings['hosts'].items():
            replacements.append((host_map['old_fqdn'], host_map['new_fqdn']))

        # 1b. Add bare hostnames (for hostname fields and SPNs)
        for host_id, host_map in self.mappings['hosts'].items():
            replacements.append((host_map['old_hostname'], host_map['new_hostname']))

        # 2. Domain-qualified usernames
        # Format: DOMAIN\\username and domain.local\\username
        for old_domain, new_domain in self.mappings['domains'].items():
            old_netbios = old_domain.split('.')[0].upper()
            new_netbios_parts = new_domain.split('.')[0].split('.')
            if len(new_netbios_parts) > 1:
                # Handle child domains like "ops.zenithcorp.local"
                new_netbios = new_netbios_parts[-2].upper()
            else:
                new_netbios = new_domain.split('.')[0].upper()

            for old_user, new_user in self.mappings['users'].items():
                # DOMAIN\\username format
                replacements.append((
                    f"{old_netbios}\\\\{old_user}",
                    f"{new_netbios}\\\\{new_user}"
                ))
                # domain.local\\username format
                replacements.append((
                    f"{old_domain}\\\\{old_user}",
                    f"{new_domain}\\\\{new_user}"
                ))

        # 3. DN paths - need to handle DC= components
        # Map domain components in DN paths
        for old_domain, new_domain in self.mappings['domains'].items():
            old_parts = old_domain.replace('.local', '').split('.')
            new_parts = new_domain.replace('.local', '').split('.')

            # Build full DN path
            old_dn = ','.join([f"DC={part}" for part in old_parts]) + ",DC=local"
            new_dn = ','.join([f"DC={part}" for part in new_parts]) + ",DC=local"

            replacements.append((old_dn, new_dn))

        # 4. SPNs - will be captured by FQDN replacements mostly
        # Add specific SPN patterns if needed

        # 5. Computer accounts
        for old_comp, new_comp in self.mappings['misc'].items():
            if old_comp.endswith('$'):
                replacements.append((old_comp, new_comp))

        # 6. Domain names (child before parent to avoid substring issues)
        domain_items = sorted(
            self.mappings['domains'].items(),
            key=lambda x: len(x[0]),
            reverse=True
        )
        for old_domain, new_domain in domain_items:
            replacements.append((old_domain, new_domain))

        # 7. Usernames
        for old_user, new_user in self.mappings['users'].items():
            replacements.append((old_user, new_user))

        # 8. Groups
        for old_group, new_group in self.mappings['groups'].items():
            replacements.append((old_group, new_group))

        # 9. OUs
        for old_ou, new_ou in self.mappings['ous'].items():
            replacements.append((old_ou, new_ou))

        # 10. Passwords
        for old_pwd, new_pwd in self.mappings['passwords'].items():
            replacements.append((old_pwd, new_pwd))

        # 11. NetBIOS names
        for old_netbios, new_netbios in self.mappings['netbios'].items():
            replacements.append((old_netbios, new_netbios))

        # 12. Misc mappings (firstname/surname components, computer accounts)
        for old_misc, new_misc in self.mappings['misc'].items():
            if not old_misc.endswith('$'):  # Computer accounts already added
                replacements.append((old_misc, new_misc))

        # Sort by length (longest first) to avoid substring collisions
        replacements.sort(key=lambda x: len(x[0]), reverse=True)

        # Remove duplicates while preserving order
        seen = set()
        unique_replacements = []
        for old, new in replacements:
            if (old, new) not in seen:
                seen.add((old, new))
                unique_replacements.append((old, new))

        self.replacements = unique_replacements

        print(f"Built {len(self.replacements)} ordered replacements")

    def apply_replacements(self, content: str) -> str:
        """
        Apply all replacements to content string.

        Uses word-boundary aware replacements for firstname/surname components
        to prevent substring collisions (e.g., 'robert' inside 'roberts').
        """
        for old, new in self.replacements:
            if old == new:
                continue

            # Check if this is a firstname/surname component (stored in misc mappings)
            # These are short strings that need word-boundary protection
            is_name_component = (
                old in self.mappings['misc'].keys() and
                not old.endswith('$') and  # Not a computer account
                not '.' in old and  # Not a FQDN or username
                not '\\' in old and  # Not a domain-qualified name
                len(old) < 50 and  # Reasonable name length
                old.replace('-', '').replace("'", '').isalpha()  # Mostly alphabetic
            )

            if is_name_component:
                # Use word-boundary regex for firstname/surname to prevent
                # "robert" from matching inside "roberts"
                pattern = r'\b' + re.escape(old) + r'\b'
                content = re.sub(pattern, new, content)
            else:
                # Use simple string replacement for everything else
                content = content.replace(old, new)

        return content

    def fix_user_firstname_surname(self, config: dict) -> dict:
        """Fix firstname/surname fields to match the generated usernames."""
        domains = config['lab']['domains']

        for _, domain_info in domains.items():
            users = domain_info.get('users', {})

            for username, user_info in users.items():
                # Skip service accounts
                if username in self.preserved_users:
                    continue

                # Split username into firstname.lastname
                if '.' in username:
                    parts = username.split('.')
                    user_info['firstname'] = parts[0]
                    user_info['surname'] = parts[1] if len(parts) > 1 else parts[0]

                    # Update description to match
                    if 'description' in user_info:
                        # Capitalize for description
                        desc_first = parts[0].capitalize()
                        desc_last = parts[1].capitalize() if len(parts) > 1 else parts[0].capitalize()
                        user_info['description'] = f"{desc_first} {desc_last}"

        return config

    def fix_passwords(self, config: dict) -> dict:
        """Fix password fields that were corrupted by global text replacement.

        When a password string equals a username (e.g., user "hodor" with
        password "hodor"), global replacement applies the username mapping
        to the password field. This method corrects those fields using the
        structured user_password_map built during map_passwords().
        """
        for _, domain_info in config['lab']['domains'].items():
            users = domain_info.get('users', {})
            for username, user_info in users.items():
                if username in self.user_password_map:
                    user_info['password'] = self.user_password_map[username]

        return config

    def rebuild_acl_keys(self, config: dict) -> dict:
        """Rebuild ACL dictionary keys to use new entity names."""
        domains = config['lab']['domains']

        for _, domain_info in domains.items():
            acls = domain_info.get('acls', {})
            if not acls:
                continue

            # Rebuild ACLs with new keys
            new_acls = {}
            for old_key, acl_data in acls.items():
                # Extract components from "for" and "to" fields to build new key
                for_entity = acl_data.get('for', '')
                to_entity = acl_data.get('to', '')

                # Simplify entity names for key (remove domain prefixes, paths, etc.)
                for_simple = for_entity.split('\\')[-1].split(',')[0].lower().replace(' ', '_')
                to_simple = to_entity.split('\\')[-1].split(',')[0].lower().replace(' ', '_')

                # Remove CN= and other LDAP prefixes
                to_simple = to_simple.replace('cn=', '').replace('ou=', '').replace('dc=', '')

                # Build new key using pattern from old key
                # Try to preserve the original key structure
                key_parts = old_key.split('_')
                if len(key_parts) >= 3:
                    # Pattern: right_entity1_entity2
                    new_key = f"{key_parts[0]}_{for_simple}_{to_simple}"
                else:
                    # Fallback: use original key
                    new_key = old_key

                new_acls[new_key] = acl_data

            domain_info['acls'] = new_acls

        return config

    def transform_file(self, file_path: Path, relative_path: Path) -> bool:
        """Transform a single file with replacements."""
        # Determine if file should be transformed
        text_extensions = {
            '.json', '.yml', '.yaml', '.ps1', '.tf', '.txt',
            '.md', '.sh', '.py', '.rb', '.cfg', '.conf', '.ini'
        }

        text_filenames = {'Vagrantfile', 'inventory', 'Makefile'}

        should_transform = (
            file_path.suffix in text_extensions or
            file_path.name in text_filenames
        )

        target_file = self.target_path / relative_path

        if should_transform:
            try:
                with open(file_path, 'r', encoding='utf-8', errors='ignore') as f:
                    content = f.read()

                # Apply replacements
                new_content = self.apply_replacements(content)

                # Special handling for config.json - fix firstname/surname fields and ACL keys
                if file_path.name == 'config.json' or file_path.name.endswith('-config.json'):
                    try:
                        config_data = json.loads(new_content)
                        config_data = self.fix_user_firstname_surname(config_data)
                        config_data = self.fix_passwords(config_data)
                        config_data = self.rebuild_acl_keys(config_data)
                        new_content = json.dumps(config_data, indent=2)
                    except json.JSONDecodeError:
                        pass  # If JSON parsing fails, use the replaced content as-is

                # Write transformed content
                target_file.parent.mkdir(parents=True, exist_ok=True)
                with open(target_file, 'w', encoding='utf-8') as f:
                    f.write(new_content)

                return True
            except Exception as e:
                print(f"Warning: Could not transform {relative_path}: {e}")
                # Fall back to copying
                target_file.parent.mkdir(parents=True, exist_ok=True)
                shutil.copy2(file_path, target_file)
                return False
        else:
            # Copy binary/other files as-is
            target_file.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(file_path, target_file)
            return False

    def copy_and_transform(self) -> None:
        """Copy GOAD directory and transform all files."""
        print("\n=== Copying and Transforming Files ===\n")

        # Create target directory
        self.target_path.mkdir(parents=True, exist_ok=True)

        # Track statistics
        total_files = 0
        transformed_files = 0
        copied_files = 0

        # Walk through source directory
        for file_path in self.source_path.rglob('*'):
            if file_path.is_file():
                relative_path = file_path.relative_to(self.source_path)

                # Skip git files and other metadata
                if '.git' in file_path.parts:
                    continue

                total_files += 1
                was_transformed = self.transform_file(file_path, relative_path)

                if was_transformed:
                    transformed_files += 1
                else:
                    copied_files += 1

                if total_files % 10 == 0:
                    print(f"Processed {total_files} files...")

        print(f"\nTransformation complete:")
        print(f"  Total files: {total_files}")
        print(f"  Transformed: {transformed_files}")
        print(f"  Copied as-is: {copied_files}")

    def validate(self) -> bool:
        """Validate the variant for completeness."""
        print("\n=== Validating Variant ===\n")

        # Original names to search for (should not appear in variant)
        original_names = [
            'sevenkingdoms', 'essos',
            'kingslanding', 'winterfell', 'meereen', 'castelblack', 'braavos',
            'stark', 'lannister', 'baratheon', 'targaryen', 'drogo', 'snow',
            'tywin', 'jaime', 'cersei', 'tyron', 'robert', 'joffrey',
            'arya', 'eddard', 'catelyn', 'robb', 'sansa', 'brandon',
            'daenerys', 'viserys', 'khal', 'jorah', 'mormont'
        ]

        violations = []
        files_checked = 0

        # Search variant files for original names
        for file_path in self.target_path.rglob('*'):
            if file_path.is_file():
                # Skip reference files that are supposed to contain original names
                if file_path.name in ['mapping.json', 'README.md']:
                    continue

                # Only check text files
                text_extensions = {
                    '.json', '.yml', '.yaml', '.ps1', '.tf', '.txt', '.md'
                }
                text_filenames = {'Vagrantfile', 'inventory'}

                if file_path.suffix not in text_extensions and file_path.name not in text_filenames:
                    continue

                files_checked += 1

                try:
                    with open(file_path, 'r', encoding='utf-8', errors='ignore') as f:
                        content = f.read().lower()

                    for name in original_names:
                        if name.lower() in content:
                            # Check if it's a real match (not part of another word)
                            pattern = r'\b' + re.escape(name.lower()) + r'\b'
                            if re.search(pattern, content):
                                violations.append((file_path, name))
                except Exception as e:
                    print(f"Warning: Could not check {file_path}: {e}")

        print(f"Checked {files_checked} text files")

        if violations:
            print(f"\n⚠️  Found {len(violations)} potential issues:")
            for file_path, name in violations[:20]:  # Show first 20
                rel_path = file_path.relative_to(self.target_path)
                print(f"  {rel_path}: contains '{name}'")
            if len(violations) > 20:
                print(f"  ... and {len(violations) - 20} more")
        else:
            print("✓ No original names found in variant files")

        # Validate structure counts
        print("\nValidating structure...")
        original_config = self.load_config()
        variant_config_path = self.target_path / "data" / "config.json"

        if variant_config_path.exists():
            with open(variant_config_path, 'r') as f:
                variant_config = json.load(f)

            # Check counts
            orig_hosts = len(original_config['lab']['hosts'])
            var_hosts = len(variant_config['lab']['hosts'])
            orig_domains = len(original_config['lab']['domains'])
            var_domains = len(variant_config['lab']['domains'])

            print(f"  Hosts: {orig_hosts} -> {var_hosts} {'✓' if orig_hosts == var_hosts else '✗'}")
            print(f"  Domains: {orig_domains} -> {var_domains} {'✓' if orig_domains == var_domains else '✗'}")

        return len(violations) == 0

    def create_documentation(self) -> None:
        """Create README for the variant."""
        readme_content = f"""# GOAD {self.variant_name.upper()}

This is a graph-isomorphic variant of the GOAD (Game of Active Directory) lab environment.

## About This Variant

- **All entity names have been randomized** while preserving the complete structure
- **Attack paths remain identical** to the original GOAD
- **All vulnerabilities preserved** with the same relationships
- **All 7 provider configs included**: VirtualBox, VMware, VMware ESXi, Proxmox, AWS, Azure, Ludus

## Structure

- **3 domains** with parent-child and trust relationships
- **5 VMs**: 3 Domain Controllers, 2 Servers
- **40+ users** with randomized names
- **18+ groups**, **8 OUs**, **20+ ACLs**

## Usage

Deploy exactly like the original GOAD:

```bash
# Navigate to provider directory
cd providers/virtualbox  # or vmware, proxmox, aws, azure, ludus

# Follow provider-specific setup instructions
# Provisioning works identically to GOAD
```

## Mapping Reference

See `mapping.json` for the complete entity mapping from GOAD to this variant.

## Purpose

This variant allows:
- **Multiple lab instances** without name conflicts
- **Training scenarios** with fresh environments
- **Testing automation** against different entity names
- **Validation** that tools work beyond hardcoded names

## Notes

- Service account `sql_svc` preserved for MSSQL functionality
- gMSA account randomized to `gmsa<Animal>` format
- All passwords changed with equivalent complexity
- VM identifiers (dc01, dc02, srv02, etc.) unchanged for compatibility
- Directory structure identical to original GOAD

---

Generated by GOAD Variant Generator
"""

        readme_path = self.target_path / "README.md"
        with open(readme_path, 'w') as f:
            f.write(readme_content)

        print(f"Documentation created at {readme_path}")

    def run(self) -> None:
        """Execute the full variant generation process."""
        print(f"\n{'='*60}")
        print(f"GOAD Variant Generator - {self.variant_name}")
        print(f"{'='*60}")
        print(f"Source: {self.source_path}")
        print(f"Target: {self.target_path}")
        print(f"{'='*60}\n")

        # Step 1: Generate mappings
        self.generate_mappings()

        # Step 2: Build ordered replacements
        self.build_ordered_replacements()

        # Step 3: Copy and transform files
        self.copy_and_transform()

        # Step 4: Save mappings
        self.save_mappings()

        # Step 5: Validate
        is_valid = self.validate()

        # Step 6: Create documentation
        self.create_documentation()

        print(f"\n{'='*60}")
        if is_valid:
            print("✓ Variant generation complete and validated!")
        else:
            print("⚠️  Variant generated but validation found issues")
        print(f"{'='*60}\n")


def main() -> None:
    parser = argparse.ArgumentParser(
        description='Generate GOAD variants with randomized names'
    )
    parser.add_argument(
        '--source',
        default='ad/GOAD',
        help='Source GOAD directory (default: ad/GOAD)'
    )
    parser.add_argument(
        '--target',
        default='ad/GOAD-variant-1',
        help='Target variant directory (default: ad/GOAD-variant-1)'
    )
    parser.add_argument(
        '--name',
        default='variant-1',
        help='Variant name (default: variant-1)'
    )

    args = parser.parse_args()

    generator = GOADVariantGenerator(
        source_path=args.source,
        target_path=args.target,
        variant_name=args.name
    )
    generator.run()


if __name__ == '__main__':
    main()

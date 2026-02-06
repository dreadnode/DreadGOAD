#!/usr/bin/env python3
"""
Name generation library for GOAD variant creation.
Generates random but pronounceable names for domains, users, groups, and OUs.
"""

from __future__ import annotations

import secrets
import string
from typing import Set


class NameGenerator:
    """Generate unique, pronounceable names for GOAD entities."""

    # Probability weights for name generation
    COMPOUND_GROUP_PROBABILITY = 0.5  # 50% chance of compound group names
    COMPOUND_OU_PROBABILITY = 0.5  # 50% chance of region vs division style
    COMPOUND_HOSTNAME_PROBABILITY = 0.33  # 33% chance of hyphenated hostnames

    def __init__(self) -> None:
        self.used_names: Set[str] = set()

        # Corporate-style words for domains
        self.domain_prefixes = [
            'zenith', 'apex', 'nexus', 'vertex', 'prism', 'quantum',
            'stellar', 'fusion', 'titan', 'phoenix', 'omega', 'delta',
            'sigma', 'vector', 'matrix', 'vortex', 'cipher', 'atlas'
        ]
        self.domain_suffixes = [
            'corp', 'tech', 'systems', 'solutions', 'global', 'industries',
            'ventures', 'enterprises', 'group', 'labs', 'dynamics', 'works'
        ]

        # Realistic first names
        self.first_names = [
            'James', 'Michael', 'Robert', 'John', 'David', 'William',
            'Richard', 'Joseph', 'Thomas', 'Charles', 'Christopher', 'Daniel',
            'Matthew', 'Anthony', 'Mark', 'Donald', 'Steven', 'Paul',
            'Andrew', 'Joshua', 'Kenneth', 'Kevin', 'Brian', 'George',
            'Timothy', 'Ronald', 'Edward', 'Jason', 'Jeffrey', 'Ryan',
            'Jacob', 'Gary', 'Nicholas', 'Eric', 'Jonathan', 'Stephen',
            'Larry', 'Justin', 'Scott', 'Brandon', 'Benjamin', 'Samuel',
            'Raymond', 'Gregory', 'Alexander', 'Patrick', 'Frank', 'Dennis',
            'Mary', 'Patricia', 'Jennifer', 'Linda', 'Barbara', 'Elizabeth',
            'Susan', 'Jessica', 'Sarah', 'Karen', 'Nancy', 'Lisa',
            'Betty', 'Margaret', 'Sandra', 'Ashley', 'Kimberly', 'Emily',
            'Donna', 'Michelle', 'Dorothy', 'Carol', 'Amanda', 'Melissa',
            'Deborah', 'Stephanie', 'Rebecca', 'Sharon', 'Laura', 'Cynthia',
            'Kathleen', 'Amy', 'Angela', 'Shirley', 'Anna', 'Brenda',
            'Pamela', 'Emma', 'Nicole', 'Helen', 'Samantha', 'Katherine',
            'Christine', 'Debra', 'Rachel', 'Carolyn', 'Janet', 'Catherine'
        ]

        # Realistic last names
        self.last_names = [
            'Smith', 'Johnson', 'Williams', 'Brown', 'Jones', 'Garcia',
            'Miller', 'Davis', 'Rodriguez', 'Martinez', 'Hernandez', 'Lopez',
            'Gonzalez', 'Wilson', 'Anderson', 'Thomas', 'Taylor', 'Moore',
            'Jackson', 'Martin', 'Lee', 'Perez', 'Thompson', 'White',
            'Harris', 'Sanchez', 'Clark', 'Ramirez', 'Lewis', 'Robinson',
            'Walker', 'Young', 'Allen', 'King', 'Wright', 'Scott',
            'Torres', 'Nguyen', 'Hill', 'Flores', 'Green', 'Adams',
            'Nelson', 'Baker', 'Hall', 'Rivera', 'Campbell', 'Mitchell',
            'Carter', 'Roberts', 'Gomez', 'Phillips', 'Evans', 'Turner',
            'Diaz', 'Parker', 'Cruz', 'Edwards', 'Collins', 'Reyes',
            'Stewart', 'Morris', 'Morales', 'Murphy', 'Cook', 'Rogers',
            'Gutierrez', 'Ortiz', 'Morgan', 'Cooper', 'Peterson', 'Bailey',
            'Reed', 'Kelly', 'Howard', 'Ramos', 'Kim', 'Cox',
            'Ward', 'Richardson', 'Watson', 'Brooks', 'Chavez', 'Wood',
            'James', 'Bennett', 'Gray', 'Mendoza', 'Ruiz', 'Hughes',
            'Price', 'Alvarez', 'Castillo', 'Sanders', 'Patel', 'Myers'
        ]

        # Hostname patterns (city names, tech terms, etc.)
        self.hostname_prefixes = [
            'aurora', 'phoenix', 'summit', 'cascade', 'horizon', 'alpine',
            'delta', 'echo', 'nova', 'terra', 'luna', 'solar',
            'atlas', 'titan', 'nexus', 'zenith', 'vertex', 'apex',
            'quantum', 'cipher', 'vector', 'matrix', 'prism', 'vortex',
            'beacon', 'sentinel', 'guardian', 'fortress', 'citadel', 'bastion'
        ]

        self.hostname_suffixes = [
            'srv', 'node', 'host', 'sys', 'hub', 'core',
            'prod', 'dev', 'test', 'app', 'db', 'web'
        ]

        # Group name themes
        self.group_themes = [
            'Operations', 'Engineering', 'Security', 'Analytics', 'Development',
            'Infrastructure', 'Platform', 'Services', 'Systems', 'Management',
            'Administration', 'Executive', 'Leadership', 'Research', 'Support'
        ]

        # OU name styles
        self.ou_regions = [
            'Americas', 'EMEA', 'APAC', 'Europe', 'Pacific', 'Atlantic',
            'Northern', 'Southern', 'Eastern', 'Western', 'Central'
        ]
        self.ou_divisions = [
            'Operations', 'Engineering', 'Sales', 'Marketing', 'Finance',
            'HR', 'IT', 'Legal', 'Corporate', 'Research'
        ]

        # Animal names for gMSA accounts
        self.animals = [
            'Phoenix', 'Griffin', 'Falcon', 'Eagle', 'Hawk', 'Raven',
            'Wolf', 'Bear', 'Lion', 'Tiger', 'Panther', 'Leopard',
            'Cobra', 'Viper', 'Python', 'Raptor', 'Condor', 'Vulture'
        ]

    def ensure_unique(self, name: str) -> str:
        """Ensure name is unique, regenerate if needed."""
        original_name = name
        counter = 2
        while name.lower() in self.used_names:
            name = f"{original_name}{counter}"
            counter += 1
        self.used_names.add(name.lower())
        return name

    def generate_domain_name(self) -> str:
        """Generate a corporate-style domain name."""
        prefix = secrets.choice(self.domain_prefixes)
        suffix = secrets.choice(self.domain_suffixes)
        domain = f"{prefix}{suffix}"
        return self.ensure_unique(domain)

    def generate_subdomain_name(self) -> str:
        """Generate a subdomain name for child domains."""
        subdomain_words = [
            'ops', 'dev', 'prod', 'test', 'stage', 'corp', 'hq',
            'services', 'apps', 'data', 'cloud', 'platform'
        ]
        subdomain = secrets.choice(subdomain_words)
        return self.ensure_unique(subdomain)

    def generate_first_name(self) -> str:
        """Generate a realistic first name."""
        name = secrets.choice(self.first_names)
        return self.ensure_unique(name)

    def generate_last_name(self) -> str:
        """Generate a realistic last name."""
        name = secrets.choice(self.last_names)
        return self.ensure_unique(name)

    def generate_username(self) -> str:
        """Generate a username in firstname.lastname format."""
        first = self.generate_first_name()
        last = self.generate_last_name()
        username = f"{first.lower()}.{last.lower()}"
        return self.ensure_unique(username)

    def generate_group_name(self) -> str:
        """Generate a group name with thematic words."""
        # Mix of single words and compound names
        if secrets.SystemRandom().random() < self.COMPOUND_GROUP_PROBABILITY:
            # Simple theme word
            name = secrets.choice(self.group_themes)
        else:
            # Compound name
            theme = secrets.choice(self.group_themes)
            suffix = secrets.choice(['Team', 'Group', 'Unit', 'Squad', 'Staff'])
            name = f"{theme}{suffix}"

        return self.ensure_unique(name)

    def generate_ou_name(self) -> str:
        """Generate an OU name in region/division style."""
        if secrets.SystemRandom().random() < self.COMPOUND_OU_PROBABILITY:
            # Region style
            name = secrets.choice(self.ou_regions)
        else:
            # Division style
            name = secrets.choice(self.ou_divisions)

        return self.ensure_unique(name)

    def generate_hostname(self) -> str:
        """Generate a realistic hostname (server/DC name)."""
        # Use realistic patterns like "aurora-srv", "phoenix-node", or just "atlas"
        if secrets.SystemRandom().random() < self.COMPOUND_HOSTNAME_PROBABILITY:
            # Compound name (prefix-suffix)
            prefix = secrets.choice(self.hostname_prefixes)
            suffix = secrets.choice(self.hostname_suffixes)
            name = f"{prefix}-{suffix}"
        else:
            # Simple name (just prefix)
            name = secrets.choice(self.hostname_prefixes)
        return self.ensure_unique(name.lower())

    def generate_gmsa_name(self) -> str:
        """Generate a gMSA account name like 'gmsaPhoenix'."""
        animal = secrets.choice(self.animals)
        name = f"gmsa{animal}"
        return self.ensure_unique(name)

    def generate_password(self, original: str) -> str:
        """
        Generate a password matching the complexity of the original.

        Analyzes length and character types (upper, lower, digit, special)
        and generates a new random password with equivalent complexity.
        """
        length = len(original)
        has_upper = any(c.isupper() for c in original)
        has_lower = any(c.islower() for c in original)
        has_digit = any(c.isdigit() for c in original)
        has_special = any(not c.isalnum() for c in original)

        # Build character set
        chars = ''
        if has_lower:
            chars += string.ascii_lowercase
        if has_upper:
            chars += string.ascii_uppercase
        if has_digit:
            chars += string.digits
        if has_special:
            chars += '!@#$%^&*()-_=+[]{}|;:,.<>?'

        # If no character types detected, use lowercase
        if not chars:
            chars = string.ascii_lowercase

        # Ensure at least one of each required type
        password = []
        if has_upper:
            password.append(secrets.choice(string.ascii_uppercase))
        if has_lower:
            password.append(secrets.choice(string.ascii_lowercase))
        if has_digit:
            password.append(secrets.choice(string.digits))
        if has_special:
            password.append(secrets.choice('!@#$%^&*()-_=+'))

        # Fill remaining length
        while len(password) < length:
            password.append(secrets.choice(chars))

        # Shuffle for randomness
        secrets.SystemRandom().shuffle(password)
        return ''.join(password)

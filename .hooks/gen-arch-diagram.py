#!/usr/bin/env python3
"""
Ansible Collection Mermaid Diagram Generator Pre-commit Hook

Generates a Mermaid diagram from an Ansible collection structure
and updates repo README.
"""

from pathlib import Path
import sys
import re

class AnsibleCollectionAnalyzer:
    def __init__(self, collection_path: str):
        self.collection_path = Path(collection_path)
        self.structure = {
            'roles': [],
            'plugins': [],
            'playbooks': []
        }

    def analyze(self):
        """Analyze the Ansible collection structure"""
        # Analyze roles
        roles_path = self.collection_path / 'roles'
        if roles_path.exists():
            for role_dir in roles_path.iterdir():
                if role_dir.is_dir() and not role_dir.name.startswith('.'):
                    self.structure['roles'].append({
                        'name': role_dir.name,
                    })

        # Analyze plugins
        plugins_path = self.collection_path / 'plugins' / 'modules'
        if plugins_path.exists():
            for plugin_file in plugins_path.glob('*.py'):
                if not plugin_file.name.startswith('__'):
                    self.structure['plugins'].append(plugin_file.stem)

        # Analyze playbooks
        playbooks_path = self.collection_path / 'playbooks'
        if playbooks_path.exists():
            for item in playbooks_path.iterdir():
                if item.is_dir() and not item.name.startswith('.'):
                    self.structure['playbooks'].append({
                        'name': item.name,
                    })

        return self.structure

def generate_mermaid(structure):
    """Generate Mermaid diagram"""
    lines = ["```mermaid", "graph TD"]
    lines.append("    Collection[Ansible Collection]")

    # Add plugins
    if structure['plugins']:
        lines.append("    Collection --> Plugins[🔌 Plugins]")
        for i, plugin in enumerate(structure['plugins']):
            lines.append(f"    Plugins --> P{i}[{plugin}]")

    # Add roles
    if structure['roles']:
        lines.append("    Collection --> Roles[⚙️ Roles]")
        for i, role in enumerate(structure['roles']):
            lines.append(f"    Roles --> R{i}[{role['name']}]")

    # Add playbooks
    if structure['playbooks']:
        lines.append("    Collection --> Playbooks[📚 Playbooks]")
        for i, playbook in enumerate(structure['playbooks']):
            lines.append(f"    Playbooks --> PB{i}[{playbook['name']}]")

    lines.append("```")
    return '\n'.join(lines)

def update_readme(mermaid_content):
    """Update README.md with the generated Mermaid diagram"""
    readme_path = Path('README.md')

    if not readme_path.exists():
        print("❌ README.md not found")
        return False

    readme_content = readme_path.read_text()

    # Define markers for the architecture section
    start_marker = "## Architecture Diagram"
    end_marker = "## Requirements"  # The next section after Architecture Diagram

    # Find the start and end positions
    start_pos = readme_content.find(start_marker)
    if start_pos == -1:
        print("❌ Could not find '## Architecture Diagram' section in README.md")
        return False

    end_pos = readme_content.find(end_marker, start_pos)
    if end_pos == -1:
        # If we can't find the next section, look for the next ## heading
        next_section_pattern = re.compile(r'\n## (?!Architecture Diagram)')
        match = next_section_pattern.search(readme_content, start_pos + len(start_marker))
        if match:
            end_pos = match.start() + 1  # +1 to keep the newline before the next section
        else:
            print("❌ Could not find the end of the Architecture Diagram section")
            return False

    # Build the new architecture section
    new_section = f"{start_marker}\n\n{mermaid_content}\n\n"

    # Replace the section
    new_readme = readme_content[:start_pos] + new_section + readme_content[end_pos:]

    # Write back to README
    readme_path.write_text(new_readme)

    return True

def main():
    """Main function for pre-commit hook"""
    # Analyze collection from current directory
    analyzer = AnsibleCollectionAnalyzer('ansible')
    structure = analyzer.analyze()

    # Generate Mermaid diagram
    mermaid_content = generate_mermaid(structure)

    # Update README.md
    if update_readme(mermaid_content):
        print("✅ Architecture diagram updated in README.md")
        print(f"\nCollection summary:")
        print(f"  • Roles: {len(structure['roles'])}")
        print(f"  • Plugins: {len(structure['plugins'])}")
        print(f"  • Playbooks: {len(structure['playbooks'])}")

        # Stage the README.md file for commit
        import subprocess
        try:
            subprocess.run(['git', 'add', 'README.md'], check=True)
            print("✅ README.md staged for commit")
        except subprocess.CalledProcessError:
            print("⚠️ Could not stage README.md - you may need to add it manually")

        return 0
    else:
        print("❌ Failed to update README.md")
        return 1

if __name__ == "__main__":
    sys.exit(main())

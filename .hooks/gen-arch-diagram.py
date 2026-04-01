#!/usr/bin/env python3
"""
Ansible Collection Architecture Diagram Generator (Pre-commit Hook)

Analyzes the collection structure, generates a categorized Mermaid diagram,
renders it to SVG via mermaid-cli (mmdc), and updates the README to reference
the SVG image.
"""

from pathlib import Path
import re
import shutil
import subprocess
import sys

# ── Role categories ──────────────────────────────────────────────────────────
# Roles are matched top-down; first match wins.  Prefix-based entries cover
# most roles, explicit names handle the exceptions.
CATEGORIES = [
    ("LAPS", {
        "prefixes": ["laps_"],
        "names": ["laps_dc"],
        "color": "#f39c12",
        "border": "#d68910",
        "detail_bg": "#3d2e0a",
    }),
    ("SCCM", {
        "prefixes": ["sccm_"],
        "names": [],
        "color": "#1abc9c",
        "border": "#16a085",
        "detail_bg": "#0d2e28",
    }),
    ("Vulnerabilities", {
        "prefixes": ["vulns_"],
        "names": [],
        "color": "#9b59b6",
        "border": "#8e44ad",
        "detail_bg": "#2a1a33",
    }),
    ("Security", {
        "prefixes": ["security_"],
        "names": ["dc_audit_sacl", "ldap_diagnostic_logging"],
        "color": "#e67e22",
        "border": "#d35400",
        "detail_bg": "#3a2210",
    }),
    ("Settings", {
        "prefixes": ["settings_"],
        "names": [],
        "color": "#3498db",
        "border": "#2980b9",
        "detail_bg": "#132a3d",
    }),
    ("Active Directory", {
        "prefixes": [],
        "names": [
            "ad", "acl", "adcs", "adcs_templates",
            "child_domain", "domain_controller", "domain_controller_slave",
            "member_server", "trusts",
            "gmsa", "gmsa_hosts", "password_policy",
            "move_to_ou", "groups_domains", "onlyusers",
            "dns_conditional_forwarder", "dc_dns_conditional_forwarder",
            "parent_child_dns", "sync_domains",
            "disable_user", "enable_user",
        ],
        "color": "#e74c3c",
        "border": "#c0392b",
        "detail_bg": "#3a1515",
    }),
    ("Server Roles", {
        "prefixes": ["mssql_"],
        "names": [
            "common", "commonwkstn", "localusers",
            "mssql", "iis", "elk", "webdav", "dhcp",
            "logs_windows", "fix_dns", "ps",
        ],
        "color": "#2ecc71",
        "border": "#27ae60",
        "detail_bg": "#133320",
    }),
]


def categorize_roles(roles):
    """Assign each role to its category.  Returns {category: [role, ...]}."""
    result = {name: [] for name, _ in CATEGORIES}
    uncategorized = []

    for role in sorted(roles):
        matched = False
        for cat_name, cat_cfg in CATEGORIES:
            if role in cat_cfg["names"]:
                result[cat_name].append(role)
                matched = True
                break
            for prefix in cat_cfg["prefixes"]:
                if role.startswith(prefix):
                    result[cat_name].append(role)
                    matched = True
                    break
            if matched:
                break
        if not matched:
            uncategorized.append(role)

    if uncategorized:
        print(f"  Warning: uncategorized roles: {uncategorized}", file=sys.stderr)
        # Put them in Server Roles as a catch-all
        result["Server Roles"].extend(uncategorized)

    return result


def discover_collection(collection_path):
    """Return roles, plugins, and playbooks from the collection."""
    base = Path(collection_path)
    roles = []
    plugins = []
    playbooks = []

    roles_dir = base / "roles"
    if roles_dir.exists():
        roles = [d.name for d in roles_dir.iterdir()
                 if d.is_dir() and not d.name.startswith(".")]

    plugins_dir = base / "plugins" / "modules"
    if plugins_dir.exists():
        plugins = [f.stem for f in plugins_dir.glob("*.py")
                   if not f.name.startswith("__")]

    playbooks_dir = base / "playbooks"
    if playbooks_dir.exists():
        playbooks = [f.stem for f in playbooks_dir.glob("*.yml")
                     if not f.name.startswith(".") and not f.name.endswith(".retry")]

    return sorted(roles), sorted(plugins), sorted(playbooks)


def abbreviate_roles(roles, prefixes=None):
    """Return a string listing all roles for the detail box."""
    def strip_prefix(name):
        if prefixes:
            for p in prefixes:
                if name.startswith(p):
                    return name[len(p):]
        return name

    display = [strip_prefix(r) for r in roles]
    # 3 per line via <br/>
    chunks = []
    for i in range(0, len(display), 3):
        chunks.append(" &bull; ".join(display[i:i + 3]))
    return "<br/>".join(chunks)


def generate_mermaid(roles, plugins, playbooks):
    """Build a categorized Mermaid diagram string."""
    categorized = categorize_roles(roles)
    lines = ["graph LR"]
    lines.append('    Collection["dreadnode.goad<br/>Ansible Collection"]')
    lines.append("")

    # Category nodes
    for cat_name, cat_cfg in CATEGORIES:
        cat_roles = categorized.get(cat_name, [])
        if not cat_roles:
            continue
        node_id = cat_name.replace(" ", "")
        lines.append(
            f'    Collection --> {node_id}'
            f'["{cat_name}<br/><i>{len(cat_roles)} roles</i>"]'
        )

    lines.append("")

    # Detail nodes (dashed links)
    for cat_name, cat_cfg in CATEGORIES:
        cat_roles = categorized.get(cat_name, [])
        if not cat_roles:
            continue
        node_id = cat_name.replace(" ", "")
        detail_id = f"{node_id}_detail"
        summary = abbreviate_roles(cat_roles, prefixes=cat_cfg["prefixes"])
        lines.append(f'    {node_id} -.- {detail_id}["{summary}"]')

    # Plugins node
    if plugins:
        lines.append("")
        lines.append(
            f'    Collection --> Plugins'
            f'["Plugins<br/><i>{len(plugins)} modules</i>"]'
        )
        plugin_summary = "<br/>".join(
            " &bull; ".join(plugins[i:i + 3])
            for i in range(0, len(plugins), 3)
        )
        lines.append(f'    Plugins -.- Plugins_detail["{plugin_summary}"]')

    # Playbooks node
    if playbooks:
        lines.append("")
        lines.append(
            f'    Collection --> Playbooks'
            f'["Playbooks<br/><i>{len(playbooks)} playbook{"s" if len(playbooks) != 1 else ""}</i>"]'
        )
        chunks = []
        for i in range(0, len(playbooks), 3):
            chunks.append(" &bull; ".join(playbooks[i:i + 3]))
        pb_summary = "<br/>".join(chunks)
        lines.append(f'    Playbooks -.- Playbooks_detail["{pb_summary}"]')

    # Styles
    lines.append("")
    lines.append("    style Collection fill:#4a9eff,stroke:#2d7cd4,color:#fff,font-weight:bold")
    if plugins:
        lines.append("    style Plugins fill:#34495e,stroke:#2c3e50,color:#fff")
        lines.append("    style Plugins_detail fill:#ebedef,stroke:#34495e,color:#333")
    if playbooks:
        lines.append("    style Playbooks fill:#7f8c8d,stroke:#6c7a7d,color:#fff")
        lines.append("    style Playbooks_detail fill:#2a2a2a,stroke:#6c7a7d,color:#ccc")
    for cat_name, cat_cfg in CATEGORIES:
        cat_roles = categorized.get(cat_name, [])
        if not cat_roles:
            continue
        nid = cat_name.replace(" ", "")
        lines.append(
            f"    style {nid} fill:{cat_cfg['color']},"
            f"stroke:{cat_cfg['border']},color:#fff"
        )
        lines.append(
            f"    style {nid}_detail fill:{cat_cfg['detail_bg']},"
            f"stroke:{cat_cfg['border']},color:#ccc"
        )

    return "\n".join(lines)


def render_svg(mmd_path, svg_path):
    """Render .mmd to .svg via mermaid-cli (mmdc)."""
    # Try mmdc directly, then npx fallback
    mmdc = shutil.which("mmdc")
    if mmdc:
        cmd = [mmdc]
    else:
        npx = shutil.which("npx")
        if not npx:
            print("Error: neither mmdc nor npx found - install @mermaid-js/mermaid-cli",
                  file=sys.stderr)
            return False
        cmd = [npx, "--yes", "@mermaid-js/mermaid-cli"]

    # Use dark theme with dark background
    config_path = mmd_path.parent / "mermaid-config.json"
    config_path.write_text('{"theme":"dark","themeVariables":{"darkMode":true}}\n')
    # Puppeteer config for CI (no-sandbox required on GitHub Actions)
    puppeteer_config_path = mmd_path.parent / "puppeteer-config.json"
    puppeteer_config_path.write_text('{"args":["--no-sandbox","--disable-setuid-sandbox"]}\n')
    cmd += [
        "-i", str(mmd_path), "-o", str(svg_path),
        "-c", str(config_path),
        "-p", str(puppeteer_config_path),
        "-b", "#1a1a2e",
        "-w", "1600",
    ]
    try:
        subprocess.run(cmd, check=True, capture_output=True, text=True)
    except subprocess.CalledProcessError as exc:
        print(f"Error rendering SVG: {exc.stderr}", file=sys.stderr)
        return False
    finally:
        config_path.unlink(missing_ok=True)
        puppeteer_config_path.unlink(missing_ok=True)
    return True


def update_readme(readme_path="README.md"):
    """Ensure README references the SVG image instead of inline mermaid."""
    path = Path(readme_path)
    if not path.exists():
        print("README.md not found", file=sys.stderr)
        return False

    content = path.read_text()

    start_marker = "## Architecture Diagram"
    start_pos = content.find(start_marker)
    if start_pos == -1:
        print("Could not find '## Architecture Diagram' section", file=sys.stderr)
        return False

    # Find the next ## heading
    next_heading = re.search(r"\n## (?!Architecture Diagram)", content[start_pos + len(start_marker):])
    if next_heading:
        end_pos = start_pos + len(start_marker) + next_heading.start() + 1
    else:
        end_pos = len(content)

    new_section = f"""{start_marker}

![Architecture](docs/architecture.svg)

"""

    new_content = content[:start_pos] + new_section + content[end_pos:]

    if new_content != content:
        path.write_text(new_content)
        return True

    return True  # No change needed


def main():
    """Main entry point for pre-commit hook."""
    roles, plugins, playbooks = discover_collection("ansible")

    print(f"  Collection: {len(roles)} roles, {len(plugins)} plugins, {len(playbooks)} playbooks")

    # Generate mermaid source
    mermaid_content = generate_mermaid(roles, plugins, playbooks)
    mmd_path = Path("docs/architecture.mmd")
    mmd_path.parent.mkdir(parents=True, exist_ok=True)
    mmd_path.write_text(mermaid_content + "\n")

    # Render to SVG
    svg_path = Path("docs/architecture.svg")
    if not render_svg(mmd_path, svg_path):
        print("Failed to render SVG - is @mermaid-js/mermaid-cli installed?",
              file=sys.stderr)
        return 1

    # Update README
    update_readme()

    # Stage generated files
    try:
        subprocess.run(
            ["git", "add", "README.md", str(mmd_path), str(svg_path)],
            check=True,
        )
    except subprocess.CalledProcessError:
        print("Warning: could not stage files", file=sys.stderr)

    print("  Architecture diagram updated (docs/architecture.svg)")
    return 0


if __name__ == "__main__":
    sys.exit(main())

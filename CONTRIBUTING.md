# Contributing to DreadGOAD

Thanks for your interest in contributing! DreadGOAD is a fork of
[GOAD](https://github.com/Orange-Cyberdefense/GOAD) and we welcome
contributions that improve the labs, tooling, and documentation.

## Development Environment

| Tool | Version | Notes |
|------|---------|-------|
| Go | 1.26+ | CLI lives in `cli/` |
| Ansible | 2.x | Roles in `ansible/`, lint with `ansible-lint` |
| Terraform | 1.x | Modules in `infra/`, `modules/` |
| Pre-commit | any | Install hooks: `pre-commit install` |

## Getting Started

1. Fork the repository
2. Create a feature branch from `main`
3. Make your changes
4. Run tests locally (see below)
5. Submit a pull request

## Running Tests

```bash
# Go CLI tests
cd cli && go test ./...

# Ansible linting
ansible-lint ansible/

# Pre-commit checks (runs automatically on commit)
pre-commit run --all-files
```

## What We're Looking For

- New vulnerability scenarios or attack paths
- Improvements to existing Ansible roles
- Bug fixes in provisioning or the Go CLI
- New provider support or extension modules
- Documentation improvements
- Test coverage

## Guidelines

### Code

- Follow the existing code style in each language (Go, Ansible/YAML)
- Ansible roles should include a `README.md` describing the role's purpose and variables
- Test your changes against at least one provider before submitting

### Ansible Roles

- Place new roles under `ansible/roles/`
- Use the collection namespace `dreadnode.goad` for module references
- Include default variables in `defaults/main.yml`

### Lab Configurations

- Lab definitions live under `ad/<LAB_NAME>/`
- Use `ad/TEMPLATE/` as a starting point for new labs
- Document the lab's topology, users, and intended vulnerabilities in its `README.md`

### Commits

- Write clear, descriptive commit messages
- Keep commits focused -- one logical change per commit

### Pull Requests

- Describe what changed and why
- Reference any related issues
- Include testing details (which provider, which lab)

## Reporting Issues

Open an issue on GitHub with:

- What you expected to happen
- What actually happened
- Steps to reproduce
- Provider and OS details

## License

By contributing, you agree that your contributions will be licensed under the
GPL-3.0-or-later license.

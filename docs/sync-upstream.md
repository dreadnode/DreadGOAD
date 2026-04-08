# Synchronizing DreadGOAD with Upstream

When working with DreadGOAD, a fork of the [GOAD repository](https://github.com/Orange-Cyberdefense/GOAD),
periodically synchronize your fork with the upstream repository to keep it
up-to-date. Follow these steps:

```bash
# Fetch all branches from the upstream repository
git fetch upstream

# Ensure you're on your main branch
git checkout main

# Rebase your main branch onto the upstream main branch
git rebase upstream/main

# Force push the rebased main branch to your fork
git push origin main --force
```

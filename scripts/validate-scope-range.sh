#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
scratch="$(mktemp -d)"
cleanup() {
  chmod -R u+w "$scratch" 2>/dev/null || true
  rm -rf "$scratch"
}
trap cleanup EXIT

cd "$repo_root"
git diff --check

python3 -m json.tool ad/SCOPE-RANGE/data/validation.json >/dev/null
env PYTHONPYCACHEPREFIX="$scratch/python-cache" \
  python3 -m py_compile scripts/validate-scope-range-live.py
env PYTHONPYCACHEPREFIX="$scratch/python-cache" \
  python3 -m unittest discover -s scripts/tests -p 'test_*.py'

tofu fmt -check -recursive modules/terraform-azure-linux-instance
tofu fmt -check -recursive modules/terraform-local-ssh-key
tofu fmt -check -recursive modules/terraform-azure-kali
terragrunt hcl fmt --check --diff --working-dir infra/azure/scope-range-deployment
terragrunt hcl validate --working-dir infra/azure/scope-range-deployment

for module in \
  modules/terraform-azure-linux-instance \
  modules/terraform-local-ssh-key \
  modules/terraform-azure-kali; do
  tofu -chdir="$module" init -backend=false -input=false >/dev/null
  tofu -chdir="$module" validate
done

env \
  GOCACHE="$scratch/go-build" \
  GOMODCACHE="$scratch/go-mod" \
  go -C cli test ./internal/config ./internal/lab ./internal/azure ./cmd

ansible-galaxy collection build ansible --output-path "$scratch" --force >/dev/null
env ANSIBLE_COLLECTIONS_PATH="$scratch/collections" ansible-galaxy collection install \
  "$scratch"/dreadnode-goad-*.tar.gz \
  --collections-path "$scratch/collections" --force --pre >/dev/null

for playbook in \
  scope-base.yml \
  scope-services.yml \
  scope-data-storage.yml \
  scope-dev-web.yml \
  scope-kali.yml; do
  env \
    ANSIBLE_CONFIG="$repo_root/ansible/ansible.cfg" \
    ANSIBLE_COLLECTIONS_PATH="$scratch/collections" \
    ANSIBLE_LOCAL_TEMP="$scratch/ansible-local" \
    ansible-playbook \
      --inventory ad/SCOPE-RANGE/providers/azure/inventory \
      --syntax-check "ansible/playbooks/$playbook"
done

echo "SCOPE-RANGE static validation passed."

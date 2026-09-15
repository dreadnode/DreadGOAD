#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
scratch="$(mktemp -d)"
cleanup() {
    chmod -R u+w "$scratch" 2> /dev/null || true
    rm -rf "$scratch"
}
trap cleanup EXIT

cd "$repo_root"
git diff --check

for json_file in \
    ad/GOAT/data/validation.json \
    ansible/roles/goat_kali/files/targets.json \
    ansible/roles/goat_storage/files/range-assets/kraken/brief.json \
    ansible/roles/goat_storage/files/research-archives/kraken/experiment-summary.json; do
    python3 -m json.tool "$json_file" > /dev/null
done

for python_file in \
    scripts/validate-goat-live.py \
    ansible/roles/goat_development/files/seed-gitea.py \
    ansible/roles/goat_development/files/seed-jenkins.py \
    ansible/roles/goat_services/files/queue_worker.py \
    ansible/roles/goat_services/files/seed-queue-jobs.py; do
    env PYTHONPYCACHEPREFIX="$scratch/python-cache" \
        python3 -m py_compile "$python_file"
done

for shell_file in \
    ansible/roles/goat_data/files/seed-backups.sh \
    ansible/roles/goat_kali/files/goat-browser-smoke.sh \
    ansible/roles/goat_storage/files/seed-storage.sh; do
    bash -n "$shell_file"
done

env PYTHONPYCACHEPREFIX="$scratch/python-cache" \
    python3 -m unittest discover -s scripts/tests -p 'test_*.py'
env PYTHONPYCACHEPREFIX="$scratch/python-cache" \
    PYTHONPATH=ansible/roles/goat_development/files/kraken-control-plane \
    python3 -m unittest discover \
    -s ansible/roles/goat_development/files/kraken-control-plane/tests \
    -p 'test_*.py'

tofu fmt -check -recursive modules/terraform-azure-linux-instance
tofu fmt -check -recursive modules/terraform-local-ssh-key
tofu fmt -check -recursive modules/terraform-azure-kali
tofu fmt -check -recursive modules/terraform-aws-instance-factory
tofu fmt -check -recursive modules/terraform-aws-net
tofu fmt -check -recursive modules/terraform-aws-kali
tofu fmt -check -recursive modules/terraform-aws-linux-instance
terragrunt hcl fmt --check --diff --working-dir infra/azure/goat-deployment
terragrunt hcl validate --working-dir infra/azure/goat-deployment
terragrunt hcl fmt --check --diff --working-dir infra/goat-deployment

for module in \
    modules/terraform-azure-linux-instance \
    modules/terraform-local-ssh-key \
    modules/terraform-azure-kali \
    modules/terraform-aws-instance-factory \
    modules/terraform-aws-net \
    modules/terraform-aws-kali \
    modules/terraform-aws-linux-instance; do
    tofu -chdir="$module" init -backend=false -input=false > /dev/null
    tofu -chdir="$module" validate
done

./scripts/validate-goat-live.py --provider azure --manifest-only > /dev/null
./scripts/validate-goat-live.py --provider aws --manifest-only > /dev/null

env \
    GOCACHE="$scratch/go-build" \
    GOMODCACHE="$scratch/go-mod" \
    go -C cli test ./...

ansible-galaxy collection build ansible --output-path "$scratch" --force > /dev/null
env ANSIBLE_COLLECTIONS_PATH="$scratch/collections" ansible-galaxy collection install \
    "$scratch"/dreadnode-goad-*.tar.gz \
    --collections-path "$scratch/collections" --force --pre > /dev/null

for playbook in \
    goat-base.yml \
    goat-services.yml \
    goat-data-storage.yml \
    goat-dev-web.yml \
    goat-kali.yml \
    goat-seed.yml; do
    env \
        ANSIBLE_CONFIG="$repo_root/ansible/ansible.cfg" \
        ANSIBLE_COLLECTIONS_PATH="$scratch/collections" \
        ANSIBLE_LOCAL_TEMP="$scratch/ansible-local" \
        ansible-playbook \
        --inventory ad/GOAT/providers/azure/inventory \
        --syntax-check "ansible/playbooks/$playbook"
done

for inventory in \
    ad/GOAT/providers/azure/inventory \
    ad/GOAT/providers/aws/inventory; do
    env \
        ANSIBLE_CONFIG="$repo_root/ansible/ansible.cfg" \
        ANSIBLE_COLLECTIONS_PATH="$scratch/collections" \
        ANSIBLE_LOCAL_TEMP="$scratch/ansible-local" \
        ansible-inventory --inventory "$inventory" --list > /dev/null
done

echo "GOAT static validation passed."

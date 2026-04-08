<!--
Thanks for contributing to DreadGOAD!

Please fill in the sections below. Delete sections that don't apply, but try
not to leave the template completely empty — context helps reviewers a lot.
-->

## Summary

<!-- One or two sentences: what does this PR do, and why? -->

## Type of change

<!-- Check all that apply. -->

- [ ] Bug fix (non-breaking change that fixes an issue)
- [ ] New feature (non-breaking change that adds functionality)
- [ ] Breaking change (fix or feature that would change existing behavior)
- [ ] New lab, lab variant, or extension
- [ ] New / updated provider support
- [ ] Refactor / internal cleanup (no functional change)
- [ ] Documentation
- [ ] CI / build / release tooling
- [ ] Dependency update

## Area

<!-- Check all that apply. -->

- [ ] CLI (`cli/`)
- [ ] Ansible collection (`ansible/`)
- [ ] Terraform / Terragrunt (`infra/`, `modules/`)
- [ ] Packer / Warpgate (`packer/`, `warpgate-templates/`)
- [ ] Lab definitions (`ad/`)
- [ ] Extensions (`extensions/`)
- [ ] Variant generator / tools (`tools/`)
- [ ] Documentation (`docs/`, `README.md`, etc.)
- [ ] CI workflows (`.github/`)

## Related issues

<!-- Link any issues this PR closes or relates to, e.g. "Closes #123" -->

## How was this tested?

<!--
Tell reviewers what you actually ran. The more concrete the better.
Examples:
  - `cd cli && go test ./...`
  - `dreadgoad doctor`
  - `dreadgoad provision --lab GOAD-Light --provider virtualbox`, then
    `dreadgoad health-check` and `dreadgoad validate --quick`
  - Re-ran the AWS warpgate AMI build end-to-end on us-east-1
  - Linted Ansible with `ansible-lint ansible/`
-->

- Provider(s) tested:
- Lab(s) tested:
- Operator OS:

## Screenshots / logs (optional)

<!-- For UX changes, output changes, or anything visually interesting. -->

## Checklist

- [ ] I have read [CONTRIBUTING.md](../CONTRIBUTING.md).
- [ ] My changes follow the existing code style of the area I touched.
- [ ] I have added or updated tests where it makes sense (Go tests under `cli/`, Ansible syntax checks, etc.).
- [ ] I have updated documentation (README, `docs/`, role README, command help text) where relevant.
- [ ] I have checked that I am not committing real secrets, personal credentials, or internal hostnames. (Intentional lab credentials inside `ad/`, `ansible/`, and `extensions/` are expected and fine.)
- [ ] If this PR changes user-facing CLI behavior, I have updated the relevant `--help` text and any docs that reference it.
- [ ] If this PR introduces a breaking change, I have called it out in the **Summary** above.

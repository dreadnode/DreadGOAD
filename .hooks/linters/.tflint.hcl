plugin "aws" {
  enabled = true
  version = "0.47.0"
  source  = "github.com/terraform-linters/tflint-ruleset-aws"
  # `tflint --init` crashes (sigstore-go nil TlogEntries panic) verifying this
  # plugin's GitHub artifact attestation in CI. Skip attestation verification;
  # the plugin is still pinned by version and fetched over HTTPS from the
  # official release. Requires tflint >= v0.62.0 (see pre-commit.yaml).
  signature = "none"
}

rule "terraform_naming_convention" {
  enabled = true
}

rule "terraform_documented_outputs" {
  enabled = true
}

rule "terraform_documented_variables" {
  enabled = true
}

rule "terraform_unused_declarations" {
  enabled = true
}

locals {
  registry_path = "${get_repo_root()}/infra/azure/scope-range-deployment/host-registry.yaml"
  registry      = yamldecode(file(local.registry_path))

  path_parts       = split("/", get_terragrunt_dir())
  deployment_index = index(local.path_parts, "scope-range-deployment")
  relative_path = join("/", slice(
    local.path_parts,
    local.deployment_index + 3,
    length(local.path_parts),
  ))

  hosts_by_path = {
    for host_id, metadata in local.registry.hosts :
    metadata.terragrunt_path => merge(metadata, { host_id = host_id })
  }

  host = lookup(local.hosts_by_path, local.relative_path, null)
  _host_exists = regex(
    "^valid$",
    local.host == null ? "host ${local.relative_path} is missing from ${local.registry_path}" : "valid",
  )
}

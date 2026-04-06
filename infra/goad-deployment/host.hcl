locals {
  registry_path = find_in_parent_folders("host-registry.yaml")
  host_registry = yamldecode(file(local.registry_path))

  terragrunt_dir       = get_terragrunt_dir()
  terragrunt_dir_parts = split("/", local.terragrunt_dir)

  # Find the goad-deployment directory in the path to determine relative position
  deployment_index = try(
    index(local.terragrunt_dir_parts, "goad-deployment"),
    -1
  )

  path_structure_validation = local.deployment_index == -1 ? (
    <<-EOT

    ============================================================================
    ERROR: Not running from within goad-deployment directory structure!
    ============================================================================

    Current path: ${local.terragrunt_dir}

    This host.hcl file is designed to be used within the goad-deployment
    structure. Please ensure you are running Terragrunt from a directory within:

        infra/goad-deployment/{env}/{region}/{module}/

    ============================================================================

    EOT
  ) : "valid"

  _path_structure_check = regex("^valid$", local.path_structure_validation)

  # Extract the module path after {env}/{region}/
  path_after_region = slice(
    local.terragrunt_dir_parts,
    local.deployment_index + 3,
    length(local.terragrunt_dir_parts)
  )

  relative_path = join("/", local.path_after_region)

  host_metadata_map = {
    for hostname, metadata in local.host_registry.hosts :
    metadata.terragrunt_path => merge(metadata, {
      hostname = hostname
    })
  }

  host_lookup = lookup(local.host_metadata_map, local.relative_path, null)

  validation_message = local.host_lookup == null ? (
    <<-EOT

    ============================================================================
    ERROR: Host not found in registry!
    ============================================================================

    Current path: ${local.relative_path}

    Available hosts in registry:
    ${join("\n", [for path in keys(local.host_metadata_map) : "  - ${path}"])}

    Please ensure:
    1. The host is defined in host-registry.yaml
    2. The terragrunt_path matches your directory structure
    3. You are in the correct directory

    Registry location: ${local.registry_path}
    ============================================================================

    EOT
  ) : "valid"

  _ = regex("^valid$", local.validation_message)

  host = local.host_lookup

  hostname      = local.host.hostname
  computer_name = try(local.host.computer_name, local.host.hostname)
  goad_id       = try(local.host.goad_id, "")

  friendly_name = local.host.friendly_name
  role          = local.host.role
  description   = try(local.host.description, "")

  os              = local.host.os
  os_type         = local.host.os
  os_version      = try(local.host.os_version, "")
  os_distribution = try(local.host.os_distribution, "")

  domain = try(local.host.domain, "nodomain.local")

  tier   = local.host.tier
  groups = local.host.groups

  owner    = try(local.host.owner, null)
  notes    = try(local.host.notes, "")
  features = try(local.host.features, [])
  services = try(local.host.services, [])

  terragrunt_path = local.host.terragrunt_path

  is_windows           = local.os == "windows"
  is_linux             = local.os == "linux"
  is_goad              = contains(local.groups, "goad")
  is_domain_controller = contains(local.groups, "domain_controllers")
  windows_os_version   = local.is_windows ? try(local.host.os_version, "2019") : ""

  debug_info = {
    terragrunt_dir  = local.terragrunt_dir
    relative_path   = local.relative_path
    registry_path   = local.registry_path
    hostname        = local.hostname
    available_hosts = keys(local.host_metadata_map)
  }
}

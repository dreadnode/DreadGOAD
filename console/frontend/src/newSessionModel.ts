import type { AppConfig, ConfigListing, LabSummary } from './api'

// These values occupy <select> entries that cannot collide with absolute config
// paths. Keeping them here gives the modal and its loading hook one vocabulary.
export const NEW_CONFIG = ' new-config'
export const NEW_ENV = ' new-env'
export const OTHER_PATH = ' other-path'

export interface NewSessionModelInput {
  cfg: AppConfig
  listing: ConfigListing | null
  choice: string
  customPath: string
  newConfigName: string
  provider: string
  region: string
  envs: string[]
  configOk: boolean
  envChoice: string
  loadedProvider: string
  loadedProviders: Record<string, string>
  loadedRegions: Record<string, string | null>
  newEnv: string
  source: string
  labList: LabSummary[]
  variantName: string
  cidr: string
  submitting: boolean
}

interface NewEnvironmentFields {
  variant: true
  variant_source: string
  variant_target: string
  variant_name: string
  vpc_cidr: string
}

export type NewSessionPayload =
  | { config_path: string; env: string }
  | { mode: 'new'; config_path: string; env: string; env_fields: NewEnvironmentFields }
  | {
    mode: 'new_config'
    config_name: string
    provider: string
    region: string | undefined
    env: string
    env_fields: NewEnvironmentFields
  }

export interface NewSessionModel {
  creatingConfig: boolean
  creatingEnv: boolean
  configPath: string
  selectedConfigError: string
  effectiveProvider: string
  providers: string[]
  credentialHint: string
  environmentName: string
  environmentCollides: boolean
  configFilename: string
  configTaken: boolean
  sourceLab?: LabSummary
  sourceUnsupported: boolean
  variantTarget: string
  missingRegion: boolean
  valid: boolean
  payload: NewSessionPayload
  effects: string[]
}

/** Mirrors configstore.slug_for; the backend remains authoritative on create. */
export function slugForConfig(name: string): string {
  return name.trim().toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '').slice(0, 48)
}

export function labSupportsProvider(lab: LabSummary, provider: string): boolean {
  return !provider || lab.providers.length === 0 || lab.providers.includes(provider)
}

/**
 * Derive validation, submission data, and the operator-facing side-effect
 * preview from the modal's raw inputs. This function deliberately performs no
 * I/O, so all three remain synchronized and can be checked without rendering.
 */
export function deriveNewSessionModel(input: NewSessionModelInput): NewSessionModel {
  const creatingConfig = input.choice === NEW_CONFIG
  const creatingEnv = creatingConfig || input.envChoice === NEW_ENV
  const configPath = input.choice === OTHER_PATH ? input.customPath : input.choice
  const selected = input.listing?.configs.find(config => config.path === configPath)
  const effectiveProvider = creatingConfig
    ? input.provider
    : ((!creatingEnv && input.loadedProviders[input.envChoice])
      || input.loadedProvider || selected?.provider || 'aws')
  const providers = input.listing?.providers ?? input.cfg.providers ?? ['aws', 'azure']
  const credentialHint = input.listing?.credential_hints?.[effectiveProvider] || ''

  const environmentName = input.newEnv.trim()
  const environmentCollides = input.envs.includes(environmentName)
  const configSlug = slugForConfig(input.newConfigName)
  const configFilename = configSlug ? `${configSlug}.yaml` : ''
  // The UI identifies sessions by config basename, so duplicates are ambiguous
  // even when their absolute paths differ.
  const configTaken = !!configSlug && !!input.listing?.configs.some(
    config => config.name === configFilename,
  )

  // An empty provider list means discovery could not determine compatibility;
  // only a known mismatch is grounds to reject the lab.
  const sourceLab = input.labList.find(lab => lab.dir === input.source.trim())
  const sourceUnsupported = !!sourceLab && !labSupportsProvider(sourceLab, effectiveProvider)
  const variantBase = input.source.trim() || 'ad/GOAD'
  const effectiveVariant = input.variantName.trim() || environmentName
  const variantTarget = effectiveVariant ? `${variantBase}-${effectiveVariant}` : ''

  const environmentValid = creatingEnv
    ? (!!environmentName && !environmentCollides && !sourceUnsupported)
    : !!input.envChoice
  // Region is required here because the console does not supply a CLI override
  // and subsequent provider commands resolve it from the config.
  const configValid = creatingConfig
    ? (!!configSlug && !configTaken && providers.includes(input.provider) && !!input.region.trim())
    : input.configOk
  const missingRegion = !creatingConfig && input.configOk && !!input.envChoice
    && input.envChoice !== NEW_ENV && !input.loadedRegions[input.envChoice]
  const valid = configValid && environmentValid && !input.submitting

  const environmentFields: NewEnvironmentFields = {
    variant: true,
    variant_source: variantBase,
    variant_target: variantTarget,
    variant_name: effectiveVariant,
    vpc_cidr: input.cidr.trim() || '10.100.0.0/16',
  }

  const payload: NewSessionPayload = creatingConfig
    ? {
      mode: 'new_config',
      config_name: input.newConfigName.trim(),
      provider: input.provider,
      region: input.region.trim() || undefined,
      env: environmentName,
      env_fields: environmentFields,
    }
    : creatingEnv
      ? {
        mode: 'new',
        config_path: configPath,
        env: environmentName,
        env_fields: environmentFields,
      }
      : { config_path: configPath, env: input.envChoice }

  const effects: string[] = []
  if (creatingConfig && configFilename) {
    effects.push(`create ${input.listing?.configs_root ?? '…'}/${configFilename}`
      + ` (provider ${input.provider}${input.region.trim() ? `, region ${input.region.trim()}` : ''})`)
  }
  if (creatingEnv && environmentName) {
    effects.push(creatingConfig
      ? `define environment “${environmentName}” inside it`
      : `write environment “${environmentName}” into ${configPath} (a .bak is saved; comments and formatting are kept)`)
  } else if (!creatingEnv && input.envChoice) {
    effects.push(`attach to the existing environment “${input.envChoice}” — no files are written`)
  }
  if (creatingEnv && environmentName) {
    const regionLabel = creatingConfig
      ? input.region.trim()
      : (input.loadedRegions[input.envChoice] || '')
    effects.push(
      `scaffold infra/…/${environmentName}/${regionLabel || '<region>'}/ (terragrunt),`
      + ` generate the variant into ${variantTarget || 'ad/…'}/,`
      + ` and write ${environmentName}-inventory`,
    )
  }
  effects.push('deploy nothing — run /up in chat when you are ready')

  return {
    creatingConfig,
    creatingEnv,
    configPath,
    selectedConfigError: selected?.error || '',
    effectiveProvider,
    providers,
    credentialHint,
    environmentName,
    environmentCollides,
    configFilename,
    configTaken,
    sourceLab,
    sourceUnsupported,
    variantTarget,
    missingRegion,
    valid,
    payload,
    effects,
  }
}

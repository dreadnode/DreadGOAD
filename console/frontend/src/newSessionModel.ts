import type { EnvironmentSummary, LabSummary, SessionOptions } from './api'

export type SessionMode = 'existing' | 'new' | 'import'
export type RangeCustomization = 'standard' | 'randomized'

export interface NewSessionModelInput {
  options: SessionOptions | null
  mode: SessionMode
  existingIndex: string
  importPath: string
  importEnvironment: string
  importReady: boolean
  rangeName: string
  provider: string
  region: string
  environmentName: string
  customization: RangeCustomization
  cidr: string
  submitting: boolean
}

export interface NewSessionModel {
  range?: LabSummary
  existing?: EnvironmentSummary
  availableProviders: string[]
  effectiveProvider: string
  providerSettings?: LabSummary['provider_settings'][string]
  effectiveRegion: string
  effectiveCIDR: string
  cidrEditable: boolean
  variantAvailable: boolean
  credentialHint: string
  valid: boolean
  payload: Record<string, unknown>
  effects: string[]
}

/** Derive one range-first request and its operator-facing consequences. */
export function deriveNewSessionModel(input: NewSessionModelInput): NewSessionModel {
  const existingPosition = Number.parseInt(input.existingIndex, 10)
  const existing = Number.isInteger(existingPosition)
    ? input.options?.environments[existingPosition]
    : undefined
  const range = input.options?.ranges.find(item => item.name === input.rangeName)
  const consoleProviders = new Set(input.options?.providers ?? [])
  const availableProviders = Object.keys(range?.provider_settings ?? {})
    .filter(provider => consoleProviders.has(provider))
    .sort()
  const effectiveProvider = availableProviders.includes(input.provider)
    ? input.provider
    : (availableProviders.length === 1 ? availableProviders[0] : '')
  const providerSettings = range?.provider_settings[effectiveProvider]
  const effectiveRegion = input.region.trim() || providerSettings?.default_region || ''
  const cidrEditable = providerSettings?.network?.editable !== false
  const effectiveCIDR = cidrEditable
    ? input.cidr.trim()
    : (providerSettings?.network?.cidr || '')
  const variantAvailable = range?.variant_supported === true
  const environmentName = input.environmentName.trim()
  const customization = variantAvailable ? input.customization : 'standard'
  const credentialHint = input.options?.credential_hints[effectiveProvider] || ''

  const payload = input.mode === 'existing' && existing?.config_path
    ? { config_path: existing.config_path, env: existing.name }
    : input.mode === 'import'
      ? { config_path: input.importPath.trim(), env: input.importEnvironment }
      : {
      mode: 'create_range',
      range: range?.name || '',
      provider: effectiveProvider,
      region: effectiveRegion,
      env: environmentName,
      customization,
      ...(effectiveCIDR ? { vpc_cidr: effectiveCIDR } : {}),
    }

  const valid = !input.submitting && (input.mode === 'existing'
    ? !!existing?.config_path
    : input.mode === 'import'
      ? input.importReady && !!input.importPath.trim() && !!input.importEnvironment
      : !!range && !!effectiveProvider && !!providerSettings
        && !!effectiveRegion && !!environmentName)

  const effects: string[] = []
  if (input.mode === 'existing' && existing) {
    effects.push(`attach to “${existing.name}” (${existing.lab} · ${existing.provider}${existing.region ? ` · ${existing.region}` : ''})`)
    effects.push('write no configuration or infrastructure files')
  } else if (input.mode === 'import' && input.importReady && input.importEnvironment) {
    effects.push(`attach to “${input.importEnvironment}” from the imported config`)
    effects.push('write no configuration or infrastructure files')
  } else if (range && environmentName) {
    effects.push(`create a managed ${range.display_name || range.name} environment named “${environmentName}”`)
    effects.push(`${effectiveProvider || '<provider>'}${effectiveRegion ? ` in ${effectiveRegion}` : ''}${customization === 'randomized' ? ' with a randomized variant' : ''}`)
    effects.push('prepare configuration, infrastructure, and inventory; deploy nothing until /up')
  }

  return {
    range,
    existing,
    availableProviders,
    effectiveProvider,
    providerSettings,
    effectiveRegion,
    effectiveCIDR,
    cidrEditable,
    variantAvailable,
    credentialHint,
    valid,
    payload,
    effects,
  }
}

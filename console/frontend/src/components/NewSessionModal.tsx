import { useEffect, useMemo, useRef, useState } from 'react'
import { api } from '../api'
import { useNewSessionOptions } from '../hooks/useNewSessionOptions'
import {
  deriveNewSessionModel,
  type RangeCustomization,
  type SessionMode,
} from '../newSessionModel'
import Modal from './Modal'
import { Field, Select, btnStyle } from './FormFields'

export default function NewSessionModal({ onClose, onCreate }: {
  onClose: () => void
  onCreate: (body: Record<string, unknown>) => Promise<void>
}) {
  const { options, loading, error } = useNewSessionOptions()
  const [mode, setMode] = useState<SessionMode>('new')
  const [existingIndex, setExistingIndex] = useState('')
  const [importPath, setImportPath] = useState('')
  const [importEnvironments, setImportEnvironments] = useState<string[]>([])
  const [importEnvironment, setImportEnvironment] = useState('')
  const [importLoading, setImportLoading] = useState(false)
  const [importError, setImportError] = useState('')
  const importRequest = useRef(0)
  const [rangeName, setRangeName] = useState('')
  const [provider, setProvider] = useState('')
  const [region, setRegion] = useState('')
  const [environmentName, setEnvironmentName] = useState('')
  const [customization, setCustomization] = useState<RangeCustomization>('standard')
  const [cidr, setCIDR] = useState('')
  const [submitErr, setSubmitErr] = useState('')
  const [submitting, setSubmitting] = useState(false)

  const creatableRanges = useMemo(() => (options?.ranges ?? []).filter(
    range => Object.keys(range.provider_settings).some(
      candidate => options?.providers.includes(candidate),
    ),
  ), [options])

  const model = deriveNewSessionModel({
    options,
    mode,
    existingIndex,
    importPath,
    importEnvironment,
    importReady: !importLoading && !importError && importEnvironments.length > 0,
    rangeName,
    provider,
    region,
    environmentName,
    customization,
    cidr,
    submitting,
  })

  useEffect(() => {
    setProvider(model.availableProviders.length === 1 ? model.availableProviders[0] : '')
    setRegion('')
    setCustomization('standard')
    setCIDR('')
    // Only the selected range should reset its dependent fields. The derived
    // model is intentionally omitted to avoid resetting after each keystroke.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [rangeName])

  useEffect(() => {
    setRegion(model.providerSettings?.default_region || '')
    setCIDR(model.providerSettings?.network?.editable === false
      ? (model.providerSettings.network.cidr || '')
      : '')
    // Provider settings change exactly when provider or range changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [provider, rangeName])

  useEffect(() => {
    if (options?.environments.length && existingIndex === '') setExistingIndex('0')
  }, [existingIndex, options])

  const loadImportedConfig = async () => {
    const path = importPath.trim()
    const request = ++importRequest.current
    setImportEnvironments([])
    setImportEnvironment('')
    setImportError('')
    if (!path) return
    setImportLoading(true)
    try {
      const result = await api.environments(path)
      if (request !== importRequest.current) return
      setImportEnvironments(result.environments)
      setImportEnvironment(result.environments[0] || '')
      if (!result.environments.length) setImportError('This config defines no environments.')
    } catch (reason) {
      if (request !== importRequest.current) return
      setImportError(reason instanceof Error ? reason.message : String(reason))
    } finally {
      if (request === importRequest.current) setImportLoading(false)
    }
  }

  const submit = async () => {
    if (!model.valid) return
    setSubmitErr('')
    setSubmitting(true)
    try {
      await onCreate(model.payload)
    } catch (reason) {
      setSubmitErr(reason instanceof Error ? reason.message : String(reason))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Modal onClose={onClose} width={460} maxHeight="86vh" ariaLabel="New Session">
      <div style={{ color: 'var(--dg-brand)', fontWeight: 700, fontSize: 13, marginBottom: 4 }}>New Session</div>
      <div style={{ color: 'var(--dn-text-muted)', fontSize: 11, marginBottom: 16 }}>
        Attach to an existing environment or prepare a new range. Configuration files are managed automatically.
      </div>

      <Select
        label="Start with"
        value={mode}
        onChange={value => setMode(value as SessionMode)}
        options={[
          { value: 'new', label: 'Create a new environment' },
          { value: 'existing', label: 'Use an existing environment', disabled: !options?.environments.length },
          { value: 'import', label: 'Import a config path (advanced)' },
        ]}
      />

      {loading && <div style={{ color: 'var(--dn-text-dim)', fontSize: 11, marginBottom: 12 }}>Loading ranges…</div>}
      {error && <div style={{ color: 'var(--dn-error)', fontSize: 11, marginBottom: 12 }}>{error}</div>}

      {mode === 'existing' ? (
        <Select
          label="Environment"
          value={existingIndex}
          onChange={setExistingIndex}
          disabled={loading}
          options={(options?.environments ?? []).map((environment, index) => ({
            value: String(index),
            label: `${environment.name} — ${environment.lab} · ${environment.provider}${environment.region ? ` · ${environment.region}` : ''}${environment.config_name ? ` · ${environment.config_name}` : ''}`,
          }))}
        />
      ) : mode === 'import' ? (
        <>
          <Field
            label="Config path"
            value={importPath}
            onChange={value => {
              importRequest.current += 1
              setImportPath(value)
              setImportLoading(false)
              setImportEnvironments([])
              setImportEnvironment('')
              setImportError('')
            }}
            onBlur={() => { void loadImportedConfig() }}
            placeholder="/absolute/path/to/dreadgoad.yaml"
          />
          <Select
            label={`Environment${importLoading ? ' (loading…)' : ''}`}
            value={importEnvironment}
            onChange={setImportEnvironment}
            disabled={importLoading || importEnvironments.length === 0}
            options={importEnvironments.map(value => ({ value, label: value }))}
          />
          {importError && (
            <div style={{ color: 'var(--dn-error)', fontSize: 11, marginTop: -8, marginBottom: 12 }}>
              {importError}
            </div>
          )}
        </>
      ) : (
        <>
          <Select
            label="Range"
            value={rangeName}
            onChange={setRangeName}
            disabled={loading}
            options={[
              { value: '', label: 'Select a range…', disabled: true },
              ...creatableRanges.map(range => ({
                value: range.name,
                label: range.display_name || range.name,
              })),
            ]}
          />

          {!!rangeName && (
            <>
              <Select
                label="Provider"
                value={model.effectiveProvider}
                onChange={setProvider}
                options={[
                  ...(model.availableProviders.length > 1
                    ? [{ value: '', label: 'Select a provider…', disabled: true }]
                    : []),
                  ...model.availableProviders.map(value => ({ value, label: value })),
                ]}
              />
              {!!model.effectiveProvider && (
                <>
                  <Field
                    label="Region"
                    value={region}
                    onChange={setRegion}
                    placeholder={model.effectiveProvider === 'azure' ? 'e.g. centralus' : 'e.g. us-west-1'}
                    suggestions={options?.regions[model.effectiveProvider]}
                  />
                  <Field
                    label="Environment name"
                    value={environmentName}
                    onChange={setEnvironmentName}
                    placeholder="e.g. kraken"
                  />
                  {model.variantAvailable && (
                    <Select
                      label="Customization"
                      value={customization}
                      onChange={value => setCustomization(value as RangeCustomization)}
                      options={[
                        { value: 'standard', label: 'Standard range' },
                        { value: 'randomized', label: 'Randomized variant' },
                      ]}
                    />
                  )}
                  {model.cidrEditable ? (
                    <Field
                      label="VPC/VNet CIDR (optional)"
                      value={cidr}
                      onChange={setCIDR}
                      placeholder="Automatically assigned from the environment name"
                    />
                  ) : model.effectiveCIDR ? (
                    <div style={{ color: 'var(--dn-text-dim)', fontSize: 11, marginBottom: 12 }}>
                      Network: {model.effectiveCIDR} (fixed by this range)
                    </div>
                  ) : null}
                </>
              )}
            </>
          )}
        </>
      )}

      {model.credentialHint && (
        <div style={{ color: 'var(--dn-warning)', fontSize: 11, marginBottom: 12 }}>
          Credentials not detected; you may need {model.credentialHint} before /up.
        </div>
      )}

      {!!model.effects.length && (
        <div style={{ color: 'var(--dn-text-dim)', fontSize: 10, marginBottom: 12 }}>
          {model.effects.map(effect => <div key={effect}>• {effect}</div>)}
        </div>
      )}
      {submitErr && <div style={{ color: 'var(--dn-error)', fontSize: 11, marginBottom: 12 }}>{submitErr}</div>}

      <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8 }}>
        <button type="button" style={btnStyle(false)} onClick={onClose}>Cancel</button>
        <button type="button" style={btnStyle(true)} disabled={!model.valid} onClick={() => { void submit() }}>
          {submitting ? 'Preparing…' : mode === 'new' ? 'Prepare range' : 'Start session'}
        </button>
      </div>
    </Modal>
  )
}

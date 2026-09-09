import { useState } from 'react'
import type { AppConfig } from '../api'
import { useNewSessionOptions } from '../hooks/useNewSessionOptions'
import {
  NEW_CONFIG,
  NEW_ENV,
  OTHER_PATH,
  deriveNewSessionModel,
  labSupportsProvider,
} from '../newSessionModel'
import Modal from './Modal'
import { Field, Select, btnStyle } from './FormFields'

export default function NewSessionModal({ cfg, onClose, onCreate }: {
  cfg: AppConfig
  onClose: () => void
  onCreate: (body: Record<string, unknown>) => Promise<void>
}) {
  // --- config step ---
  const [choice, setChoice] = useState<string>(cfg.default_config_path)
  const [customPath, setCustomPath] = useState('')
  const [newConfigName, setNewConfigName] = useState('')
  const [provider, setProvider] = useState('aws')
  const [region, setRegion] = useState('')

  // --- environment step ---
  const [newEnv, setNewEnv] = useState('')
  const [variantName, setVariantName] = useState('')
  const [cidr, setCidr] = useState('10.100.0.0/16')

  const [submitErr, setSubmitErr] = useState('')
  const [submitting, setSubmitting] = useState(false)

  const creatingConfig = choice === NEW_CONFIG
  const {
    listing,
    envs,
    envErr,
    configOk,
    loading,
    envChoice,
    setEnvChoice,
    loadedProvider,
    loadedRegions,
    labList,
    source,
    setSource,
    loadEnvs,
  } = useNewSessionOptions(choice, customPath, creatingConfig)

  const model = deriveNewSessionModel({
    cfg,
    listing,
    choice,
    customPath,
    newConfigName,
    provider,
    region,
    envs,
    configOk,
    envChoice,
    loadedProvider,
    loadedRegions,
    newEnv,
    source,
    labList,
    variantName,
    cidr,
    submitting,
  })
  const {
    creatingEnv,
    selectedConfigError,
    effectiveProvider,
    providers,
    credentialHint,
    environmentName: nm,
    environmentCollides: collides,
    configFilename,
    configTaken,
    sourceLab,
    sourceUnsupported,
    variantTarget,
    missingRegion,
    valid,
    effects,
  } = model

  const submit = async () => {
    if (!valid) return
    setSubmitErr('')
    setSubmitting(true)
    try {
      await onCreate(model.payload)
    } catch (e) {
      // Previously this rejection was unhandled: the modal stayed open with no
      // explanation and the same click kept failing.
      setSubmitErr(e instanceof Error ? e.message : String(e))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Modal onClose={onClose} width={460} maxHeight="86vh" ariaLabel="New Session">
        <div style={{ color: 'var(--dg-brand)', fontWeight: 700, fontSize: 13, marginBottom: 4 }}>New Session</div>
        <div style={{ color: 'var(--dn-text-muted)', fontSize: 11, marginBottom: 16 }}>
          A session is a tab you talk to. It can create the config and environment it needs.
        </div>

        {/* --- 1. Config --- */}
        <Select
          label="Config"
          value={choice}
          onChange={setChoice}
          options={[
            ...(listing?.configs ?? []).map(c => ({
              value: c.path,
              label: `${c.name}${c.provider ? ` (${c.provider})` : ''}${c.error ? ' — unreadable' : ''}`
                + (c.source === 'default' ? ' · default' : ''),
            })),
            { value: NEW_CONFIG, label: '＋ New config…' },
            { value: OTHER_PATH, label: 'Other path…' },
          ]}
        />
        {selectedConfigError && (
          <div style={{ color: 'var(--dn-error)', fontSize: 11, marginTop: -8, marginBottom: 12 }}>{selectedConfigError}</div>
        )}

        {choice === OTHER_PATH && (
          <Field label="Config path" value={customPath} onChange={setCustomPath} onBlur={() => { void loadEnvs(customPath) }} placeholder="/abs/path/to/dreadgoad.yaml" />
        )}

        {creatingConfig && (
          <>
            <Field label="Config name" value={newConfigName} onChange={setNewConfigName} placeholder="e.g. azure-lab" />
            {configFilename && (
              <div style={{ color: configTaken ? 'var(--dn-error)' : 'var(--dn-text-dim)', fontSize: 10, marginTop: -8, marginBottom: 12 }}>
                {configTaken ? `${configFilename} already exists — pick another name` : `→ ${configFilename}`}
              </div>
            )}
            <Select
              label="Provider"
              value={provider}
              onChange={setProvider}
              options={[
                ...providers.map(p => ({ value: p, label: p })),
                // Listed but unselectable: the CLI deploys these, the console
                // cannot render or connect to them (connect.ts:90), and leaving
                // them out entirely reads as "not supported at all".
                { value: 'proxmox', label: 'proxmox — CLI only, no console support', disabled: true },
                { value: 'ludus', label: 'ludus — CLI only, no console support', disabled: true },
              ]}
            />
            <Field label="Region" value={region} onChange={setRegion}
              placeholder={provider === 'azure' ? 'e.g. eastus' : 'e.g. us-east-1'}
              suggestions={listing?.regions?.[provider]} />
            {!region.trim() && (
              <div style={{ color: 'var(--dn-warning)', fontSize: 11, marginTop: -8, marginBottom: 12 }}>
                Required — every {provider} command resolves its region from this config.
              </div>
            )}
          </>
        )}

        {/* --- 2. Environment --- */}
        {!creatingConfig && (
          <Select
            label={`Environment${loading ? ' (loading…)' : ''}`}
            value={envChoice}
            onChange={setEnvChoice}
            disabled={loading || !configOk}
            options={[
              ...envs.map(n => ({ value: n, label: n })),
              { value: NEW_ENV, label: '＋ New environment…' },
            ]}
          />
        )}
        {envErr && <div style={{ color: 'var(--dn-error)', fontSize: 11, marginTop: -8, marginBottom: 12 }}>{envErr}</div>}

        {creatingEnv && (
          <>
            <Field label="New environment name" value={newEnv} onChange={setNewEnv} placeholder="e.g. redteam" />
            {nm && collides && <div style={{ color: 'var(--dn-error)', fontSize: 11, marginTop: -8, marginBottom: 12 }}>“{nm}” already exists in this config</div>}
            {labList.length > 0 ? (
              <Select
                label="Variant source (base lab)"
                value={source}
                onChange={setSource}
                options={labList.map(l => ({
                  value: l.dir,
                  // The host count is the difference between these labs — GOAD
                  // is five hosts, GOAD-Mini is one — and it was invisible while
                  // this was a path you had to already know.
                  label: `${l.name} — ${l.hosts.length} host${l.hosts.length === 1 ? '' : 's'}`
                    + (l.generated ? ' (generated variant)' : '')
                    + (labSupportsProvider(l, effectiveProvider) ? '' : ` — no ${effectiveProvider} support`),
                  // A lab with no terraform for this provider deploys nothing.
                  // Disabled rather than hidden so the reason is legible.
                  disabled: !labSupportsProvider(l, effectiveProvider),
                }))}
              />
            ) : (
              <Field label="Variant source (base lab)" value={source} onChange={setSource} placeholder="ad/GOAD" />
            )}
            {sourceUnsupported && (
              <div style={{ color: 'var(--dn-error)', fontSize: 11, marginTop: -8, marginBottom: 12 }}>
                {sourceLab?.name} ships no {effectiveProvider} terraform
                (has: {sourceLab?.providers.join(', ') || 'none'}) — it cannot be deployed by this config.
              </div>
            )}
            <Field label="Variant name" value={variantName} onChange={setVariantName} placeholder={nm || 'defaults to the environment name'} />
            {variantTarget && (
              <div style={{ color: 'var(--dn-text-dim)', fontSize: 10, marginTop: -8, marginBottom: 12 }}>
                → generated into {variantTarget}/
              </div>
            )}
            <Field label="VPC CIDR" value={cidr} onChange={setCidr} placeholder="10.100.0.0/16" />
          </>
        )}

        {/* --- what CREATE will actually do --- */}
        <div style={{ border: '1px solid var(--dn-border)', borderRadius: 3, padding: '8px 10px', marginBottom: 12, background: 'var(--dn-surface-alt)' }}>
          <div style={{ color: 'var(--dn-text-bright)', fontWeight: 700, fontSize: 11, marginBottom: 4 }}>This will</div>
          <ul style={{ margin: 0, paddingLeft: 16, color: 'var(--dn-text-muted)', fontSize: 11 }}>
            {effects.map(line => <li key={line} style={{ marginBottom: 2 }}>{line}</li>)}
          </ul>
          {missingRegion && (
            <div style={{ color: 'var(--dn-warning)', fontSize: 11, marginTop: 6 }}>
              ⚠ This config sets no region, so {effectiveProvider || 'provider'} commands
              will fail with “region not configured”. Add <code>region:</code> to it before deploying.
            </div>
          )}
          {credentialHint && (
            <div style={{ color: 'var(--dn-warning)', fontSize: 11, marginTop: 6 }}>⚠ {credentialHint}</div>
          )}
        </div>

        {submitErr && <div style={{ color: 'var(--dn-error)', fontSize: 11, marginBottom: 8 }}>{submitErr}</div>}

        <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8, marginTop: 8 }}>
          <button onClick={onClose} style={btnStyle(false)}>CANCEL</button>
          <button
            onClick={submit}
            disabled={!valid}
            style={{ ...btnStyle(true), opacity: valid ? 1 : 0.5, cursor: valid ? 'pointer' : 'not-allowed' }}
          >{submitting ? 'CREATING…' : 'CREATE'}</button>
        </div>
    </Modal>
  )
}

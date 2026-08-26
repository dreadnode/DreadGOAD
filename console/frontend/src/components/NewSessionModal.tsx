import { useCallback, useEffect, useState } from 'react'
import { api, type AppConfig, type ConfigListing, type LabSummary } from '../api'
import Modal from './Modal'
import { Field, Select, btnStyle } from './FormFields'

// Sentinels for the two config-picker entries that aren't a path. Prefixed so
// they can never collide with a real absolute path, which always starts "/".
const NEW_CONFIG = ' new-config'
// Distinct from NEW_CONFIG despite never appearing in the same <select>:
// they mean different things, and `envChoice === NEW_CONFIG` would read as
// a bug even when it isn't one.
const NEW_ENV = ' new-env'
const OTHER_PATH = ' other-path'

/**
 * Mirror of configstore.slug_for, for previewing the filename before it exists.
 *
 * Kept in step by hand — the backend is the authority and re-derives it on
 * create, so a drift here shows the operator a filename slightly different from
 * the one they get, rather than writing to the wrong place.
 */
function slugFor(name: string): string {
  return name.trim().toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '').slice(0, 48)
}

export default function NewSessionModal({ cfg, onClose, onCreate }: {
  cfg: AppConfig
  onClose: () => void
  onCreate: (body: Record<string, unknown>) => Promise<void>
}) {
  // --- config step ---
  const [listing, setListing] = useState<ConfigListing | null>(null)
  const [choice, setChoice] = useState<string>(cfg.default_config_path)
  const [customPath, setCustomPath] = useState('')
  const [newConfigName, setNewConfigName] = useState('')
  const [provider, setProvider] = useState('aws')
  const [region, setRegion] = useState('')

  // --- environment step ---
  const [envs, setEnvs] = useState<string[]>([])
  const [envErr, setEnvErr] = useState('')
  const [configOk, setConfigOk] = useState(false)
  const [loading, setLoading] = useState(false)
  const [envChoice, setEnvChoice] = useState('')
  const [loadedProvider, setLoadedProvider] = useState('')
  // Per-env resolved regions, not just the file-level key: a config can declare
  // regions per environment and none at the top, which resolves fine.
  const [loadedRegions, setLoadedRegions] = useState<Record<string, string | null>>({})
  // The path loadEnvs last read successfully. Distinct from `configPath`, which
  // under "Other path…" is whatever is currently in the text box — including
  // half-typed. Anything expensive keys off this instead.
  const [loadedPath, setLoadedPath] = useState('')
  const [newEnv, setNewEnv] = useState('')
  const [source, setSource] = useState('ad/GOAD')
  const [labList, setLabList] = useState<LabSummary[]>([])
  const [variantName, setVariantName] = useState('')
  const [cidr, setCidr] = useState('10.100.0.0/16')

  const [submitErr, setSubmitErr] = useState('')
  const [submitting, setSubmitting] = useState(false)

  const creatingConfig = choice === NEW_CONFIG
  // A brand-new config has no environments in it, so its first one is always
  // being defined here — there is nothing to attach to yet.
  const creatingEnv = creatingConfig || envChoice === NEW_ENV
  const configPath = choice === OTHER_PATH ? customPath : choice
  // What the lab lookup keys on. '' means "no config — use the repo root";
  // null means "nothing settled yet, don't ask". Never the raw `configPath`,
  // which is mid-typing under "Other path…".
  const labsKey: string | null = creatingConfig ? '' : (loadedPath || null)

  useEffect(() => {
    api.configs().then(setListing).catch(() => {})
  }, [])

  // Re-read per chosen config: labs live under that config's own project root,
  // so pointing at another checkout must not offer this repo's `ad/` contents.
  // An empty result is not an error state here — the field falls back to being
  // free text, which is what it was before.
  //
  // Keyed on `choice`, NOT on `configPath`. The latter resolves to `customPath`
  // under "Other path…", which changes on every keystroke — and each change
  // spawns a `dreadgoad lab list` subprocess, measured at ~260ms. Typing a
  // 45-character path would have fired 45 of them. `loadEnvs` already avoids
  // this by loading on blur; this rides the same signal via `labsKey`.
  useEffect(() => {
    if (labsKey === null) return
    let cancelled = false
    api.labs(labsKey || undefined)
      .then(r => {
        if (cancelled) return
        setLabList(r.labs)
        // A <select> whose value matches no <option> renders the first option
        // while state keeps the unmatched value — the form would then show one
        // lab and submit another. `ad/GOAD` is only the default because it is
        // the usual base lab; a config in a tree without one must not silently
        // keep it. Only snap when the list is non-empty, so the free-text
        // fallback keeps whatever was typed.
        setSource(prev => (r.labs.length && !r.labs.some(l => l.dir === prev)
          ? r.labs[0].dir
          : prev))
      })
      .catch(() => { if (!cancelled) setLabList([]) })
    // Responses can land out of order when the config changes mid-flight; the
    // last one to *return* would otherwise win over the last one requested.
    return () => { cancelled = true }
  }, [labsKey])

  // Read the chosen config's environments. Always re-read rather than trusting
  // the listing's copy: the file is on disk and a `dreadgoad env add` between
  // opening this modal and using it would otherwise go unseen.
  const loadEnvs = useCallback(async (path: string, signal?: { cancelled: boolean }) => {
    if (!path.trim()) {
      setEnvs([]); setEnvChoice(''); setConfigOk(false); setLoadedProvider(''); setLoadedRegions({}); setLoadedPath('')
      setEnvErr('config path is required')
      return
    }
    setLoading(true)
    setEnvErr('')
    try {
      const r = await api.environments(path.trim())
      if (signal?.cancelled) return
      setEnvs(r.environments)
      setConfigOk(true)
      setLoadedPath(path.trim())
      setLoadedProvider(r.provider || '')
      setLoadedRegions(r.env_regions || {})
      setEnvChoice(prev => (r.environments.includes(prev) ? prev : (r.environments[0] || NEW_ENV)))
    } catch (e) {
      if (signal?.cancelled) return
      setEnvs([]); setEnvChoice(''); setConfigOk(false); setLoadedProvider(''); setLoadedRegions({}); setLoadedPath('')
      setEnvErr(e instanceof Error ? e.message : String(e))
    } finally {
      if (!signal?.cancelled) setLoading(false)
    }
  }, [])

  useEffect(() => {
    if (creatingConfig) { setEnvs([]); setConfigOk(true); setEnvErr(''); return }
    const signal = { cancelled: false }
    if (choice === OTHER_PATH) {
      setEnvs([]); setEnvChoice(''); setConfigOk(false); setLoadedProvider(''); setLoadedRegions({}); setLoadedPath('')
      setEnvErr(customPath.trim() ? '' : 'enter a path, then click away to load it')
      if (customPath.trim()) loadEnvs(customPath, signal)
      return () => { signal.cancelled = true }
    }
    loadEnvs(choice, signal)
    return () => { signal.cancelled = true }
    // customPath is deliberately not a dependency: it would re-request on every
    // keystroke. The field loads on blur instead.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [choice, creatingConfig, loadEnvs])

  // Provider is a property of the config, so an existing one dictates it and it
  // is shown rather than asked. This is what makes the file-level provider key
  // (config.go:94) safe to expose: picking a provider can only ever apply to a
  // config being created, never silently re-point the environments already in
  // one that exists.
  const selected = listing?.configs.find(c => c.path === configPath)
  // loadedProvider before the listing's copy: it was read from the file just
  // now, and a hand-typed path has no listing entry at all.
  const effectiveProvider = creatingConfig ? provider : (loadedProvider || selected?.provider || '')
  const providers = listing?.providers ?? cfg.providers ?? ['aws', 'azure']
  const credentialHint = listing?.credential_hints?.[effectiveProvider] || ''

  const nm = newEnv.trim()
  const collides = envs.includes(nm)
  const configSlug = slugFor(newConfigName)
  const configFilename = configSlug ? `${configSlug}.yaml` : ''
  // Compared against EVERY known config, not just console-created ones, and by
  // basename rather than full path. Two configs whose filenames match are
  // indistinguishable in the UI even when they live in different directories:
  // the tab is named from the config's stem, and nothing else on screen shows
  // which config a session belongs to. Naming a new config "dreadgoad" next to
  // the repo-root dreadgoad.yaml produced two tabs reading "dreadgoad/staging"
  // with no way to tell them apart.
  const configTaken = !!configSlug && !!listing?.configs.some(
    c => c.name === configFilename,
  )

  // A lab ships terraform per provider (ad/<lab>/providers/<name>/), so one
  // without the session's provider cannot be deployed by it. Labs discovered
  // before a provider is known are all treated as usable rather than all
  // rejected — an empty provider is "not yet chosen", not "supports nothing".
  // The same goes for a lab reporting no providers at all: that means discovery
  // could not tell, which is not evidence of incompatibility.
  const labSupportsProvider = (lab: LabSummary) =>
    !effectiveProvider || lab.providers.length === 0 || lab.providers.includes(effectiveProvider)

  // Re-checked against the *current* provider rather than trusting the disabled
  // options: picking a lab and then changing the provider is a path no disabled
  // <option> can intercept.
  const sourceLab = labList.find(l => l.dir === source.trim())
  const sourceUnsupported = !!sourceLab && !labSupportsProvider(sourceLab)

  // variant_target and variant_name were two fields, and the second one was
  // decorative: the generator uses VariantName for a console banner and the
  // generated README heading only (variant/generator.go:271,1368) — it is not a
  // randomisation seed, so it cannot change what gets built. The directory is
  // what matters, and it is mechanical, so the name is asked for and the path
  // derived from it.
  //
  // Derived from `source`, not a literal "ad/GOAD-" — the old default hardcoded
  // the GOAD prefix even when the base lab was something else, which produced a
  // GOAD-named directory holding a variant of a different lab.
  const variantBase = source.trim() || 'ad/GOAD'
  const effectiveVariant = variantName.trim() || nm
  const variantTarget = effectiveVariant ? `${variantBase}-${effectiveVariant}` : ''

  const envValid = creatingEnv
    ? (!!nm && !collides && !sourceUnsupported)
    : !!envChoice
  // Region is REQUIRED, not optional, despite being an optional key in the
  // schema. Config.ResolveRegion (config.go:474-485) errors without one, the
  // console never passes --region (commands.py:347 actively refuses it), and
  // DREADGOAD_REGION is not something a GUI user is expected to have exported.
  // Labelling it optional produced a config that created cleanly and then
  // failed every single command against it — both `lab status` and `up`.
  const configValid = creatingConfig
    ? (!!configSlug && !configTaken && providers.includes(provider) && !!region.trim())
    : configOk

  // The same hole, reached through an existing config that has no region. Not
  // blocking here: the file is the operator's, and it may be deliberate for a
  // provider that doesn't need one. Saying so beats a deploy that fails later.
  const missingRegion = !creatingConfig && configOk && !!envChoice
    && envChoice !== NEW_ENV && !loadedRegions[envChoice]
  const valid = configValid && envValid && !submitting

  const envFields = () => (creatingEnv
    ? {
      variant: true,
      variant_source: variantBase,
      variant_target: variantTarget,
      variant_name: effectiveVariant,
      vpc_cidr: cidr.trim() || '10.100.0.0/16',
    }
    : undefined)

  const submit = async () => {
    if (!valid) return
    setSubmitErr('')
    setSubmitting(true)
    try {
      if (creatingConfig) {
        await onCreate({
          mode: 'new_config',
          config_name: newConfigName.trim(),
          provider,
          region: region.trim() || undefined,
          env: nm,
          env_fields: envFields(),
        })
      } else if (creatingEnv) {
        await onCreate({
          mode: 'new',
          config_path: configPath,
          env: nm,
          env_fields: envFields(),
        })
      } else {
        await onCreate({ config_path: configPath, env: envChoice })
      }
    } catch (e) {
      // Previously this rejection was unhandled: the modal stayed open with no
      // explanation and the same click kept failing.
      setSubmitErr(e instanceof Error ? e.message : String(e))
    } finally {
      setSubmitting(false)
    }
  }

  // What CREATE is about to do on disk, in the order it happens. Spelled out
  // because none of it is guessable from the button: two of the three steps
  // write to files that outlive the session, and the last one is the absence of
  // the thing "environment" most implies.
  const effects: string[] = []
  if (creatingConfig && configFilename) {
    effects.push(`create ${listing?.configs_root ?? '…'}/${configFilename}`
      + ` (provider ${provider}${region.trim() ? `, region ${region.trim()}` : ''})`)
  }
  if (creatingEnv && nm) {
    effects.push(creatingConfig
      ? `define environment “${nm}” inside it`
      : `write environment “${nm}” into ${configPath} (a .bak is saved; comments and formatting are kept)`)
  } else if (!creatingEnv && envChoice) {
    // The pure-attach case writes nothing at all; saying so is the whole point
    // of this box, which otherwise only ever appears when something is created.
    effects.push(`attach to the existing environment “${envChoice}” — no files are written`)
  }
  if (creatingEnv && nm) {
    // By far the largest thing CREATE does, and it was going entirely unsaid:
    // a terragrunt tree, a randomised copy of the base lab, and an inventory.
    // `dreadgoad env create` does the work; the console only asks for it.
    const regionLabel = creatingConfig ? region.trim() : (loadedRegions[envChoice] || '')
    effects.push(
      `scaffold infra/…/${nm}/${regionLabel || '<region>'}/ (terragrunt),`
      + ` generate the variant into ${variantTarget || 'ad/…'}/,`
      + ` and write ${nm}-inventory`,
    )
  }
  effects.push('deploy nothing — run /up in chat when you are ready')

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
        {selected?.error && (
          <div style={{ color: 'var(--dn-error)', fontSize: 11, marginTop: -8, marginBottom: 12 }}>{selected.error}</div>
        )}

        {choice === OTHER_PATH && (
          <Field label="Config path" value={customPath} onChange={setCustomPath} onBlur={() => loadEnvs(customPath)} placeholder="/abs/path/to/dreadgoad.yaml" />
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
                    + (labSupportsProvider(l) ? '' : ` — no ${effectiveProvider} support`),
                  // A lab with no terraform for this provider deploys nothing.
                  // Disabled rather than hidden so the reason is legible.
                  disabled: !labSupportsProvider(l),
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

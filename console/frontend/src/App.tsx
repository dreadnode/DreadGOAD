import { useCallback, useEffect, useId, useRef, useState } from 'react'
import TerminalChat from './components/TerminalChat'
import RangeView from './components/RangeView'
import { useWebSocket } from './hooks/useWebSocket'
import { api, type AppConfig, type ConfigListing, type LabSummary } from './api'
import type { ChatEvent, Session } from './types'

const MIN_W = 320
const DEFAULT_RATIO = 0.45

// Monotonic client-side id → stable React keys for chat events (F3).
let _cid = 0
const withCid = (ev: ChatEvent): ChatEvent => ({ ...ev, _cid: ++_cid })

export default function App() {
  const [sessions, setSessions] = useState<Session[]>([])
  const [activeId, setActiveId] = useState<string | null>(null)
  const [msgs, setMsgs] = useState<Record<string, ChatEvent[]>>({})
  const [cfg, setCfg] = useState<AppConfig | null>(null)
  const [ratio, setRatio] = useState(DEFAULT_RATIO)
  const [showNew, setShowNew] = useState(false)
  const [showSettings, setShowSettings] = useState(false)
  // Per-session counter bumped when the range changes, so RangeView re-fetches (F1).
  const [rangeRefresh, setRangeRefresh] = useState<Record<string, number>>({})
  // Per-session "a turn is in flight" flag → drives the cancel affordance.
  const [processing, setProcessing] = useState<Record<string, boolean>>({})
  // In-flight command name per session → warn before cancelling a destructive one.
  const [procCmd, setProcCmd] = useState<Record<string, string>>({})
  // When the in-flight turn started (epoch ms), so the elapsed timer survives a
  // reload instead of restarting from zero. 0 means idle.
  const [turnStart, setTurnStart] = useState<Record<string, number>>({})
  // Seed for the "Agent <verb>" flavour word, one per session per turn. Held
  // here rather than in TerminalChat because that component is not keyed by
  // session: switching tabs changes its `processing` prop true→false→true, and
  // a latch living inside it would re-roll the word for a turn already running.
  const [verbSeed, setVerbSeed] = useState<Record<string, number>>({})

  const sessionsRef = useRef<Session[]>([])
  const resumedRef = useRef<Set<string>>(new Set())
  const containerRef = useRef<HTMLDivElement>(null)
  sessionsRef.current = sessions

  // --- WebSocket (single, multiplexed by session_id) ---
  const handleMessage = useCallback((data: string) => {
    let ev: ChatEvent
    try { ev = JSON.parse(data) } catch { return }
    const sid = ev.session_id
    if (!sid) return
    if (ev.kind === 'history') {
      const events = (ev.events || []).map(withCid)
      setMsgs(prev => ({ ...prev, [sid]: events }))
      // The server reports whether a turn is still running. A turn survives a
      // disconnect, so after a reload this is the only way the pane learns it
      // isn't idle — otherwise the working indicator and the cancel affordance
      // both go missing while a deploy is mid-flight.
      const running = ev.active === true
      setProcessing(prev => ({ ...prev, [sid]: running }))
      setProcCmd(prev => ({ ...prev, [sid]: running ? (ev.command as string) || '' : '' }))
      setVerbSeed(prev => ({
        ...prev,
        [sid]: running ? prev[sid] || Date.now() : 0,
      }))
      setTurnStart(prev => {
        const at = running ? Date.parse((ev.started_at as string) || '') : NaN
        // Fall back to now if the timestamp is unusable, so the timer starts
        // from zero rather than rendering a nonsense duration.
        return { ...prev, [sid]: running ? (Number.isNaN(at) ? Date.now() : at) : 0 }
      })
      return
    }
    setMsgs(prev => ({ ...prev, [sid]: [...(prev[sid] || []), withCid(ev)] }))
    // A check_run means the hook refreshed the range doc → make RangeView re-fetch.
    if (ev.kind === 'check_run') {
      setRangeRefresh(prev => ({ ...prev, [sid]: (prev[sid] || 0) + 1 }))
      // The same hook also learns *session*-level facts — the cloud account,
      // resource group and attack box are only knowable post-deploy and get
      // written to the snapshot. Sessions were otherwise fetched once at mount,
      // so those fields never surfaced until a full page reload.
      api.listSessions().then(d => setSessions(d.sessions)).catch(() => {})
    }
    if (ev.kind === 'command_run' && ev.phase === 'start' && typeof ev.command === 'string') {
      setProcCmd(prev => ({ ...prev, [sid]: ev.command as string }))
    }
    if (ev.kind === 'agent_end') {
      setProcessing(prev => ({ ...prev, [sid]: false }))
      setProcCmd(prev => ({ ...prev, [sid]: '' }))
      setTurnStart(prev => ({ ...prev, [sid]: 0 }))
      setVerbSeed(prev => ({ ...prev, [sid]: 0 }))
    }
  }, [])

  const resume = useCallback((send: (d: string) => void, id: string) => {
    if (resumedRef.current.has(id)) return
    resumedRef.current.add(id)
    send(JSON.stringify({ type: 'resume', session_id: id }))
  }, [])

  const handleOpen = useCallback((send: (d: string) => void) => {
    // Re-subscribe every known session so background tabs stay live (§4.2).
    resumedRef.current.clear()
    for (const s of sessionsRef.current) resume(send, s.id)
  }, [resume])

  const { status, send } = useWebSocket('/ws/chat', handleMessage, handleOpen)

  // --- load config + sessions ---
  useEffect(() => {
    api.config().then(setCfg).catch(() => {})
    api.listSessions().then(d => setSessions(d.sessions)).catch(() => {})
  }, [])

  // resume + activate a session
  const activate = useCallback((id: string) => {
    setActiveId(id)
    if (status === 'connected') resume(send, id)
  }, [status, send, resume])

  useEffect(() => {
    if (!activeId && sessions.length) activate(sessions[0].id)
  }, [sessions, activeId, activate])

  const sendMessage = useCallback((content: string) => {
    if (!activeId) return
    setProcessing(prev => ({ ...prev, [activeId]: true }))
    // Optimistic start; a later resume replaces it with the server's timestamp.
    setTurnStart(prev => ({ ...prev, [activeId]: Date.now() }))
    // A new turn always draws a new word.
    setVerbSeed(prev => ({ ...prev, [activeId]: Date.now() }))
    send(JSON.stringify({ session_id: activeId, content }))
  }, [activeId, send])

  const onCancel = useCallback(() => {
    if (!activeId) return
    const cmd = procCmd[activeId]
    // Cancelling mid-terraform can leave infra half-applied/destroyed (§5.4).
    if ((cmd === '/up' || cmd === '/destroy') &&
      !window.confirm(`Cancelling ${cmd} mid-run can leave infrastructure in a half-applied state. Cancel anyway?`)) {
      return
    }
    send(JSON.stringify({ type: 'cancel', session_id: activeId }))
  }, [activeId, send, procCmd])

  const createSession = useCallback(async (body: Record<string, unknown>) => {
    const s = await api.createSession(body)
    setSessions(prev => [...prev, s])
    setShowNew(false)
    activate(s.id)
  }, [activate])

  const changeModel = useCallback(async (model: string) => {
    if (!activeId) return
    try {
      // Only reflect locally on success, using the server-confirmed model; the
      // backend also emits a status event to chat. On failure, leave as-is.
      const r = await api.setModel(activeId, model)
      setSessions(prev => prev.map(s => (s.id === activeId ? { ...s, model: r.model } : s)))
    } catch {
      /* PUT rejected (404/network) — keep the current model */
    }
  }, [activeId])

  const closeSession = useCallback(async (id: string) => {
    await api.deleteSession(id).catch(() => {})
    setSessions(prev => prev.filter(s => s.id !== id))
    setMsgs(prev => { const n = { ...prev }; delete n[id]; return n })
    if (activeId === id) setActiveId(null)
  }, [activeId])

  // --- resizer ---
  const onDrag = useCallback((e: React.MouseEvent) => {
    e.preventDefault()
    const move = (ev: MouseEvent) => {
      const rect = containerRef.current?.getBoundingClientRect()
      if (!rect) return
      const r = Math.max(MIN_W / rect.width, Math.min(1 - MIN_W / rect.width, (ev.clientX - rect.left) / rect.width))
      setRatio(r)
    }
    const up = () => { document.removeEventListener('mousemove', move); document.removeEventListener('mouseup', up) }
    document.addEventListener('mousemove', move)
    document.addEventListener('mouseup', up)
  }, [])

  return (
    <div style={{ display: 'flex', flexDirection: 'column', position: 'fixed', inset: 0, background: 'var(--dn-black)' }}>
      {showNew && cfg && (
        <NewSessionModal cfg={cfg} onClose={() => setShowNew(false)} onCreate={createSession} />
      )}
      {showSettings && cfg && (
        <SettingsModal
          cfg={cfg}
          model={sessions.find(s => s.id === activeId)?.model}
          onModelChange={activeId ? changeModel : undefined}
          onClose={() => setShowSettings(false)}
          onSaved={() => { setShowSettings(false); api.config().then(setCfg).catch(() => {}) }}
        />
      )}

      {/* Tab bar */}
      <div style={{ display: 'flex', alignItems: 'center', borderBottom: '1px solid var(--dn-border)', background: 'var(--dn-black)', padding: '0 8px', height: 40, gap: 4 }}>
        {/* Wordmark + release stage, grouped so the trailing gap applies to both. */}
        <span style={{ display: 'flex', alignItems: 'center', gap: 6, marginRight: 12, flexShrink: 0 }}>
          {/* Two colours rather than one string: the product is DreadGOAD, the
              surface is the Console. Mirrors the launcher's split wordmark.
              Both carry the bold weight so the pair reads as one wordmark. */}
          <span style={{ fontSize: 13, whiteSpace: 'nowrap' }}>
            <span style={{ color: 'var(--dg-brand)', fontWeight: 700 }}>DreadGOAD</span>
            <span style={{ color: 'var(--dn-text-bright)', fontWeight: 700 }}> Console</span>
          </span>
          {/* Outlined rather than filled: it should read as a qualifier on the
              wordmark, not compete with it. */}
          <span
            title="Pre-release — interfaces and behaviour may change"
            style={{
              color: 'var(--dn-warning)', border: '1px solid var(--dn-warning)',
              borderRadius: 3, padding: '0 4px', lineHeight: 1.6,
              fontSize: 9, fontWeight: 700, letterSpacing: 0.6,
              textTransform: 'uppercase', whiteSpace: 'nowrap',
            }}
          >Beta</span>
        </span>
        {sessions.map(s => (
          <div key={s.id} onClick={() => activate(s.id)} style={{
            display: 'flex', alignItems: 'center', gap: 6, padding: '4px 10px', cursor: 'pointer',
            borderRadius: 4, fontSize: 12,
            background: s.id === activeId ? 'var(--dn-surface)' : 'transparent',
            color: s.id === activeId ? 'var(--dn-text-bright)' : 'var(--dn-text-muted)',
          }}>
            <span>{s.label}</span>
            <span
              onClick={(e) => {
                e.stopPropagation()
                // Names what survives as well as what goes: deleting a session
                // has never removed the environment it created from the config,
                // and nothing said so — leaving entries in a tracked
                // dreadgoad.yaml that look like they were cleaned up.
                if (window.confirm(`Delete session "${s.label}"? This cancels any running operation and removes its working dir.\n\nThe environment stays in the config file, and any deployed infrastructure stays up — run /destroy first if you want it gone.`)) {
                  closeSession(s.id)
                }
              }}
              style={{ color: 'var(--dn-text-dim)' }}
            >✕</span>
          </div>
        ))}
        <button onClick={() => setShowNew(true)} style={{
          background: 'transparent', border: '1px solid var(--dn-border-lt)', color: 'var(--dg-brand)',
          borderRadius: 4, cursor: 'pointer', fontSize: 12, padding: '2px 8px',
        }}>+ NEW SESSION</button>
        <div style={{ flex: 1 }} />
        {cfg && !cfg.api_key_set && (
          <span
            onClick={() => setShowSettings(true)}
            title="No LLM API key set — click to add one"
            style={{ color: 'var(--dn-warning)', fontSize: 11, cursor: 'pointer' }}
          >⚠ no key</span>
        )}
      </div>

      {/* Two-pane, or an empty state until a session exists */}
      {activeId ? (
        <div ref={containerRef} style={{ display: 'flex', flex: 1, overflow: 'hidden' }}>
          <div style={{ width: `${ratio * 100}%`, minWidth: MIN_W, height: '100%' }}>
            <TerminalChat
              sessionId={activeId}
              messages={msgs[activeId] || []}
              status={status}
              onSend={sendMessage}
              processing={!!processing[activeId]}
              turnStartedAt={turnStart[activeId] || 0}
              verbSeed={verbSeed[activeId] || 0}
              onCancel={onCancel}
              model={sessions.find(s => s.id === activeId)?.model}
              onOpenSettings={() => setShowSettings(true)}
            />
          </div>
          <div onMouseDown={onDrag} style={{ width: 4, cursor: 'col-resize', background: 'var(--dn-border)', flexShrink: 0 }} />
          <div style={{ flex: 1, minWidth: MIN_W, height: '100%' }}>
            <RangeView
              sessionId={activeId}
              session={sessions.find(s => s.id === activeId)}
              refreshKey={rangeRefresh[activeId] || 0}
            />
          </div>
        </div>
      ) : (
        // --dn-text-dim measured 2.03:1 against --dn-black here, well under the
        // 4.5:1 floor — the same mistake the modal's field labels had. This is
        // the only thing on an otherwise empty screen, so it carries the whole
        // first impression of the app.
        <div style={{ flex: 1, display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', gap: 16, color: 'var(--dn-text-bright)' }}>
          <div style={{ fontSize: 13 }}>No sessions yet.</div>
          <button onClick={() => setShowNew(true)} style={{
            background: 'var(--dg-brand)', border: 'none', color: 'var(--dn-black)',
            borderRadius: 4, cursor: 'pointer', fontSize: 12, fontWeight: 700, padding: '6px 16px',
          }}>+ NEW SESSION</button>
        </div>
      )}
    </div>
  )
}

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

function NewSessionModal({ cfg, onClose, onCreate }: {
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
  const loadEnvs = useCallback(async (path: string) => {
    if (!path.trim()) {
      setEnvs([]); setEnvChoice(''); setConfigOk(false); setLoadedProvider(''); setLoadedRegions({}); setLoadedPath('')
      setEnvErr('config path is required')
      return
    }
    setLoading(true)
    setEnvErr('')
    try {
      const r = await api.environments(path.trim())
      setEnvs(r.environments)
      setConfigOk(true)
      setLoadedPath(path.trim())
      // Read from the file rather than from the listing: it is fresher, and for
      // a hand-typed path there is no listing entry to read a provider from.
      setLoadedProvider(r.provider || '')
      setLoadedRegions(r.env_regions || {})
      setEnvChoice(prev => (r.environments.includes(prev) ? prev : (r.environments[0] || NEW_ENV)))
    } catch (e) {
      setEnvs([]); setEnvChoice(''); setConfigOk(false); setLoadedProvider(''); setLoadedRegions({}); setLoadedPath('')
      setEnvErr(e instanceof Error ? e.message : String(e))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    if (creatingConfig) { setEnvs([]); setConfigOk(true); setEnvErr(''); return }
    if (choice === OTHER_PATH) {
      // Nothing has been read from this path yet, so clear the previous
      // config's verdict. Without this, switching here from a valid selection
      // left configOk true and CREATE enabled against an empty path.
      setEnvs([]); setEnvChoice(''); setConfigOk(false); setLoadedProvider(''); setLoadedRegions({}); setLoadedPath('')
      setEnvErr(customPath.trim() ? '' : 'enter a path, then click away to load it')
      if (customPath.trim()) loadEnvs(customPath)
      return
    }
    loadEnvs(choice)
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
    <div onClick={onClose} style={{ position: 'fixed', inset: 0, zIndex: 100, background: 'rgba(0,0,0,0.6)', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
      <div onClick={e => e.stopPropagation()} style={{ background: 'var(--dn-surface)', border: '1px solid var(--dn-border-lt)', borderRadius: 6, padding: 20, width: 460, maxHeight: '86vh', overflowY: 'auto', fontFamily: 'var(--font-mono)', fontSize: 12, color: 'var(--dn-text)' }}>
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
      </div>
    </div>
  )
}

function SettingsModal({ cfg, model, onModelChange, onClose, onSaved }: {
  cfg: AppConfig
  model?: string
  onModelChange?: (model: string) => Promise<void> | void
  onClose: () => void
  onSaved: () => void
}) {
  const [modelInput, setModelInput] = useState(model ?? '')
  const [apiKey, setApiKey] = useState('')
  const [apiKeyEnv, setApiKeyEnv] = useState('OPENROUTER_API_KEY')
  const [err, setErr] = useState('')
  const [saving, setSaving] = useState(false)

  const save = async () => {
    setErr('')
    setSaving(true)
    try {
      // Model (per active session) — apply if changed.
      const m = modelInput.trim()
      if (onModelChange && m && m !== model) await onModelChange(m)
      // API key (global) — apply if a key was entered.
      if (apiKey.trim()) {
        await api.setSettings({ api_key: apiKey.trim(), api_key_env: apiKeyEnv.trim() || undefined })
      }
      onSaved()
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e))
    } finally {
      setSaving(false)
    }
  }

  return (
    <div onClick={onClose} style={{ position: 'fixed', inset: 0, zIndex: 100, background: 'rgba(0,0,0,0.6)', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
      <div onClick={e => e.stopPropagation()} style={{ background: 'var(--dn-surface)', border: '1px solid var(--dn-border-lt)', borderRadius: 6, padding: 20, width: 420, fontFamily: 'var(--font-mono)', fontSize: 12, color: 'var(--dn-text)' }}>
        <div style={{ color: 'var(--dg-brand)', fontWeight: 700, fontSize: 13, marginBottom: 16 }}>Settings</div>

        {onModelChange ? (
          <>
            <Field label="Model (this session)" value={modelInput} onChange={setModelInput} placeholder="openrouter/anthropic/claude-sonnet-5" />
            <div style={{ color: 'var(--dn-text-dim)', fontSize: 10, marginTop: -8, marginBottom: 12 }}>
              Changing the model continues this session's conversation on the new model.
            </div>
          </>
        ) : (
          <div style={{ color: 'var(--dn-text-dim)', fontSize: 11, marginBottom: 12 }}>
            Open a session to change its model.
          </div>
        )}

        <Field label="API key" value={apiKey} onChange={setApiKey} placeholder="sk-or-…  (stored in memory, never saved)" type="password" />
        <Field label="API key env var" value={apiKeyEnv} onChange={setApiKeyEnv} placeholder="OPENROUTER_API_KEY" />
        <div style={{ color: cfg.api_key_set ? 'var(--dn-success)' : 'var(--dn-warning)', fontSize: 11, marginTop: -4, marginBottom: 12 }}>
          {cfg.api_key_set ? '● API key is set (leave blank to keep)' : '○ No API key set — agent turns will fail'}
        </div>

        {err && <div style={{ color: 'var(--dn-error)', fontSize: 11, marginBottom: 8 }}>{err}</div>}
        <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8, marginTop: 8 }}>
          <button onClick={onClose} style={btnStyle(false)}>CANCEL</button>
          <button onClick={save} disabled={saving} style={btnStyle(true)}>{saving ? 'SAVING…' : 'SAVE'}</button>
        </div>
      </div>
    </div>
  )
}

function Field({ label, value, onChange, placeholder, type, onBlur, suggestions }: {
  label: string
  value: string
  onChange: (v: string) => void
  placeholder?: string
  type?: string
  onBlur?: () => void
  /** Offered via a datalist — the field stays free text, so these never constrain. */
  suggestions?: string[]
}) {
  // useId, not a module counter: a datalist is referenced by id, so each field
  // needs its own or they attach the wrong suggestions to each other. Deriving
  // it from a counter meant writing to a module global during render — impure,
  // and double-counted under StrictMode (main.tsx enables it). useId exists for
  // exactly this and is stable across renders without the side effect.
  const listId = useId()
  const hasList = !!suggestions?.length

  return (
    <div style={{ marginBottom: 12 }}>
      {/* Bright and bold: --dn-text-dim measured 1.94:1 against the modal
          surface, far under the 4.5:1 floor, which left the field labels
          barely visible. These name what you are about to type into a form
          that creates a range — the last thing that should be guessed at. */}
      <label style={{
        display: 'block', marginBottom: 4,
        color: 'var(--dn-text-bright)', fontWeight: 700,
      }}>{label}</label>
      <input type={type} value={value} placeholder={placeholder} onChange={e => onChange(e.target.value)} onBlur={onBlur}
        list={hasList ? listId : undefined} style={{
        width: '100%', boxSizing: 'border-box', padding: '6px 8px', background: 'var(--dn-bg)',
        border: '1px solid var(--dn-border)', borderRadius: 3, color: 'var(--dn-text)',
        fontFamily: 'var(--font-mono)', fontSize: 12,
      }} />
      {hasList && (
        <datalist id={listId}>
          {suggestions!.map(s => <option key={s} value={s} />)}
        </datalist>
      )}
    </div>
  )
}

/** A labelled <select>, matching Field's label treatment and input chrome. */
function Select({ label, value, onChange, options, disabled }: {
  label: string
  value: string
  onChange: (v: string) => void
  options: Array<{ value: string; label: string; disabled?: boolean }>
  disabled?: boolean
}) {
  return (
    <div style={{ marginBottom: 12 }}>
      <label style={{
        display: 'block', marginBottom: 4,
        color: 'var(--dn-text-bright)', fontWeight: 700,
      }}>{label}</label>
      <select
        value={value}
        disabled={disabled}
        onChange={e => onChange(e.target.value)}
        style={{
          width: '100%', boxSizing: 'border-box', padding: '6px 8px', background: 'var(--dn-bg)',
          border: '1px solid var(--dn-border)', borderRadius: 3, color: 'var(--dn-text)',
          fontFamily: 'var(--font-mono)', fontSize: 12,
        }}
      >
        {options.length === 0 && <option value="">—</option>}
        {options.map(o => (
          <option key={o.value} value={o.value} disabled={o.disabled}>{o.label}</option>
        ))}
      </select>
    </div>
  )
}

function btnStyle(primary: boolean): React.CSSProperties {
  return {
    background: primary ? 'var(--dg-brand)' : 'transparent',
    border: primary ? 'none' : '1px solid var(--dn-border-lt)',
    color: primary ? 'var(--dn-black)' : 'var(--dn-text-dim)',
    fontFamily: 'var(--font-mono)', fontSize: 11, fontWeight: primary ? 700 : 400,
    padding: '4px 12px', borderRadius: 3, cursor: 'pointer',
  }
}

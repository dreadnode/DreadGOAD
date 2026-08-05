import { useCallback, useEffect, useRef, useState } from 'react'
import TerminalChat from './components/TerminalChat'
import RangeView from './components/RangeView'
import { useWebSocket } from './hooks/useWebSocket'
import { api, type AppConfig } from './api'
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
      return
    }
    setMsgs(prev => ({ ...prev, [sid]: [...(prev[sid] || []), withCid(ev)] }))
    // A check_run means the hook refreshed the range doc → make RangeView re-fetch.
    if (ev.kind === 'check_run') {
      setRangeRefresh(prev => ({ ...prev, [sid]: (prev[sid] || 0) + 1 }))
    }
    if (ev.kind === 'command_run' && ev.phase === 'start' && typeof ev.command === 'string') {
      setProcCmd(prev => ({ ...prev, [sid]: ev.command as string }))
    }
    if (ev.kind === 'agent_end') {
      setProcessing(prev => ({ ...prev, [sid]: false }))
      setProcCmd(prev => ({ ...prev, [sid]: '' }))
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
        <span style={{ color: 'var(--dg-brand)', fontWeight: 700, fontSize: 13, marginRight: 12 }}>DreadGOAD</span>
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
                if (window.confirm(`Delete session "${s.label}"? This cancels any running operation and removes its working dir.`)) {
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
        }}>+ NEW</button>
        <div style={{ flex: 1 }} />
        {cfg && !cfg.api_key_set && (
          <span title="No LLM API key set — agent turns will fail" style={{ color: 'var(--dn-warning)', fontSize: 11 }}>⚠ no key</span>
        )}
        <button onClick={() => setShowSettings(true)} title="Settings (API key)" style={{
          background: 'transparent', border: 'none', color: 'var(--dn-text-muted)',
          cursor: 'pointer', fontSize: 14, padding: '2px 8px',
        }}>⚙</button>
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
        <div style={{ flex: 1, display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', gap: 16, color: 'var(--dn-text-dim)' }}>
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

function NewSessionModal({ cfg, onClose, onCreate }: {
  cfg: AppConfig
  onClose: () => void
  onCreate: (body: Record<string, unknown>) => void
}) {
  const [mode, setMode] = useState<'attach' | 'new'>('attach')
  const [configPath, setConfigPath] = useState(cfg.default_config_path)
  const [label, setLabel] = useState('')
  // shared config → env list
  const [envs, setEnvs] = useState<string[]>([])
  const [envErr, setEnvErr] = useState('')
  const [configOk, setConfigOk] = useState(false)
  const [loading, setLoading] = useState(false)
  // attach mode
  const [env, setEnv] = useState('')
  // new-environment mode
  const [newEnv, setNewEnv] = useState('')
  const [source, setSource] = useState('ad/GOAD')
  const [target, setTarget] = useState('')
  const [variantName, setVariantName] = useState('')
  const [cidr, setCidr] = useState('10.100.0.0/16')

  // Load the environments in the chosen config → attach dropdown + new-env
  // collision check. Runs on mount (default config) and on config-path change.
  const loadEnvs = useCallback(async (path: string) => {
    if (!path.trim()) { setEnvs([]); setEnv(''); setConfigOk(false); setEnvErr('config path is required'); return }
    setLoading(true)
    setEnvErr('')
    try {
      const r = await api.environments(path.trim())
      setEnvs(r.environments)
      setConfigOk(true)
      setEnv(prev => (r.environments.includes(prev) ? prev : (r.environments[0] || '')))
    } catch (e) {
      setEnvs([]); setEnv(''); setConfigOk(false)
      setEnvErr(e instanceof Error ? e.message : String(e))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { loadEnvs(cfg.default_config_path) }, [cfg.default_config_path, loadEnvs])

  const nm = newEnv.trim()
  const collides = envs.includes(nm)
  const attachValid = configOk && !!env
  const newValid = configOk && !!nm && !collides
  const valid = mode === 'attach' ? attachValid : newValid

  const submit = () => {
    if (mode === 'attach') {
      if (attachValid) onCreate({ config_path: configPath, env, label: label.trim() || undefined })
      return
    }
    if (!newValid) return
    onCreate({
      mode: 'new',
      config_path: configPath,
      env: nm,
      env_fields: {
        variant: true,
        variant_source: source.trim() || 'ad/GOAD',
        variant_target: target.trim() || `ad/GOAD-${nm}`,
        variant_name: variantName.trim() || nm,
        vpc_cidr: cidr.trim() || '10.100.0.0/16',
      },
      label: label.trim() || undefined,
    })
  }

  return (
    <div onClick={onClose} style={{ position: 'fixed', inset: 0, zIndex: 100, background: 'rgba(0,0,0,0.6)', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
      <div onClick={e => e.stopPropagation()} style={{ background: 'var(--dn-surface)', border: '1px solid var(--dn-border-lt)', borderRadius: 6, padding: 20, width: 420, fontFamily: 'var(--font-mono)', fontSize: 12, color: 'var(--dn-text)' }}>
        <div style={{ color: 'var(--dg-brand)', fontWeight: 700, fontSize: 13, marginBottom: 12 }}>New Session</div>

        {/* Mode toggle */}
        <div style={{ display: 'flex', gap: 4, marginBottom: 16 }}>
          {(['attach', 'new'] as const).map(m => (
            <button key={m} onClick={() => setMode(m)} style={{
              flex: 1, padding: '5px 0', borderRadius: 3, cursor: 'pointer', fontSize: 11,
              border: '1px solid var(--dn-border-lt)',
              background: mode === m ? 'var(--dn-border)' : 'transparent',
              color: mode === m ? 'var(--dn-text-bright)' : 'var(--dn-text-muted)',
            }}>{m === 'attach' ? 'Attach existing' : 'New environment'}</button>
          ))}
        </div>

        <Field label="Config path" value={configPath} onChange={setConfigPath} onBlur={() => loadEnvs(configPath)} />

        {mode === 'attach' ? (
          <div style={{ marginBottom: 12 }}>
            <label style={{ display: 'block', marginBottom: 4, color: 'var(--dn-text-dim)' }}>
              Environment{loading ? ' (loading…)' : ''}
            </label>
            <select
              value={env}
              onChange={e => setEnv(e.target.value)}
              disabled={loading || envs.length === 0}
              style={{
                width: '100%', boxSizing: 'border-box', padding: '6px 8px', background: 'var(--dn-bg)',
                border: '1px solid var(--dn-border)', borderRadius: 3, color: 'var(--dn-text)',
                fontFamily: 'var(--font-mono)', fontSize: 12,
              }}
            >
              {envs.length === 0 && <option value="">{loading ? 'loading…' : '—'}</option>}
              {envs.map(n => <option key={n} value={n}>{n}</option>)}
            </select>
            {configOk && envs.length === 0 && (
              <div style={{ color: 'var(--dn-warning)', fontSize: 11, marginTop: 4 }}>no environments in this config — use “New environment”</div>
            )}
            {envErr && <div style={{ color: 'var(--dn-error)', fontSize: 11, marginTop: 4 }}>{envErr}</div>}
          </div>
        ) : (
          <>
            <Field label="New environment name" value={newEnv} onChange={setNewEnv} placeholder="e.g. redteam" />
            {envErr && <div style={{ color: 'var(--dn-error)', fontSize: 11, marginTop: -8, marginBottom: 12 }}>{envErr}</div>}
            {nm && collides && <div style={{ color: 'var(--dn-error)', fontSize: 11, marginTop: -8, marginBottom: 12 }}>“{nm}” already exists in this config</div>}
            <Field label="Variant source (base lab)" value={source} onChange={setSource} placeholder="ad/GOAD" />
            <Field label="Variant target" value={target} onChange={setTarget} placeholder={nm ? `ad/GOAD-${nm}` : 'ad/GOAD-<name>'} />
            <Field label="Variant name" value={variantName} onChange={setVariantName} placeholder={nm || '<name>'} />
            <Field label="VPC CIDR" value={cidr} onChange={setCidr} placeholder="10.100.0.0/16" />
            <div style={{ color: 'var(--dn-text-dim)', fontSize: 10, marginBottom: 12 }}>
              Writes a new env into the config (a .bak is saved; comments are not preserved).
            </div>
          </>
        )}

        <Field label="Label (optional)" value={label} onChange={setLabel} />
        <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8, marginTop: 8 }}>
          <button onClick={onClose} style={btnStyle(false)}>CANCEL</button>
          <button
            onClick={submit}
            disabled={!valid}
            style={{ ...btnStyle(true), opacity: valid ? 1 : 0.5, cursor: valid ? 'pointer' : 'not-allowed' }}
          >CREATE</button>
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

function Field({ label, value, onChange, placeholder, type, onBlur }: { label: string; value: string; onChange: (v: string) => void; placeholder?: string; type?: string; onBlur?: () => void }) {
  return (
    <div style={{ marginBottom: 12 }}>
      <label style={{ display: 'block', marginBottom: 4, color: 'var(--dn-text-dim)' }}>{label}</label>
      <input type={type} value={value} placeholder={placeholder} onChange={e => onChange(e.target.value)} onBlur={onBlur} style={{
        width: '100%', boxSizing: 'border-box', padding: '6px 8px', background: 'var(--dn-bg)',
        border: '1px solid var(--dn-border)', borderRadius: 3, color: 'var(--dn-text)',
        fontFamily: 'var(--font-mono)', fontSize: 12,
      }} />
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

import { useCallback, useEffect, useRef, useState } from 'react'
import TerminalChat from './components/TerminalChat'
import RangeView from './components/RangeView'
import Modal from './components/Modal'
import ConfirmModal from './components/ConfirmModal'
import NewSessionModal from './components/NewSessionModal'
import { Field, btnStyle } from './components/FormFields'
import { useWebSocket } from './hooks/useWebSocket'
import { api, type AppConfig } from './api'
import type { ChatEvent, Session } from './types'

const MIN_W = 320
const DEFAULT_RATIO = 0.45
const MAX_PROGRESS = 200

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
  const [pendingConfirm, setPendingConfirm] = useState<{
    title: string; message: string; confirmLabel?: string;
    destructive?: boolean; onConfirm: () => void;
  } | null>(null)

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
    if (ev.kind === 'command_progress') {
      setMsgs(prev => {
        const arr = prev[sid] || []
        const updated = [...arr, withCid(ev)]
        let run = 0
        for (let i = updated.length - 1; i >= 0 && updated[i].kind === 'command_progress'; i--) run++
        if (run > MAX_PROGRESS) updated.splice(updated.length - run, run - MAX_PROGRESS)
        return { ...prev, [sid]: updated }
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
    if (ev.kind === 'command_run' && typeof ev.command === 'string') {
      setProcCmd(prev => ({ ...prev, [sid]: ev.phase === 'start' ? ev.command as string : '' }))
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
    if (!activeId || pendingConfirm) return
    const cmd = procCmd[activeId]
    if (cmd === '/up' || cmd === '/destroy') {
      setPendingConfirm({
        title: `Cancel ${cmd}?`,
        message: `Cancelling ${cmd} mid-run can leave infrastructure in a half-applied state.`,
        destructive: true,
        confirmLabel: 'CANCEL ANYWAY',
        onConfirm: () => {
          send(JSON.stringify({ type: 'cancel', session_id: activeId }))
          setPendingConfirm(null)
        },
      })
      return
    }
    send(JSON.stringify({ type: 'cancel', session_id: activeId }))
  }, [activeId, send, procCmd, pendingConfirm])

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
      {pendingConfirm && (
        <ConfirmModal
          title={pendingConfirm.title}
          message={pendingConfirm.message}
          confirmLabel={pendingConfirm.confirmLabel}
          destructive={pendingConfirm.destructive}
          onConfirm={pendingConfirm.onConfirm}
          onCancel={() => setPendingConfirm(null)}
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
                setPendingConfirm({
                  title: `Delete "${s.label}"?`,
                  message: 'This cancels any running operation and removes its working dir.\n\nThe environment stays in the config file, and any deployed infrastructure stays up — run /destroy first if you want it gone.',
                  destructive: true,
                  confirmLabel: 'DELETE',
                  onConfirm: () => { closeSession(s.id); setPendingConfirm(null) },
                })
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
    <Modal onClose={onClose} width={420} ariaLabel="Settings">
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
    </Modal>
  )
}

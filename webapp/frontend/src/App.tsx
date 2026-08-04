import { useCallback, useEffect, useRef, useState } from 'react'
import TerminalChat from './components/TerminalChat'
import RangeView from './components/RangeView'
import { useWebSocket } from './hooks/useWebSocket'
import { api, type AppConfig } from './api'
import type { ChatEvent, Session } from './types'

const MIN_W = 320
const DEFAULT_RATIO = 0.45

export default function App() {
  const [sessions, setSessions] = useState<Session[]>([])
  const [activeId, setActiveId] = useState<string | null>(null)
  const [msgs, setMsgs] = useState<Record<string, ChatEvent[]>>({})
  const [cfg, setCfg] = useState<AppConfig | null>(null)
  const [ratio, setRatio] = useState(DEFAULT_RATIO)
  const [showNew, setShowNew] = useState(false)

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
      const events = ev.events || []
      setMsgs(prev => ({ ...prev, [sid]: events }))
      return
    }
    setMsgs(prev => ({ ...prev, [sid]: [...(prev[sid] || []), ev] }))
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
    send(JSON.stringify({ session_id: activeId, content }))
  }, [activeId, send])

  const createSession = useCallback(async (body: Record<string, unknown>) => {
    const s = await api.createSession(body)
    setSessions(prev => [...prev, s])
    setShowNew(false)
    activate(s.id)
  }, [activate])

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
            <span onClick={(e) => { e.stopPropagation(); closeSession(s.id) }} style={{ color: 'var(--dn-text-dim)' }}>✕</span>
          </div>
        ))}
        <button onClick={() => setShowNew(true)} style={{
          background: 'transparent', border: '1px solid var(--dn-border-lt)', color: 'var(--dg-brand)',
          borderRadius: 4, cursor: 'pointer', fontSize: 12, padding: '2px 8px',
        }}>+ NEW</button>
      </div>

      {/* Two-pane */}
      <div ref={containerRef} style={{ display: 'flex', flex: 1, overflow: 'hidden' }}>
        <div style={{ width: `${ratio * 100}%`, minWidth: MIN_W, height: '100%' }}>
          <TerminalChat sessionId={activeId} messages={activeId ? (msgs[activeId] || []) : []} status={status} onSend={sendMessage} />
        </div>
        <div onMouseDown={onDrag} style={{ width: 4, cursor: 'col-resize', background: 'var(--dn-border)', flexShrink: 0 }} />
        <div style={{ flex: 1, minWidth: MIN_W, height: '100%' }}>
          <RangeView sessionId={activeId} />
        </div>
      </div>
    </div>
  )
}

function NewSessionModal({ cfg, onClose, onCreate }: {
  cfg: AppConfig
  onClose: () => void
  onCreate: (body: Record<string, unknown>) => void
}) {
  const [configPath, setConfigPath] = useState(cfg.default_config_path)
  const [env, setEnv] = useState('')
  const [label, setLabel] = useState('')

  return (
    <div onClick={onClose} style={{ position: 'fixed', inset: 0, zIndex: 100, background: 'rgba(0,0,0,0.6)', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
      <div onClick={e => e.stopPropagation()} style={{ background: 'var(--dn-surface)', border: '1px solid var(--dn-border-lt)', borderRadius: 6, padding: 20, width: 380, fontFamily: 'var(--font-mono)', fontSize: 12, color: 'var(--dn-text)' }}>
        <div style={{ color: 'var(--dg-brand)', fontWeight: 700, fontSize: 13, marginBottom: 16 }}>New Session</div>
        <Field label="Config path" value={configPath} onChange={setConfigPath} />
        <Field label="Environment" value={env} onChange={setEnv} placeholder="e.g. staging" />
        <Field label="Label (optional)" value={label} onChange={setLabel} />
        <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8, marginTop: 8 }}>
          <button onClick={onClose} style={btnStyle(false)}>CANCEL</button>
          <button
            onClick={() => env.trim() && onCreate({ config_path: configPath, env: env.trim(), label: label.trim() || undefined })}
            style={btnStyle(true)}
          >CREATE</button>
        </div>
      </div>
    </div>
  )
}

function Field({ label, value, onChange, placeholder }: { label: string; value: string; onChange: (v: string) => void; placeholder?: string }) {
  return (
    <div style={{ marginBottom: 12 }}>
      <label style={{ display: 'block', marginBottom: 4, color: 'var(--dn-text-dim)' }}>{label}</label>
      <input value={value} placeholder={placeholder} onChange={e => onChange(e.target.value)} style={{
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

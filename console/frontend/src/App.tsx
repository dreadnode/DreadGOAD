import { useCallback, useEffect, useRef, useState } from 'react'
import TerminalChat from './components/TerminalChat'
import RangeView from './components/RangeView'
import ConfirmModal from './components/ConfirmModal'
import NewSessionModal from './components/NewSessionModal'
import SettingsModal from './components/SettingsModal'
import { mergeSessionSnapshots } from './consoleEventState'
import { useConsoleSessionEvents } from './hooks/useConsoleSessionEvents'
import { api, type AppConfig } from './api'
import type { Session } from './types'

const MIN_W = 320
const DEFAULT_RATIO = 0.45
export default function App() {
  const [sessions, setSessions] = useState<Session[]>([])
  const [activeId, setActiveId] = useState<string | null>(null)
  const [cfg, setCfg] = useState<AppConfig | null>(null)
  const [ratio, setRatio] = useState(DEFAULT_RATIO)
  const [showNew, setShowNew] = useState(false)
  const [showSettings, setShowSettings] = useState(false)
  const [pendingConfirm, setPendingConfirm] = useState<{
    title: string; message: string; confirmLabel?: string;
    destructive?: boolean; onConfirm: () => void;
  } | null>(null)
  const containerRef = useRef<HTMLDivElement>(null)
  const refreshSessionSnapshots = useCallback((next: Session[]) => {
    setSessions(current => mergeSessionSnapshots(current, next))
  }, [])
  const {
    state: {
      messages: msgs,
      rangeRefresh,
      processing,
      command: procCmd,
      turnStartedAt: turnStart,
      verbSeed,
      approvals: pendingApprovals,
    },
    status,
    send,
    resume,
    beginTurn,
    forgetSession,
    sendApproval,
  } = useConsoleSessionEvents(sessions, refreshSessionSnapshots)

  // --- load config + sessions ---
  useEffect(() => {
    api.config().then(setCfg).catch(() => {})
    api.listSessions().then(d => setSessions(d.sessions)).catch(() => {})
  }, [])

  // resume + activate a session
  const activate = useCallback((id: string) => {
    setActiveId(id)
    resume(id)
  }, [resume])

  useEffect(() => {
    if (!activeId && sessions.length) activate(sessions[0].id)
  }, [sessions, activeId, activate])

  const sendMessage = useCallback((content: string) => {
    if (!activeId) return
    const sent = send(JSON.stringify({ session_id: activeId, content }))
    if (sent) beginTurn(activeId)
  }, [activeId, beginTurn, send])

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
    try {
      await api.deleteSession(id)
    } catch {
      // The backend rejects deletion while a turn or approval is active. Keep
      // the tab and approval visible so the operator can resolve it safely.
      return
    }
    setSessions(prev => prev.filter(s => s.id !== id))
    forgetSession(id)
    if (activeId === id) setActiveId(null)
  }, [activeId, forgetSession])

  const approval = Object.entries(pendingApprovals)[0]
  const approvalSession = approval
    ? sessions.find(session => session.id === approval[0])?.label || approval[0]
    : null
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
      {approval && (
        <ConfirmModal
          title={`Run ${approval[1].command}?`}
          message={`Session: ${approvalSession}\n${approval[1].detail}`}
          codeLabel="Exact command"
          commandArgv={approval[1].argv}
          confirmLabel="CONFIRM"
          destructive
          onConfirm={() => sendApproval(approval[0], approval[1], 'confirm')}
          onCancel={() => sendApproval(approval[0], approval[1], 'deny')}
        />
      )}
      {!approval && showNew && cfg && (
        <NewSessionModal cfg={cfg} onClose={() => setShowNew(false)} onCreate={createSession} />
      )}
      {!approval && showSettings && cfg && (
        <SettingsModal
          cfg={cfg}
          model={sessions.find(s => s.id === activeId)?.model}
          onModelChange={activeId ? changeModel : undefined}
          onClose={() => setShowSettings(false)}
          onSaved={() => { setShowSettings(false); api.config().then(setCfg).catch(() => {}) }}
        />
      )}
      {!approval && pendingConfirm && (
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
              confirmationBlocked={Boolean(approval)}
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

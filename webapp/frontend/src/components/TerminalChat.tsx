import { useEffect, useRef, useState } from 'react'
import Markdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import type { ChatEvent } from '../types'
import type { ConnectionStatus } from '../hooks/useWebSocket'

interface Props {
  sessionId: string | null
  messages: ChatEvent[]
  status: ConnectionStatus
  onSend: (content: string) => void
  processing: boolean
  onCancel: () => void
}

function Badge({ text, color }: { text: string; color: string }) {
  return (
    <span style={{
      display: 'inline-block', padding: '1px 6px', borderRadius: 3,
      background: 'var(--dn-surface)', color, fontSize: 11, marginRight: 6,
    }}>{text}</span>
  )
}

function toolSummary(ev: ChatEvent): string {
  if (ev.tool) {
    let a = ''
    try { a = JSON.stringify(JSON.parse(ev.args || '{}')) } catch { a = ev.args || '' }
    return `${ev.tool} ${a}`.trim()
  }
  return ev.command || ''
}

function Message({ ev }: { ev: ChatEvent }) {
  switch (ev.kind) {
    case 'user_message':
      return (
        <div style={{ marginBottom: 8 }}>
          <span style={{ color: 'var(--dg-interactive)', marginRight: 8 }}>&gt;</span>
          <span style={{ color: 'var(--dn-text-bright)' }}>{ev.content}</span>
        </div>
      )
    case 'generation':
      return ev.content ? (
        <div className="markdown-body" style={{ marginBottom: 8, fontSize: 13 }}>
          <Markdown remarkPlugins={[remarkGfm]}>{ev.content}</Markdown>
        </div>
      ) : null
    case 'tool_start':
      return <div style={{ marginBottom: 6 }}><Badge text="TOOL" color="#4fc3f7" /><span style={{ color: 'var(--dn-text-muted)', fontSize: 12 }}>{toolSummary(ev)}</span></div>
    case 'tool_end':
      return ev.result ? <pre style={preStyle}>{ev.result}</pre> : null
    case 'command_run':
      return ev.phase === 'start'
        ? <div style={{ marginBottom: 6 }}><Badge text="CMD" color="var(--dg-brand)" /><span style={{ color: 'var(--dn-text-muted)', fontSize: 12 }}>{ev.command}</span></div>
        : <div style={{ marginBottom: 6, fontSize: 12, color: ev.exit_code ? 'var(--dn-error)' : 'var(--dn-success)' }}>exit {String(ev.exit_code)}</div>
    case 'command_progress':
      return <div style={{ fontSize: 11, color: 'var(--dn-text-dim)', whiteSpace: 'pre-wrap' }}>{ev.line}</div>
    case 'check_run':
      return <div style={{ marginBottom: 6 }}><Badge text="CHECK" color="var(--dg-interactive)" /><span style={{ color: 'var(--dn-text-muted)', fontSize: 12 }}>{ev.error ? `check failed: ${String(ev.error)}` : `range verified — ${ev.hosts_updated ?? 0} host(s) updated`}</span></div>
    case 'error':
      return <div style={{ marginBottom: 6 }}><Badge text="ERROR" color="var(--dn-error)" /><span style={{ color: 'var(--dn-error)', fontSize: 12 }}>{ev.message}</span></div>
    default:
      return null
  }
}

export default function TerminalChat({ sessionId, messages, status, onSend, processing, onCancel }: Props) {
  const [input, setInput] = useState('')
  const endRef = useRef<HTMLDivElement>(null)

  useEffect(() => { endRef.current?.scrollIntoView({ behavior: 'smooth' }) }, [messages])

  // Esc cancels the in-flight command/turn (sends {type:cancel} → SIGINT, §5.4).
  useEffect(() => {
    if (!processing) return
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') onCancel() }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [processing, onCancel])

  const submit = () => {
    const t = input.trim()
    if (!t || !sessionId || status !== 'connected') return
    onSend(t)
    setInput('')
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%', background: 'var(--dn-bg)', borderRight: '1px solid var(--dn-border)' }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', padding: '12px 16px', borderBottom: '1px solid var(--dn-border)', background: 'var(--dn-black)' }}>
        <span style={{ color: 'var(--dg-brand)', fontSize: 13, fontWeight: 700 }}>AGENT</span>
        <span style={{ color: 'var(--dn-text-dim)', fontSize: 11 }}>{status}</span>
      </div>
      <div style={{ flex: 1, overflowY: 'auto', padding: '12px 16px' }}>
        {!sessionId && <div style={{ color: 'var(--dn-text-dim)', fontSize: 13 }}>Create or select a session to begin.</div>}
        {messages.map((ev, i) => <Message key={ev._cid ?? i} ev={ev} />)}
        {processing && (
          <div style={{ display: 'flex', gap: 12, alignItems: 'baseline', marginTop: 4 }}>
            <span style={{ color: 'var(--dg-interactive)', fontSize: 13 }}>working…</span>
            <span
              role="button"
              tabIndex={0}
              onClick={onCancel}
              style={{ color: 'var(--dn-text-dim)', fontSize: 12, cursor: 'pointer' }}
            >Press Esc to cancel</span>
          </div>
        )}
        <div ref={endRef} />
      </div>
      <div style={{ display: 'flex', alignItems: 'flex-start', padding: '12px 16px', borderTop: '1px solid var(--dn-border)', background: 'var(--dn-black)' }}>
        <span style={{ color: 'var(--dg-interactive)', marginRight: 8 }}>&gt;</span>
        <textarea
          value={input}
          rows={1}
          disabled={!sessionId || status !== 'connected'}
          onChange={e => setInput(e.target.value)}
          onKeyDown={e => { if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); submit() } }}
          placeholder={status === 'connected' ? 'message or /command' : 'connecting…'}
          style={{
            flex: 1, background: 'transparent', border: 'none', outline: 'none', resize: 'none',
            color: 'var(--dn-text-bright)', fontFamily: 'var(--font-mono)', fontSize: 13,
          }}
        />
      </div>
    </div>
  )
}

const preStyle: React.CSSProperties = {
  margin: '0 0 6px 12px', whiteSpace: 'pre-wrap', fontFamily: 'inherit',
  fontSize: 11, color: 'var(--dn-text-muted)', maxHeight: 120, overflow: 'auto',
}

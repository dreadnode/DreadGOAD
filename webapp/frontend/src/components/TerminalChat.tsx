import { useEffect, useMemo, useRef, useState } from 'react'
import Markdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import type { ChatEvent, HealthCheck } from '../types'
import type { ConnectionStatus } from '../hooks/useWebSocket'
import { api, type CommandDef } from '../api'

const HEALTH_COLOR: Record<string, string> = {
  OK: 'var(--dn-success)',
  FAIL: 'var(--dn-error)',
  SKIP: 'var(--dn-text-muted)',
}

function HealthReport({ ev }: { ev: ChatEvent }) {
  const checks = ev.checks ?? []
  const failed = ev.failed ?? 0
  const summaryColor = failed > 0 ? 'var(--dn-error)' : 'var(--dn-success)'
  return (
    <div style={{ marginBottom: 8 }}>
      <div style={{ marginBottom: 4 }}>
        <Badge text="HEALTH" color="var(--dg-interactive)" />
        <span style={{ color: summaryColor, fontSize: 12 }}>
          {ev.passed ?? 0} passed · {failed} failed · {ev.skipped ?? 0} skipped
        </span>
      </div>
      <div style={{ display: 'grid', gridTemplateColumns: 'auto auto 1fr', gap: '2px 10px', fontSize: 11, marginLeft: 12 }}>
        {checks.map((c: HealthCheck, i: number) => (
          <div key={i} style={{ display: 'contents' }}>
            <span style={{ color: HEALTH_COLOR[c.status] ?? 'var(--dn-text-muted)', fontWeight: 700 }}>{c.status}</span>
            <span style={{ color: 'var(--dn-text-muted)' }}>{c.host}</span>
            <span style={{ color: 'var(--dn-text-dim)', whiteSpace: 'pre-wrap' }}>
              {c.name}{c.detail ? ` — ${c.detail}` : ''}
            </span>
          </div>
        ))}
      </div>
    </div>
  )
}

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
    case 'health_report':
      return <HealthReport ev={ev} />
    case 'error':
      return <div style={{ marginBottom: 6 }}><Badge text="ERROR" color="var(--dn-error)" /><span style={{ color: 'var(--dn-error)', fontSize: 12 }}>{ev.message}</span></div>
    default:
      return null
  }
}

export default function TerminalChat({ sessionId, messages, status, onSend, processing, onCancel }: Props) {
  const [input, setInput] = useState('')
  const [commands, setCommands] = useState<CommandDef[]>([])
  const [cmdHighlight, setCmdHighlight] = useState(0)
  const endRef = useRef<HTMLDivElement>(null)
  const inputRef = useRef<HTMLTextAreaElement>(null)

  useEffect(() => { endRef.current?.scrollIntoView({ behavior: 'smooth' }) }, [messages])

  // Load the slash-command registry once for the autocomplete menu (§5.1).
  useEffect(() => { api.commands().then(r => setCommands(r.commands)).catch(() => {}) }, [])

  // Esc cancels the in-flight command/turn (sends {type:cancel} → SIGINT, §5.4).
  useEffect(() => {
    if (!processing) return
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') onCancel() }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [processing, onCancel])

  // Autocomplete: filter to the typed `/`-token; hide once a space is typed (args).
  const firstToken = input.split(' ')[0]
  const filteredCommands = useMemo(
    () => (input.startsWith('/') ? commands.filter(c => c.name.startsWith(firstToken)) : []),
    [commands, input, firstToken],
  )
  const showCmdMenu = filteredCommands.length > 0 && !input.includes(' ')

  const submit = () => {
    const t = input.trim()
    if (!t || !sessionId || status !== 'connected') return
    onSend(t)
    setInput('')
    setCmdHighlight(0)
  }

  const selectCommand = (cmd: CommandDef) => {
    // Fill the command; agent commands take args, so leave a trailing space.
    setInput(cmd.name + ' ')
    setCmdHighlight(0)
    inputRef.current?.focus()
  }

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (showCmdMenu) {
      if (e.key === 'ArrowUp') {
        e.preventDefault()
        setCmdHighlight(i => (i > 0 ? i - 1 : filteredCommands.length - 1))
        return
      }
      if (e.key === 'ArrowDown') {
        e.preventDefault()
        setCmdHighlight(i => (i < filteredCommands.length - 1 ? i + 1 : 0))
        return
      }
      if (e.key === 'Tab' || (e.key === 'Enter' && !e.shiftKey)) {
        e.preventDefault()
        selectCommand(filteredCommands[cmdHighlight])
        return
      }
      if (e.key === 'Escape') { e.preventDefault(); setInput(''); return }
    }
    if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); submit() }
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
      <div style={{ position: 'relative', borderTop: '1px solid var(--dn-border)', background: 'var(--dn-black)' }}>
        {showCmdMenu && (
          <div style={{
            position: 'absolute', bottom: '100%', left: 0, right: 0, zIndex: 50,
            background: 'var(--dn-surface)', border: '1px solid var(--dn-border)',
            borderBottom: 'none', borderRadius: '4px 4px 0 0',
            maxHeight: 220, overflowY: 'auto', fontFamily: 'var(--font-mono)', fontSize: 12,
          }}>
            {filteredCommands.map((cmd, i) => (
              <div
                key={cmd.name}
                // onMouseDown (not onClick) so the item is chosen before the
                // textarea blurs, and preventDefault keeps focus in the input.
                onMouseDown={e => { e.preventDefault(); selectCommand(cmd) }}
                onMouseEnter={() => setCmdHighlight(i)}
                style={{
                  padding: '6px 12px', cursor: 'pointer', display: 'flex', gap: 8, alignItems: 'center',
                  background: i === cmdHighlight ? 'var(--dn-border)' : 'transparent',
                }}
              >
                <span title={cmd.dispatch === 'agent' ? 'agent — takes free-form args' : 'direct'}>
                  {cmd.dispatch === 'agent' ? '🤖' : '⚡'}
                </span>
                <span style={{ color: 'var(--dg-interactive)', minWidth: 96 }}>{cmd.name}</span>
                <span style={{ color: 'var(--dn-text-muted)', fontSize: 11 }}>{cmd.description}</span>
              </div>
            ))}
          </div>
        )}
        <div style={{ display: 'flex', alignItems: 'flex-start', padding: '12px 16px' }}>
          <span style={{ color: 'var(--dg-interactive)', marginRight: 8 }}>&gt;</span>
          <textarea
            ref={inputRef}
            value={input}
            rows={1}
            disabled={!sessionId || status !== 'connected'}
            onChange={e => { setInput(e.target.value); setCmdHighlight(0) }}
            onKeyDown={handleKeyDown}
            placeholder={status === 'connected' ? 'message or /command  (type / for commands)' : 'connecting…'}
            style={{
              flex: 1, background: 'transparent', border: 'none', outline: 'none', resize: 'none',
              color: 'var(--dn-text-bright)', fontFamily: 'var(--font-mono)', fontSize: 13,
            }}
          />
        </div>
      </div>
    </div>
  )
}

const preStyle: React.CSSProperties = {
  margin: '0 0 6px 12px', whiteSpace: 'pre-wrap', fontFamily: 'inherit',
  fontSize: 11, color: 'var(--dn-text-muted)', maxHeight: 120, overflow: 'auto',
}

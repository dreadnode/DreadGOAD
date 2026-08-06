import { useEffect, useMemo, useRef, useState } from 'react'
import Markdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import type { ChatEvent, HealthCheck, Instance } from '../types'
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

// Raw cloud power-state → dot color (mirrors the hook's _STATE normalization).
const INSTANCE_STATE_COLOR: Record<string, string> = {
  running: 'var(--dn-success)',
  stopped: 'var(--dn-text-muted)',
  deallocated: 'var(--dn-text-muted)',
  pending: 'var(--dn-warning)',
  starting: 'var(--dn-warning)',
  creating: 'var(--dn-warning)',
  terminated: 'var(--dn-error)',
}

function InstancesReport({ ev }: { ev: ChatEvent }) {
  const instances = ev.instances ?? []
  const total = ev.total ?? instances.length
  const running = ev.running ?? 0
  return (
    <div style={{ marginBottom: 8 }}>
      <div style={{ marginBottom: 4 }}>
        <Badge text="INSTANCES" color="var(--dg-interactive)" />
        <span style={{ color: 'var(--dn-text-muted)', fontSize: 12 }}>
          {total === 0 ? 'no instances found' : `${total} total · ${running} running`}
        </span>
      </div>
      {total > 0 && (
        <div style={{ display: 'grid', gridTemplateColumns: 'auto 1fr auto', gap: '3px 14px', fontSize: 11, marginLeft: 12 }}>
          {instances.map((inst: Instance, i: number) => {
            const color = INSTANCE_STATE_COLOR[(inst.state || '').toLowerCase()] ?? 'var(--dn-text-muted)'
            return (
              <div key={i} style={{ display: 'contents' }}>
                <span style={{ color, whiteSpace: 'nowrap' }}>● {inst.state}</span>
                <span style={{ color: 'var(--dn-text-bright)', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>{inst.name}</span>
                <span style={{ color: 'var(--dn-text-dim)', whiteSpace: 'nowrap' }}>{inst.private_ip || '—'}</span>
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}

const VALIDATE_STATE: Record<string, { mark: string; color: string }> = {
  failed:  { mark: '✕', color: 'var(--dn-error)' },
  passed:  { mark: '✓', color: 'var(--dn-success)' },
  skipped: { mark: '·', color: 'var(--dn-text-muted)' },
}

function ValidateReport({ ev }: { ev: ChatEvent }) {
  const cats = ev.categories ?? []
  const failures = ev.failures ?? []
  const failed = ev.failed ?? 0
  // Categories that asserted nothing are noise at a glance — collapse them to a
  // count and lead with what actually failed.
  const shown = cats.filter(c => c.state !== 'skipped')
  const skipped = cats.length - shown.length
  return (
    <div style={{ marginBottom: 8 }}>
      <div style={{ marginBottom: 4 }}>
        <Badge text="VALIDATE" color="var(--dg-interactive)" />
        <span style={{ color: failed > 0 ? 'var(--dn-error)' : 'var(--dn-success)', fontSize: 12 }}>
          {ev.passed ?? 0} passed · {failed} failed
          {ev.warnings ? ` · ${ev.warnings} warnings` : ''}
          <span style={{ color: 'var(--dn-text-muted)' }}> of {ev.total ?? 0} checks</span>
        </span>
      </div>

      {failures.length > 0 && (
        <div style={{ marginLeft: 12, marginBottom: 6, fontSize: 11 }}>
          {failures.map((f, i) => (
            <div key={i} style={{ display: 'flex', gap: 8 }}>
              <span style={{ color: 'var(--dn-error)', fontWeight: 700, minWidth: 60 }}>{f.category}</span>
              <span style={{ color: 'var(--dn-text)' }}>{f.name}</span>
            </div>
          ))}
        </div>
      )}

      {/* Category grid, mirroring the CLI's own summary block. */}
      <div style={{
        marginLeft: 12, fontSize: 11,
        display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(190px, 1fr))',
        gap: '1px 14px',
      }}>
        {shown.map(c => {
          const s = VALIDATE_STATE[c.state] ?? VALIDATE_STATE.skipped
          return (
            <div key={c.category} style={{ display: 'flex', gap: 6, alignItems: 'baseline' }}>
              <span style={{ color: s.color }}>{s.mark}</span>
              <span style={{ color: 'var(--dn-text)', flex: 1, overflow: 'hidden', textOverflow: 'ellipsis' }}>
                {c.category}
              </span>
              <span style={{ color: 'var(--dn-text-muted)', fontVariantNumeric: 'tabular-nums' }}>
                {c.passed}/{c.total}
              </span>
            </div>
          )
        })}
      </div>
      {skipped > 0 && (
        <div style={{ marginLeft: 12, marginTop: 4, fontSize: 11, color: 'var(--dn-text-muted)' }}>
          {skipped} categor{skipped === 1 ? 'y' : 'ies'} not configured for this variant
        </div>
      )}
    </div>
  )
}

function ScrubReport({ ev }: { ev: ChatEvent }) {
  const hosts = ev.hosts ?? []
  const found = ev.found ?? 0
  const dryRun = ev.mode !== 'apply'
  // Clean hosts are the expected case; collapse them to a count so the eye
  // lands on the ones that actually had artifacts.
  const dirty = hosts.filter(h => h.found > 0 || h.errors.length > 0)
  const clean = hosts.length - dirty.length
  return (
    <div style={{ marginBottom: 8 }}>
      <div style={{ marginBottom: 4 }}>
        <Badge text="SCRUB" color="var(--dg-interactive)" />
        <span style={{ color: found > 0 ? 'var(--dn-warning)' : 'var(--dn-success)', fontSize: 12 }}>
          {found === 0 ? 'no artifacts found' : `${found} artifact${found === 1 ? '' : 's'} found`}
          <span style={{ color: 'var(--dn-text-muted)' }}> across {hosts.length} host{hosts.length === 1 ? '' : 's'}</span>
        </span>
        {dryRun && (
          // The distinction that matters most: a dry run changed nothing.
          <span style={{
            marginLeft: 8, fontSize: 10, fontWeight: 700, letterSpacing: 0.5,
            color: 'var(--dn-warning)', border: '1px solid var(--dn-warning)',
            borderRadius: 3, padding: '0 5px', textTransform: 'uppercase',
          }}>dry run — nothing removed</span>
        )}
      </div>

      {dirty.length > 0 && (
        <div style={{
          marginLeft: 12, fontSize: 11,
          display: 'grid', gridTemplateColumns: 'auto auto 1fr', gap: '2px 12px',
        }}>
          {dirty.map((h, i) => (
            <div key={i} style={{ display: 'contents' }}>
              <span style={{ color: 'var(--dn-text-bright)', whiteSpace: 'nowrap' }}>{h.host}</span>
              <span style={{ color: 'var(--dn-warning)', fontVariantNumeric: 'tabular-nums', whiteSpace: 'nowrap' }}>
                {h.found} found · {h.removed} {dryRun ? 'would remove' : 'removed'}
              </span>
              <span style={{ color: 'var(--dn-error)' }}>
                {h.errors.length > 0 ? h.errors.join('; ') : ''}
              </span>
            </div>
          ))}
        </div>
      )}
      {clean > 0 && (
        <div style={{ marginLeft: 12, marginTop: 4, fontSize: 11, color: 'var(--dg-node-label)' }}>
          {clean} host{clean === 1 ? '' : 's'} already clean
        </div>
      )}
    </div>
  )
}

/**
 * Elapsed turn time as `m:ss`, or `h:mm:ss` once it passes an hour — a `/up`
 * legitimately runs for tens of minutes, so minutes alone would wrap awkwardly.
 * Exported for testing.
 */
export function formatElapsed(ms: number): string {
  const total = Math.max(Math.floor(ms / 1000), 0)
  const hours = Math.floor(total / 3600)
  const mins = Math.floor((total % 3600) / 60)
  const secs = total % 60
  const pad = (n: number) => String(n).padStart(2, '0')
  return hours > 0 ? `${hours}:${pad(mins)}:${pad(secs)}` : `${mins}:${pad(secs)}`
}

interface Props {
  sessionId: string | null
  messages: ChatEvent[]
  status: ConnectionStatus
  onSend: (content: string) => void
  processing: boolean
  /** Epoch ms the in-flight turn began; 0 when idle. Supplied by the server on
   *  resume so the elapsed time is true across a reload. */
  turnStartedAt: number
  onCancel: () => void
  model?: string
  onOpenSettings?: () => void
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
          <div style={{
            background: 'var(--dn-surface)', border: '1px solid var(--dn-border-lt)',
            borderRadius: 6, padding: '8px 12px', display: 'inline-block', maxWidth: '90%',
          }}>
            <span style={{ color: 'var(--dg-interactive)', marginRight: 8 }}>&gt;</span>
            <span style={{ color: 'var(--dn-text-bright)', whiteSpace: 'pre-wrap' }}>{ev.content}</span>
          </div>
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
      if (ev.phase === 'start') {
        return <div style={{ marginBottom: 6 }}><Badge text="CMD" color="var(--dg-brand)" /><span style={{ color: 'var(--dn-text-muted)', fontSize: 12 }}>{ev.command}</span></div>
      }
      // A cancel is not a failure, and "exit -2" tells an operator nothing.
      return ev.cancelled
        ? <div style={{ marginBottom: 6, fontSize: 12, color: 'var(--dn-warning)' }}>
            cancelled — stopped early, output is incomplete
          </div>
        : <div style={{ marginBottom: 6, fontSize: 12, color: ev.exit_code ? 'var(--dn-error)' : 'var(--dn-success)' }}>exit {String(ev.exit_code)}</div>
    case 'command_progress':
      return <div style={{ fontSize: 11, color: 'var(--dn-text-dim)', whiteSpace: 'pre-wrap' }}>{ev.line}</div>
    case 'check_run':
      return <div style={{ marginBottom: 6 }}><Badge text="CHECK" color="var(--dg-interactive)" /><span style={{ color: 'var(--dn-text-muted)', fontSize: 12 }}>{ev.error ? `check failed: ${String(ev.error)}` : `range verified — ${ev.hosts_updated ?? 0} host(s) updated`}</span></div>
    case 'health_report':
      return <HealthReport ev={ev} />
    case 'instances_report':
      return <InstancesReport ev={ev} />
    case 'validate_report':
      return <ValidateReport ev={ev} />
    case 'scrub_report':
      return <ScrubReport ev={ev} />
    case 'status':
      return <div style={{ margin: '6px 0', fontSize: 11, color: 'var(--dn-text-dim)', fontStyle: 'italic' }}>{ev.content}</div>
    case 'error':
      return <div style={{ marginBottom: 6 }}><Badge text="ERROR" color="var(--dn-error)" /><span style={{ color: 'var(--dn-error)', fontSize: 12 }}>{ev.message}</span></div>
    default:
      return null
  }
}

export default function TerminalChat({ sessionId, messages, status, onSend, processing, turnStartedAt, onCancel, model, onOpenSettings }: Props) {
  const [input, setInput] = useState('')
  const [commands, setCommands] = useState<CommandDef[]>([])
  const [cmdHighlight, setCmdHighlight] = useState(0)
  const endRef = useRef<HTMLDivElement>(null)
  const inputRef = useRef<HTMLTextAreaElement>(null)
  const activeCmdRef = useRef<HTMLDivElement>(null)

  useEffect(() => { endRef.current?.scrollIntoView({ behavior: 'smooth' }) }, [messages])

  // Auto-grow the input upward as it wraps to multiple lines (like ALFRED).
  // Reset to 'auto' first so it also shrinks back when text is deleted.
  useEffect(() => {
    const el = inputRef.current
    if (!el) return
    el.style.height = 'auto'
    el.style.height = `${Math.min(el.scrollHeight, 200)}px`
  }, [input])

  // Keep the keyboard-highlighted command scrolled into the popup's viewport
  // (the menu is a fixed-height scroll box; arrow-nav can move past the fold).
  useEffect(() => { activeCmdRef.current?.scrollIntoView({ block: 'nearest' }) }, [cmdHighlight])

  // Load the slash-command registry once for the autocomplete menu (§5.1).
  // Sorted by name: the registry is grouped by lifecycle, but in a menu you
  // scan for a command you already know the name of, so alphabetical wins.
  useEffect(() => {
    api.commands()
      .then(r => setCommands([...r.commands].sort((a, b) => a.name.localeCompare(b.name))))
      .catch(() => {})
  }, [])

  // Autocomplete: filter to the typed `/`-token; hide once a space is typed (args).
  // Declared before the Esc handler below, which needs to know if it's open.
  const firstToken = input.split(' ')[0]
  const filteredCommands = useMemo(
    () => (input.startsWith('/') ? commands.filter(c => c.name.startsWith(firstToken)) : []),
    [commands, input, firstToken],
  )
  const showCmdMenu = filteredCommands.length > 0 && !input.includes(' ')

  // Stopwatch for the current turn. Counts from the supplied start rather than
  // from mount, so a reload mid-turn shows the true elapsed time instead of
  // restarting at 0:00. Re-keyed when the turn changes; the interval is cleared
  // the moment it ends, so nothing ticks while the pane is idle.
  const [elapsed, setElapsed] = useState(0)
  useEffect(() => {
    if (!processing) return
    const started = turnStartedAt || Date.now()
    const tick = () => setElapsed(Date.now() - started)
    tick()
    const id = setInterval(tick, 1000)
    return () => clearInterval(id)
  }, [processing, turnStartedAt])

  // Esc cancels the in-flight command/turn (sends {type:cancel} → SIGINT, §5.4).
  // Suppressed while the slash-command menu is open: Esc there means "close the
  // menu", and this document-level listener would otherwise *also* fire and kill
  // a running command. The menu's own handler (below) can't prevent that on its
  // own, since this listener is native and separate from React's dispatch.
  useEffect(() => {
    if (!processing || showCmdMenu) return
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') onCancel() }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [processing, showCmdMenu, onCancel])

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
      // stopPropagation as well as preventDefault: belt-and-braces so this Esc
      // can never reach the document-level cancel listener.
      if (e.key === 'Escape') {
        e.preventDefault()
        e.stopPropagation()
        setInput('')
        return
      }
    }
    if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); submit() }
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%', background: 'var(--dn-bg)', borderRight: '1px solid var(--dn-border)' }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', padding: '12px 16px', borderBottom: '1px solid var(--dn-border)', background: 'var(--dn-black)' }}>
        <span style={{ color: 'var(--dg-brand)', fontSize: 13, fontWeight: 700 }}>AGENT</span>
        <div style={{ display: 'flex', alignItems: 'center', gap: 12, minWidth: 0 }}>
          {model && (
            <span
              role="button"
              tabIndex={0}
              onClick={onOpenSettings}
              onKeyDown={e => { if (e.key === 'Enter') onOpenSettings?.() }}
              title="Change model"
              style={{
                color: 'var(--dg-interactive)', fontSize: 11, cursor: 'pointer',
                textDecoration: 'underline', textDecorationStyle: 'dotted', textUnderlineOffset: 3,
                maxWidth: 260, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap',
              }}
            >{model}</span>
          )}
          <span
            title={status}
            style={{
              width: 8, height: 8, borderRadius: '50%', flexShrink: 0,
              background: status === 'connected' ? 'var(--dn-success)'
                : status === 'connecting' ? 'var(--dn-warning)' : 'var(--dn-error)',
              boxShadow: status === 'connected' ? '0 0 6px var(--dn-success)' : 'none',
            }}
          />
        </div>
      </div>
      <div style={{ flex: 1, overflowY: 'auto', padding: '12px 16px' }}>
        {!sessionId && <div style={{ color: 'var(--dn-text-dim)', fontSize: 13 }}>Create or select a session to begin.</div>}
        {messages.map((ev, i) => <Message key={ev._cid ?? i} ev={ev} />)}
        {processing && (
          <div style={{ display: 'flex', gap: 16, alignItems: 'baseline', marginTop: 4 }}>
            <span className="agent-working" style={{
              color: 'var(--dg-interactive)', fontSize: 13, fontFamily: 'var(--font-mono)', opacity: 0.6,
            }}>Agent working</span>
            <span
              // Tabular figures so the digits don't shuffle the row every tick.
              style={{
                color: 'var(--dg-node-label)', fontSize: 12,
                fontFamily: 'var(--font-mono)', fontVariantNumeric: 'tabular-nums',
              }}
            >{formatElapsed(elapsed)}</span>
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
            // Taller now that each row carries a second line — still shows ~5
            // rows, which is what arrow-key navigation needs to feel anchored.
            maxHeight: 300, overflowY: 'auto', fontFamily: 'var(--font-mono)', fontSize: 12,
          }}>
            {filteredCommands.map((cmd, i) => (
              <div
                key={cmd.name}
                ref={i === cmdHighlight ? activeCmdRef : undefined}
                // onMouseDown (not onClick) so the item is chosen before the
                // textarea blurs, and preventDefault keeps focus in the input.
                onMouseDown={e => { e.preventDefault(); selectCommand(cmd) }}
                onMouseEnter={() => setCmdHighlight(i)}
                style={{
                  padding: '7px 12px', cursor: 'pointer', display: 'flex', gap: 8,
                  alignItems: 'flex-start',
                  background: i === cmdHighlight ? 'var(--dn-border)' : 'transparent',
                }}
              >
                <span
                  title={cmd.dispatch === 'agent'
                    ? 'agent — interprets free-form arguments into CLI flags'
                    : 'direct — runs the CLI verb as-is, no LLM involved'}
                >{cmd.dispatch === 'agent' ? '🤖' : '⚡'}</span>
                <span style={{ color: 'var(--dg-interactive)', minWidth: 96, flexShrink: 0 }}>
                  {cmd.name}
                </span>
                <span style={{ minWidth: 0 }}>
                  <span style={{ color: 'var(--dn-text-bright)', fontSize: 11 }}>
                    {cmd.description}
                  </span>
                  {/* Second line: the consequence, then the verb it maps to.
                      This is what stops someone running /variant on a live range. */}
                  <span style={{ display: 'block', fontSize: 10, marginTop: 2 }}>
                    {cmd.detail && (
                      <span style={{ color: 'var(--dg-node-label)' }}>{cmd.detail}</span>
                    )}
                    {cmd.cli && (
                      <span style={{ color: 'var(--dn-text-muted)' }}>
                        {cmd.detail ? '  ·  ' : ''}{cmd.cli}
                      </span>
                    )}
                  </span>
                </span>
              </div>
            ))}
          </div>
        )}
        <div style={{ display: 'flex', alignItems: 'flex-start', padding: '12px 16px' }}>
          <span style={{ color: 'var(--dg-interactive)', marginRight: 8, fontSize: 13, lineHeight: '20px' }}>&gt;</span>
          <textarea
            ref={inputRef}
            value={input}
            rows={1}
            disabled={!sessionId || status !== 'connected'}
            onChange={e => { setInput(e.target.value); setCmdHighlight(0) }}
            onKeyDown={handleKeyDown}
            placeholder={
              status !== 'connected'
                ? `${status}…`
                : !sessionId
                  ? 'create or select a session (+ NEW) to begin'
                  : 'message or /command  (type / for commands)'
            }
            style={{
              flex: 1, background: 'transparent', border: 'none', outline: 'none', resize: 'none',
              color: 'var(--dn-text-bright)', fontFamily: 'var(--font-mono)', fontSize: 13,
              lineHeight: '20px', padding: 0, margin: 0,
              maxHeight: 200, overflowY: 'auto', display: 'block',
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

import { useEffect, useMemo, useRef, useState } from 'react'
import Markdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import type {
  ChatEvent,
  ExecReportEvent,
  HealthReportEvent,
  InstancesReportEvent,
  ScrubReportEvent,
  SecurityReportEvent,
  ToolStartEvent,
  ValidateReportEvent,
} from '../types'
import type { ConnectionStatus } from '../hooks/useWebSocket'
import { api, type CommandDef } from '../api'
import { agentVerb } from '../agentVerbs'
import { buildHelpLines, type HelpLineKind } from '../help'
import TerminalComposer from './TerminalComposer'
import { COPY_COMMAND, HELP_COMMAND } from './terminalCommands'

export { mergeHistory } from './terminalChatHistory'

function formatTokens(n: number): string {
  if (n >= 1_000_000) { const v = n / 1_000_000; return (v >= 10 ? Math.round(v) : +v.toFixed(1)) + 'M' }
  if (n >= 1_000) { const v = n / 1_000; return (v >= 10 ? Math.round(v) : +v.toFixed(1)) + 'k' }
  return String(n)
}

const HEALTH_COLOR: Record<string, string> = {
  OK: 'var(--dn-success)',
  FAIL: 'var(--dn-error)',
  WARN: 'var(--dn-warning)',
  SKIP: 'var(--dn-text-muted)',
}

function HealthReport({ ev }: { ev: HealthReportEvent }) {
  const checks = ev.checks ?? []
  const failed = ev.failed ?? 0
  const warned = ev.warned ?? 0
  const summaryColor = failed > 0 ? 'var(--dn-error)' : warned > 0 ? 'var(--dn-warning)' : 'var(--dn-success)'
  return (
    <div style={{ marginBottom: 8 }}>
      <div style={{ marginBottom: 4 }}>
        <Badge text="HEALTH" color="var(--dg-interactive)" />
        <span style={{ color: summaryColor, fontSize: 12 }}>
          {ev.passed ?? 0} passed · {failed} failed · {warned} warned · {ev.skipped ?? 0} skipped
        </span>
      </div>
      <div style={{ display: 'grid', gridTemplateColumns: 'auto auto 1fr', gap: '2px 10px', fontSize: 11, marginLeft: 12 }}>
        {checks.map((c, i) => (
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

const SECURITY_COLOR: Record<string, string> = {
  OK: 'var(--dn-success)',
  FAIL: 'var(--dn-error)',
  WARN: 'var(--dn-warning)',
  SKIP: 'var(--dn-text-muted)',
}

const SEVERITY_ORDER: Record<string, number> = { critical: 0, high: 1, info: 2 }

function SecurityReport({ ev }: { ev: SecurityReportEvent }) {
  const checks = ev.security_checks ?? []
  const failed = ev.failed ?? 0
  const warned = ev.warned ?? 0
  const summaryColor = failed > 0 ? 'var(--dn-error)' : warned > 0 ? 'var(--dn-warning)' : 'var(--dn-success)'
  const sorted = [...checks].sort((a, b) =>
    (SEVERITY_ORDER[a.severity] ?? 9) - (SEVERITY_ORDER[b.severity] ?? 9)
  )
  return (
    <div style={{ marginBottom: 8 }}>
      <div style={{ marginBottom: 4 }}>
        <Badge text="SECURITY" color="var(--dg-interactive)" />
        <span style={{ color: summaryColor, fontSize: 12 }}>
          {ev.passed ?? 0} passed · {failed} failed · {warned} warned · {ev.skipped ?? 0} skipped
        </span>
      </div>
      <div style={{ display: 'grid', gridTemplateColumns: 'auto auto auto 1fr', gap: '2px 10px', fontSize: 11, marginLeft: 12 }}>
        {sorted.map((c, i) => (
          <div key={i} style={{ display: 'contents' }}>
            <span style={{ color: SECURITY_COLOR[c.status] ?? 'var(--dn-text-muted)', fontWeight: 700 }}>{c.status}</span>
            <span style={{ color: 'var(--dn-text-dim)', fontSize: 10 }}>{c.severity}</span>
            <span style={{ color: 'var(--dn-text-muted)' }}>{c.resource}</span>
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

function InstancesReport({ ev }: { ev: InstancesReportEvent }) {
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
          {instances.map((inst, i) => {
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

function ValidateReport({ ev }: { ev: ValidateReportEvent }) {
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
          {skipped} {skipped === 1 ? 'category' : 'categories'} not configured for this variant
        </div>
      )}
    </div>
  )
}

function ScrubReport({ ev }: { ev: ScrubReportEvent }) {
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

function ExecReport({ ev }: { ev: ExecReportEvent }) {
  const results = ev.results ?? []
  const succeeded = ev.succeeded ?? 0
  const total = ev.total ?? results.length
  const allOk = succeeded === total
  return (
    <div style={{ marginBottom: 8 }}>
      <div style={{ marginBottom: 4 }}>
        <Badge text="EXEC" color="var(--dg-interactive)" />
        <span style={{ color: allOk ? 'var(--dn-success)' : 'var(--dn-error)', fontSize: 12 }}>
          {succeeded}/{total} succeeded
        </span>
      </div>
      {results.map((r, i) => (
        <div key={i} style={{ marginLeft: 12, marginBottom: 6, fontSize: 11 }}>
          <div style={{ display: 'flex', gap: 8, alignItems: 'baseline' }}>
            <span style={{ color: 'var(--dn-text-bright)' }}>{r.host}</span>
            {/* The CLI's convention is "Success"; "Succeeded" is Azure's raw
                ARM state, accepted so this can't drift from the Go side. */}
            <span style={{
              color: ['success', 'succeeded'].includes(r.status?.toLowerCase() ?? '')
                ? 'var(--dn-success)' : 'var(--dn-error)',
            }}>{r.status}</span>
          </div>
          {/* Output is raw host text — pre-wrapped, never rendered as markdown.
              It comes off a deliberately vulnerable range and is untrusted. */}
          {r.stdout ? (
            <pre style={preStyle}>{r.stdout}</pre>
          ) : null}
          {r.stderr ? (
            <pre style={{ ...preStyle, color: 'var(--dn-error)' }}>{r.stderr}</pre>
          ) : null}
          {!r.stdout && !r.stderr ? (
            <span style={{ color: 'var(--dg-node-label)' }}>(no output)</span>
          ) : null}
        </div>
      ))}
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
  /** Seeds the flavour verb; changes once per turn. See agentVerbs.ts. */
  verbSeed: number
  onCancel: () => void
  model?: string
  onOpenSettings?: () => void
  confirmationBlocked?: boolean
}

function Badge({ text, color }: { text: string; color: string }) {
  return (
    <span style={{
      display: 'inline-block', padding: '1px 6px', borderRadius: 3,
      background: 'var(--dn-surface)', color, fontSize: 11, marginRight: 6,
    }}>{text}</span>
  )
}

function toolSummary(ev: ToolStartEvent): string {
  let args = ''
  try { args = JSON.stringify(JSON.parse(ev.args || '{}')) } catch { args = ev.args || '' }
  return `${ev.tool} ${args}`.trim()
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
      // For anything that had already reached the cloud, "cancelled" alone was
      // a false claim: we stop watching, the deallocate or playbook finishes
      // anyway. Say which of the two happened.
      return ev.cancelled
        ? <div style={{ marginBottom: 6, fontSize: 12, color: 'var(--dn-warning)' }}>
            {ev.still_running
              ? 'stopped watching — the operation was already sent and will finish '
                + 'on its own; range state re-read below'
              : 'cancelled — stopped early, output is incomplete'}
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
    case 'exec_report':
      return <ExecReport ev={ev} />
    case 'security_report':
      return <SecurityReport ev={ev} />
    case 'status':
      return <div style={{ margin: '6px 0', fontSize: 11, color: 'var(--dn-text-dim)', fontStyle: 'italic' }}>{ev.content}</div>
    case 'error':
      return <div style={{ marginBottom: 6 }}><Badge text="ERROR" color="var(--dn-error)" /><span style={{ color: 'var(--dn-error)', fontSize: 12 }}>{ev.message}</span></div>
    default:
      return null
  }
}

// Keyed off the line's declared kind, not its text. Detail paragraphs
// often open with a command name ("/scrub deletes ..."), so styling by a
// leading slash painted five of them as command rows.
const HELP_LINE_COLOR: Record<HelpLineKind, string> = {
  title: 'var(--dg-interactive)',
  command: 'var(--dn-text-bright)',
  detail: 'var(--dg-node-label)',
  blank: 'transparent',
}

/** The workflow guide, rendered as a monospaced block. */
function HelpPanel({ commands }: { commands: CommandDef[] }) {
  const lines = useMemo(() => buildHelpLines(commands), [commands])
  return (
    <div style={{
      fontFamily: 'var(--font-mono)', fontSize: 12, lineHeight: 1.6,
      whiteSpace: 'pre-wrap', marginBottom: 12,
    }}>
      {lines.map((line, i) => (
        <div key={i} style={{
          color: HELP_LINE_COLOR[line.kind],
          fontWeight: line.kind === 'title' ? 700 : 400,
          marginTop: line.kind === 'title' && i > 0 ? 6 : 0,
        }}>{line.text || ' '}</div>
      ))}
    </div>
  )
}

/**
 * Keep the guide's transcript position inside the transcript.
 *
 * A resume replaces `messages` wholesale, and the replacement can be shorter
 * than what was on screen. `helpAfter` then points past the end, where
 * `slice(0, helpAfter)` returns everything and `slice(helpAfter)` returns
 * nothing: the guide pins itself to the bottom and every later message stacks
 * *above* it, until the transcript grows past the stale index and it silently
 * jumps back into the middle.
 *
 * Re-anchoring to the end rather than clamping at render time is deliberate —
 * the recorded position is meaningless once the transcript it referred to is
 * gone, so the guide should stay where it currently is and new output should
 * land below it. mergeHistory guards the same hazard for client-only entries.
 *
 * Exported so the behaviour can be checked without a DOM.
 */
export function reanchorHelp(helpAfter: number | null, length: number): number | null {
  return helpAfter !== null && helpAfter > length ? length : helpAfter
}

export default function TerminalChat({ sessionId, messages, status, onSend, processing, turnStartedAt, verbSeed, onCancel, model, onOpenSettings, confirmationBlocked }: Props) {
  // Transcript position the guide was last requested at; null = never asked.
  // An empty pane shows it regardless, so a new session opens on the workflow.
  const [helpAfter, setHelpAfter] = useState<number | null>(null)
  const [commands, setCommands] = useState<CommandDef[]>([])
  // False once the command catalog fails to load: nothing can be classified,
  // so the destructive-command confirm has to assume the worst.
  const [catalogOk, setCatalogOk] = useState(true)
  const endRef = useRef<HTMLDivElement>(null)
  // The scrolling transcript container — needed to pin it to the TOP for the
  // guide, which endRef (an anchor at the bottom) can't express.
  const scrollRef = useRef<HTMLDivElement>(null)
  const pinnedRef = useRef(true)
  const autoScrollingRef = useRef(false)
  const autoScrollTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const [showJump, setShowJump] = useState(false)

  const sessionTokens = useMemo(() => {
    let inp = 0, out = 0
    for (const ev of messages) {
      if (ev.kind === 'generation' && ev.usage) {
        inp += ev.usage.input_tokens || 0
        out += ev.usage.output_tokens || 0
      }
    }
    return { input: inp, output: out }
  }, [messages])

  const handleScroll = () => {
    if (autoScrollingRef.current) return
    const el = scrollRef.current
    if (!el) return
    const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 48
    pinnedRef.current = atBottom
    setShowJump(!atBottom)
  }

  // Follow the transcript only while the user is pinned to the bottom.
  // The guide is taller than the pane, so scrolling to the end would open a new
  // session on its last line — the reader needs its first line.
  // autoScrollingRef suppresses handleScroll during the smooth animation so
  // intermediate scroll positions don't unpin the view.
  useEffect(() => {
    if (messages.length === 0) {
      pinnedRef.current = true
      setShowJump(false)
      scrollRef.current?.scrollTo({ top: 0 })
      return
    }
    if (pinnedRef.current) {
      autoScrollingRef.current = true
      endRef.current?.scrollIntoView({ behavior: 'smooth' })
      if (autoScrollTimer.current) clearTimeout(autoScrollTimer.current)
      autoScrollTimer.current = setTimeout(() => { autoScrollingRef.current = false }, 600)
    } else {
      setShowJump(true)
    }
  }, [messages])

  // The guide is per-session. This component is never remounted when tabs
  // change (App renders one instance and swaps its props), so without this the
  // index would carry into the next session — showing the panel where /help was
  // never typed, and at an offset that means nothing in that transcript.
  useEffect(() => {
    setHelpAfter(null)
    pinnedRef.current = true
    setShowJump(false)
  }, [sessionId])

  // See reanchorHelp: a resume can hand us a shorter transcript than the one
  // the guide's position was recorded against.
  useEffect(() => {
    setHelpAfter(prev => reanchorHelp(prev, messages.length))
  }, [messages.length])

  // Load the slash-command registry once for the autocomplete menu (§5.1).
  // Sorted by name: the registry is grouped by lifecycle, but in a menu you
  // scan for a command you already know the name of, so alphabetical wins.
  // HELP_COMMAND is merged in client-side — it has no CLI verb, so it isn't in
  // the server registry, but it must still be discoverable by typing "/".
  useEffect(() => {
    api.commands()
      .then(r => setCommands(
        [...r.commands, HELP_COMMAND, COPY_COMMAND].sort((a, b) => a.name.localeCompare(b.name)),
      ))
      .catch(() => {
        // Falling back to help-only leaves every command unclassifiable, which
        // matters because the destructive-command confirm below is keyed on
        // catalog data. Recorded so that gate can fail closed rather than
        // silently waving /destroy through.
        setCommands([HELP_COMMAND, COPY_COMMAND])
        setCatalogOk(false)
      })
  }, [])

  // One verb per turn. Pure in render because the seed only changes when a turn
  // starts (App owns it per session): latching locally would re-roll the word
  // when a tab switch flips `processing`, and drawing at random here would
  // reshuffle it every second, since the stopwatch below re-renders this row.
  const verb = agentVerb(verbSeed)

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

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%', background: 'var(--dn-bg)', borderRight: '1px solid var(--dn-border)', position: 'relative' }}>
      {/* minHeight is shared with RangeView's header so the two pane banners
          line up across the split — see --dg-pane-header-h in index.css. */}
      <div style={{
        display: 'flex', justifyContent: 'space-between', alignItems: 'center',
        padding: '12px 16px', borderBottom: '1px solid var(--dn-border)',
        background: 'var(--dn-black)', minHeight: 'var(--dg-pane-header-h)',
      }}>
        <span style={{ color: 'var(--dg-brand)', fontSize: 13, fontWeight: 700 }}>AGENT</span>
        <div style={{ display: 'flex', alignItems: 'center', gap: 12, minWidth: 0 }}>
          <span
            title={status}
            style={{
              width: 8, height: 8, borderRadius: '50%', flexShrink: 0,
              background: status === 'connected' ? 'var(--dn-success)'
                : status === 'connecting' ? 'var(--dn-warning)' : 'var(--dn-error)',
              boxShadow: status === 'connected' ? '0 0 6px var(--dn-success)' : 'none',
            }}
          />
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
          {(sessionTokens.input > 0 || sessionTokens.output > 0) && (
            <span
              style={{ fontSize: 10, whiteSpace: 'nowrap' }}
              title={`Input: ${sessionTokens.input.toLocaleString()} tokens\nOutput: ${sessionTokens.output.toLocaleString()} tokens`}
            ><span style={{ color: '#fff' }}>{'↑'}</span><span style={{ color: 'var(--dg-brand)' }}>{formatTokens(sessionTokens.input)}</span>{' '}<span style={{ color: '#fff' }}>{'↓'}</span><span style={{ color: 'var(--dg-brand)' }}>{formatTokens(sessionTokens.output)}</span></span>
          )}
        </div>
      </div>
      <div ref={scrollRef} onScroll={handleScroll} style={{ flex: 1, overflowY: 'auto', padding: '12px 16px', position: 'relative' }}>
        {!sessionId && <div style={{ color: 'var(--dn-text-dim)', fontSize: 13 }}>Create or select a session to begin.</div>}
        {/* The guide leads an empty pane, so a fresh session opens on the
            workflow rather than a blank screen. After /help it renders at the
            point in the transcript where it was asked for, so later output
            still lands below it and the scroll position stays truthful. */}
        {messages.length === 0 && <HelpPanel commands={commands} />}
        {messages.slice(0, helpAfter ?? messages.length)
          .map((ev, i) => <Message key={ev.seq != null ? `s${ev.seq}` : (ev._cid ?? i)} ev={ev} />)}
        {helpAfter !== null && messages.length > 0 && <HelpPanel commands={commands} />}
        {helpAfter !== null && messages.slice(helpAfter)
          .map((ev, i) => <Message key={ev.seq != null ? `s${ev.seq}` : (ev._cid ?? `h${i}`)} ev={ev} />)}
        {processing && (
          <div style={{ display: 'flex', gap: 16, alignItems: 'baseline', marginTop: 4 }}>
            {/* Colour and opacity live in the stylesheet, not here: the shimmer
                paints the text with a clipped gradient, which an inline `color`
                would override and an inline `opacity` would fade along with the
                highlight, flattening the sweep. */}
            <span className="agent-working" style={{
              fontSize: 13, fontFamily: 'var(--font-mono)',
            }}>Agent {verb}</span>
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
      {showJump && (
        <button
          onClick={() => {
            pinnedRef.current = true
            autoScrollingRef.current = true
            setShowJump(false)
            endRef.current?.scrollIntoView({ behavior: 'smooth' })
            if (autoScrollTimer.current) clearTimeout(autoScrollTimer.current)
            autoScrollTimer.current = setTimeout(() => { autoScrollingRef.current = false }, 600)
          }}
          style={{
            position: 'absolute', bottom: 72, left: '50%', transform: 'translateX(-50%)',
            zIndex: 30, padding: '4px 14px', borderRadius: 12,
            border: '1px solid var(--dn-border-lt)', background: 'var(--dn-surface)',
            color: 'var(--dg-interactive)', fontSize: 11, cursor: 'pointer',
            fontFamily: 'var(--font-mono)', boxShadow: '0 2px 8px rgba(0,0,0,0.4)',
          }}
        >↓ jump to latest</button>
      )}
      <TerminalComposer
        sessionId={sessionId}
        messages={messages}
        status={status}
        processing={processing}
        commands={commands}
        catalogOk={catalogOk}
        confirmationBlocked={confirmationBlocked}
        onSend={onSend}
        onCancel={onCancel}
        onShowHelp={setHelpAfter}
      />
    </div>
  )
}

const preStyle: React.CSSProperties = {
  margin: '0 0 6px 12px', whiteSpace: 'pre-wrap', fontFamily: 'inherit',
  fontSize: 11, color: 'var(--dn-text-muted)', maxHeight: 120, overflow: 'auto',
}

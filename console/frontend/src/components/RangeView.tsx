import { Fragment, useCallback, useEffect, useMemo, useState } from 'react'
import {
  ReactFlow,
  Background,
  Controls,
  useNodesState,
  type Node,
  type NodeProps,
  type NodeChange,
} from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import { api } from '../api'
import type { RangeDoc, RangeHost, Session } from '../types'

const ROLE_ICON: Record<string, string> = {
  dc: '🌐', member: '🖥️', workstation: '💻', bastion: '🛡️',
  attackbox: '☠️', linux: '🐧', other: '❔',
}
// Drives both the status text and the node border. The three semantic colours
// clear AA on --dn-surface (8.6 / 8.6 / 4.6); the two neutral states used the
// muted/dim tokens at 3.0 and 1.8, which left "stopped" and "unknown" hosts
// effectively unreadable — they get the calibrated node greys instead.
const STATUS_COLOR: Record<string, string> = {
  running: 'var(--dn-success)', stopped: 'var(--dg-node-value)',
  provisioning: 'var(--dn-warning)', absent: 'var(--dn-error)',
  unknown: 'var(--dg-node-label)',
}

// Vertical tiers: access enters at the top and reaches the lab below it.
const TIER: Record<string, number> = {
  bastion: 0, attackbox: 0,   // ingress
  dc: 1,                      // domain controllers
  member: 2, workstation: 2,  // domain members
  linux: 3, other: 3,         // extensions & everything else
}
const INGRESS_ROLES = new Set(['bastion', 'attackbox'])
const TIER_Y = 165
const COL_W = 220

// The bastion is Azure's *managed* Bastion service, not a VM — it never appears
// in `lab status`, so its host would sit permanently at "absent". Render it as
// a service instead of as a missing machine.
const isManagedService = (h: RangeHost) => h.role === 'bastion'

// Operating system per role. Derived from the lab definition, not read off the
// VM: `lab status --json` reports no OS, and for an Active Directory range the
// mapping is structural rather than a guess — a DC is Windows Server by
// definition, the attack box is the Kali image, extension machines are Linux.
const ROLE_OS: Record<string, string> = {
  dc: 'Windows Server',
  member: 'Windows Server',
  workstation: 'Windows',
  attackbox: 'Kali Linux',
  linux: 'Linux',
}

/** A label/value line in the node's detail block. */
function NodeRow({ label, value, title }: { label: string; value: string; title?: string }) {
  return (
    <div style={{ display: 'flex', gap: 6, fontSize: 10, marginTop: 2 }}>
      <span style={{ color: 'var(--dn-text-muted)', flexShrink: 0 }}>{label}</span>
      <span
        title={title ?? value}
        style={{
          color: 'var(--dg-node-value)', overflow: 'hidden',
          textOverflow: 'ellipsis', whiteSpace: 'nowrap',
        }}
      >{value}</span>
    </div>
  )
}

function HostNode({ data }: NodeProps) {
  const h = data as unknown as RangeHost
  const ingress = INGRESS_ROLES.has(h.role)
  const service = isManagedService(h)
  const color = service ? 'var(--dn-electric)' : (STATUS_COLOR[h.status] ?? STATUS_COLOR.unknown)
  return (
    <div style={{
      background: 'var(--dn-surface)',
      border: `1px solid ${color}`,
      // Ingress nodes are the way in — give them a visible edge.
      boxShadow: ingress ? `0 0 0 1px ${color}55` : 'none',
      // Wider now that VM names are shown; they still ellipsize (full name in
      // the tooltip) rather than stretching the node to ~40 characters.
      borderRadius: 6, padding: '8px 12px', minWidth: 140, maxWidth: 210,
      fontFamily: 'var(--font-mono)', color: 'var(--dn-text)',
    }}>
      {ingress && (
        <div style={{
          fontSize: 9, letterSpacing: 0.5, color, marginBottom: 4, fontWeight: 700,
        }}>◆ INGRESS</div>
      )}
      <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
        <span style={{ fontSize: 18 }}>{ROLE_ICON[h.role] ?? ROLE_ICON.other}</span>
        <span style={{ fontSize: 13, fontWeight: 700, color: 'var(--dn-text-bright)' }}>{h.hostname}</span>
      </div>
      <div style={{ fontSize: 11, color: 'var(--dg-node-label)', marginTop: 4 }}>
        {h.role}{h.domain ? ` · ${h.domain}` : ''}
      </div>
      <div style={{ display: 'flex', gap: 8, marginTop: 6, fontSize: 11 }}>
        {service
          ? <span style={{ color }} title="Azure Bastion is a managed service, not a VM — it never shows in lab status">managed service</span>
          : <span style={{ color }}>● {h.status}</span>}
        {!service && h.health !== 'unknown' && <span style={{ color: 'var(--dg-node-label)' }}>{h.health}</span>}
      </div>
      {h.ip_private && (
        // Read digit-by-digit, so the highest-contrast tier; tabular figures
        // keep the octets aligned between stacked nodes.
        <div style={{
          fontSize: 11, color: 'var(--dg-node-value)', marginTop: 3,
          fontVariantNumeric: 'tabular-nums', letterSpacing: 0.2,
        }}>{h.ip_private}</div>
      )}

      {/* Detail block. Each row appears only when its value exists, so a node
          that hasn't been discovered yet stays compact rather than showing a
          column of dashes. */}
      {!service && (ROLE_OS[h.role] || h.cloud_name || h.ip_public) && (
        <div style={{ marginTop: 6, paddingTop: 5, borderTop: '1px solid var(--dn-border)' }}>
          {ROLE_OS[h.role] && (
            <NodeRow
              label="os"
              value={ROLE_OS[h.role]}
              title="Derived from the host's role in the lab definition, not read from the VM"
            />
          )}
          {h.cloud_name && <NodeRow label="vm" value={h.cloud_name} />}
          {h.ip_public && <NodeRow label="pub" value={h.ip_public} />}
        </div>
      )}
    </div>
  )
}

const nodeTypes = { host: HostNode }

// Range state only refreshes when a command completes — there is no polling.
// Past this age the reading is more likely stale than quiet, which is worth
// flagging: a wedged command leaves the view frozen with no other symptom.
const STALE_AFTER_MS = 5 * 60_000
const TICK_MS = 30_000

/** How the "last updated" label should read. Null means render nothing. */
export interface CheckedLabel {
  label: string
  stale: boolean
  exact: string
}

/**
 * Describe when the range was last refreshed. Pure and exported so the age
 * boundaries and the null/unparseable/clock-skew paths can be tested directly
 * rather than through a reimplementation.
 */
export function describeChecked(
  iso: string | null | undefined,
  now: number,
): CheckedLabel | null {
  if (!iso) {
    return { label: 'never checked', stale: true, exact: 'No command has run yet' }
  }
  const at = new Date(iso).getTime()
  if (Number.isNaN(at)) return null      // unparseable → say nothing, not "NaN ago"
  const age = Math.max(now - at, 0)      // clock skew must not yield "-3m ago"
  return {
    label: `updated ${formatAge(age)}`,
    stale: age > STALE_AFTER_MS,
    exact: new Date(iso).toLocaleString(),
  }
}

/** "just now" / "4m ago" / "2h ago" — coarse on purpose; precision is in the tooltip. */
function formatAge(ms: number): string {
  const secs = Math.floor(ms / 1000)
  if (secs < 45) return 'just now'
  const mins = Math.floor(secs / 60)
  if (mins < 60) return `${Math.max(mins, 1)}m ago`
  const hours = Math.floor(mins / 60)
  if (hours < 24) return `${hours}h ago`
  return `${Math.floor(hours / 24)}d ago`
}

/** One labelled pill in the range header. */
interface HeaderField {
  label: string
  value: string
  title?: string
  /** Force a wrap before this pill, starting a new row of the header. */
  newRow?: boolean
}

/** One range-identity pair as a segmented pill: tinted label fused to its value. */
function Field({ label, value, title }: HeaderField) {
  return (
    // Hover always reveals the untruncated pair, since the value half
    // ellipsizes when a single field is wider than the whole header.
    <span title={title ?? `${label}: ${value}`} style={{
      display: 'inline-flex', alignItems: 'stretch', flexShrink: 0,
      border: '1px solid var(--dn-border-lt)', borderRadius: 4,
      overflow: 'hidden', whiteSpace: 'nowrap',
      // A pill can't wrap or shrink, so one field longer than the header (a
      // 90-char Azure resource group is legal) would otherwise escape the
      // panel entirely. Bounded here, truncated in the value half below.
      maxWidth: '100%',
      // lineHeight 1 on both halves: a unitless value resolves against each
      // element's OWN font-size, so 10px and 12px text got line boxes of
      // different heights and the smaller label sat high in its stretched half.
      // Height now comes from padding, and each half centres its own text.
      lineHeight: 1,
    }}>
      <span style={{
        // Tinted half — reads as the key, not another value. Never truncates:
        // a pill with no readable label is worse than one with no value.
        display: 'flex', alignItems: 'center', flexShrink: 0,
        background: 'var(--dn-surface)', color: 'var(--dn-electric)',
        fontSize: 10, fontWeight: 700, letterSpacing: 0.6,
        textTransform: 'uppercase', padding: '4px 7px',
      }}>{label}</span>
      <span style={{
        // block, not flex: `text-overflow` needs a text container, and
        // minWidth 0 is what lets it shrink below its content width at all.
        display: 'block', color: 'var(--dn-text-bright)', fontSize: 12,
        padding: '4px 8px', minWidth: 0, overflow: 'hidden',
        textOverflow: 'ellipsis',
      }}>{value}</span>
    </span>
  )
}

// Exported for verification: pure, so it can be exercised without a DOM.
export function buildNodes(range: RangeDoc): Node[] {
  // Lay out in role tiers (ingress → DCs → members → the rest) so the topology
  // reads top-down as "access enters here, reaches these". A saved position
  // always wins, so dragging a node still sticks.
  const tiers = new Map<number, RangeHost[]>()
  for (const h of range.hosts) {
    const t = TIER[h.role] ?? 3
    if (!tiers.has(t)) tiers.set(t, [])
    tiers.get(t)!.push(h)
  }
  const widest = Math.max(...[...tiers.values()].map(v => v.length), 1)
  const nodes: Node[] = []
  for (const [tier, hosts] of [...tiers.entries()].sort((a, b) => a[0] - b[0])) {
    // Centre each tier against the widest one, so rows stay visually stacked.
    const offset = ((widest - hosts.length) * COL_W) / 2
    hosts.forEach((h, i) => {
      nodes.push({
        id: h.id,
        type: 'host',
        position: range.layout?.[h.id] ?? { x: offset + i * COL_W, y: tier * TIER_Y },
        data: h as unknown as Record<string, unknown>,
      })
    })
  }
  return nodes
}

export default function RangeView(
  { sessionId, session, refreshKey = 0 }:
    { sessionId: string | null; session?: Session; refreshKey?: number },
) {
  const [range, setRange] = useState<RangeDoc | null>(null)
  const [nodes, setNodes, onNodesChange] = useNodesState<Node>([])
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(() => {
    if (!sessionId) { setRange(null); return }
    api.getRange(sessionId)
      .then(r => { setRange(r); setNodes(buildNodes(r)); setError(null) })
      .catch(() => setError('range not found'))
  }, [sessionId, refreshKey, setNodes])

  useEffect(() => { load() }, [load])

  const handleChange = useCallback((changes: NodeChange<Node>[]) => {
    onNodesChange(changes)
  }, [onNodesChange])

  const persistLayout = useCallback(() => {
    if (!sessionId) return
    const layout: Record<string, { x: number; y: number }> = {}
    for (const n of nodes) layout[n.id] = { x: Math.round(n.position.x), y: Math.round(n.position.y) }
    api.saveLayout(sessionId, layout).catch(() => {})
  }, [sessionId, nodes])

  const header = useMemo(() => {
    if (!range) return ''
    // Count VMs only. Azure Bastion is a managed service with no power state —
    // it never appears in `lab status`, so it can never be "running" and its
    // presence in the denominator made a fully healthy range read as 6/7.
    const vms = range.hosts.filter(h => !isManagedService(h))
    const up = vms.filter(h => h.status === 'running').length
    return `${up}/${vms.length} running`
  }, [range])

  // A relative label freezes unless something re-renders it, so tick a clock.
  // Local only — no network, no re-fetch; the range itself still refreshes on
  // check_run alone.
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), TICK_MS)
    return () => clearInterval(id)
  }, [])

  const checked = useMemo(
    () => (range ? describeChecked(range.last_checked_at, now) : null),
    [range, now],
  )

  // Key range identity, from the session anchor + snapshot. Split across two
  // rows by where the values come from: the config anchor (env/provider/region,
  // known before anything is deployed) on the first, then the cloud placement
  // the ingestion hook learns post-deploy on the second. Both providers report
  // an account; only Azure has a resource group, absent on AWS.
  const fields = useMemo<HeaderField[]>(() => {
    if (!session) return []
    const snap = session.snapshot ?? {}
    // Provider-neutral: `account` is an AWS account ID or an Azure subscription,
    // `group` an Azure resource group (absent on AWS, which has no equivalent).
    const account = snap.account
    // Values are nullable here and filtered below, so the pill list only ever
    // holds fields that actually have something to show.
    const rows: Array<Omit<HeaderField, 'value'> & { value?: string | null }> = [
      { label: 'env', value: session.anchor?.env },
      { label: 'provider', value: snap.provider },
      { label: 'region', value: snap.region },
      { label: 'resource group', value: snap.group, newRow: true },
      // An Azure subscription GUID is 36 chars — far too wide for a header pill,
      // so show the leading segment with the whole value in the tooltip. An AWS
      // account ID is 12 digits and has no dashes, so it renders in full.
      {
        label: 'account',
        value: account ? `${account.split('-')[0]}${account.includes('-') ? '…' : ''}` : null,
        title: account ?? undefined,
        // Carries the break when there's no resource group to carry it (AWS),
        // so the placement row starts on its own line either way.
        newRow: !snap.group,
      },
    ]
    return rows
      .filter(r => !!r.value)
      .map(r => ({ label: r.label, value: r.value as string, title: r.title, newRow: r.newRow }))
  }, [session])

  if (!sessionId) {
    return <div style={emptyStyle}>No session selected</div>
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%', background: 'var(--dn-black)' }}>
      <div style={headerStyle}>
        {/* Wraps rather than clips: the pill row grows with the range (a long
            resource group, an Azure subscription), and a truncated account id
            is worse than a two-line header. `rowGap` keeps wrapped rows from
            touching; the status block opposite stays on the first row. */}
        <div style={{
          display: 'flex', alignItems: 'center', flexWrap: 'wrap',
          columnGap: 8, rowGap: 6, minWidth: 0,
        }}>
          {/* Extra right margin (on top of the flex gap) sets the label apart
              from the identity fields, which are spaced more tightly. */}
          <span style={{ color: 'var(--dn-electric)', fontSize: 13, fontWeight: 700, marginRight: 10 }}>RANGE</span>
          {fields.map(f => (
            <Fragment key={f.label}>
              {/* A full-width zero-height item is the only way to force a wrap
                  in a flex row — it fills the line, pushing what follows down. */}
              {f.newRow && <span style={{ flexBasis: '100%', height: 0 }} />}
              <Field label={f.label} value={f.value} title={f.title} />
            </Fragment>
          ))}
        </div>
        {/* minHeight = one pill (12px text + 8px padding + 2px border), so the
            status centres against the FIRST pill row, not the wrapped block. */}
        <div style={{
          display: 'flex', alignItems: 'center', gap: 12,
          flexShrink: 0, minHeight: 22,
        }}>
          {checked && (
            <span
              title={`Range state last refreshed: ${checked.exact}`}
              style={{
                fontSize: 11, whiteSpace: 'nowrap',
                // Amber once stale — the view updates only after a command, so
                // an old timestamp is the only sign a run is wedged or absent.
                color: checked.stale ? 'var(--dn-warning)' : 'var(--dn-text-muted)',
              }}
            >{checked.label}</span>
          )}
          <span style={{ color: 'var(--dg-node-label)', fontSize: 11 }}>{header}</span>
        </div>
      </div>
      <div style={{ flex: 1, position: 'relative' }}>
        {error ? (
          <div style={emptyStyle}>{error}</div>
        ) : (
          <ReactFlow
            nodes={nodes}
            edges={[]}
            nodeTypes={nodeTypes}
            onNodesChange={handleChange}
            onNodeDragStop={persistLayout}
            fitView
            proOptions={{ hideAttribution: true }}
          >
            <Background color="var(--dn-border)" gap={20} />
            <Controls position="bottom-right" />
          </ReactFlow>
        )}
      </div>
    </div>
  )
}

const headerStyle: React.CSSProperties = {
  // flex-start, not center: when the field pills wrap to a second row the
  // header grows downward and the status block stays put on the first row.
  display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between',
  gap: 16, padding: '12px 16px', borderBottom: '1px solid var(--dn-border)',
  background: 'var(--dn-black)', flexShrink: 0,
}
const emptyStyle: React.CSSProperties = {
  display: 'flex', alignItems: 'center', justifyContent: 'center',
  height: '100%', color: 'var(--dg-node-label)', fontSize: 13,
}

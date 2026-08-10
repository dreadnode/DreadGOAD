import {
  createContext, useCallback, useContext, useEffect, useMemo, useRef, useState,
} from 'react'
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
import type { RangeDoc, RangeHost, RangeLayout, Session } from '../types'
import { LatestLayoutSaver } from '../layoutSaveQueue'
import ConnectModal from './ConnectModal'
import Tooltip from './Tooltip'

// How a node asks for the connect modal. A context rather than a prop on the
// node's `data`: buildNodes is pure and exported for verification, and threading
// a callback through it would put a function in the data React Flow diffs.
//
// Exported for verification: with no provider the button does not render at all,
// so a size check that cannot supply one silently measures the button-less node
// and reports a bound that does not hold in the app.
export const ConnectRequest = createContext<((host: RangeHost) => void) | null>(null)

// Only the attack box gets a connect button. The Windows hosts run no SSH
// server, and the bastion is a managed service with nothing to log into.
const isConnectable = (h: RangeHost) => h.role === 'attackbox'

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

// The health vocabulary is closed: health_sync.py derives exactly these three
// per host, worst-wins across that host's checks. `unknown` is the seed value
// every host carries until /health has run, and the node hides it rather than
// stamping "unknown" across a range nobody has checked yet — so grey here is
// only a fallback for a value this map does not know.
const HEALTH_COLOR: Record<string, string> = {
  healthy: 'var(--dn-success)',
  unhealthy: 'var(--dn-warning)',
  unknown: 'var(--dg-node-label)',
}

// One FAIL among twenty checks marks the whole host unhealthy, so the word
// alone overstates: say what it aggregates, since the node has no room to.
const HEALTH_TITLE: Record<string, string> = {
  healthy: 'healthy — every /health check on this host passed',
  unhealthy: 'unhealthy — at least one /health check on this host failed; '
    + 'run /health for the per-check detail',
  unknown: 'unknown — /health has not run since this range was read',
}

// Vertical tiers: access enters at the top and reaches the lab below it.
const TIER: Record<string, number> = {
  bastion: 0, attackbox: 0,   // ingress
  dc: 1,                      // domain controllers
  member: 2, workstation: 2,  // domain members
  linux: 3, other: 3,         // extensions & everything else
}
const INGRESS_ROLES = new Set(['bastion', 'attackbox'])

// Node geometry, in border-box terms. The previous spacing was derived from
// HostNode's `maxWidth: 210`, which is the CONTENT box — the rendered node adds
// 12px padding and 1px border on each side, so it is up to 236px wide, and the
// old 220px column made neighbours overlap by 16px. Height grew past the old
// 165px tier the same way, once the detail block (os / vm / pub) landed.
//
// Measured in a browser against the tallest possible content: an INGRESS badge
// plus all three detail rows. Every variable-length line in HostNode is clamped
// to one line (hostname, role/domain, the health word, and the detail rows all
// ellipsize), which is what makes this a real bound rather than a sample — an
// unclamped hostname wrapped to two lines measured 180px and broke the
// guarantee.
//
// The height was 176 and did not hold. Re-measuring the REAL component against
// the REAL hosts found the attack box at 179 and a fully-populated one at 197,
// both over — the INGRESS badge and the detail block had been costed
// separately but never together on one node. Since the tier pitch derives from
// this number, the nodes that overlapped on load were the ingress row, every
// time. 200 covers the worst case measured: INGRESS + domain + status +
// health + private IP + all three detail rows + the connect button.
//
// Re-measure if HostNode gains a row, or if any line is allowed to wrap.
// Exported for verification: a layout test can assert no two nodes overlap
// without re-deriving these, and will fail if a future edit shrinks them.
export const NODE_MAX_W = 236
export const NODE_MAX_H = 200
// Guaranteed clear space between adjacent nodes on both axes.
export const NODE_GUTTER = 28
const COL_W = NODE_MAX_W + NODE_GUTTER
const TIER_Y = NODE_MAX_H + NODE_GUTTER

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
      {/* The key ("os", "vm", "pub") is bright and bold, not the dim tier it
          used to be: at 10px, --dn-text-muted measured 3.03:1 against the node
          surface, under the 4.5:1 floor and hard to read at this size.

          That inverts the usual hierarchy — at 17.4:1 the key is now brighter
          than its own value (8.39:1), a 2.07:1 separation the other way. It
          holds because the key is a fixed two or three characters from a set of
          three, so it is recognised by shape rather than read, while the value
          beside it is long and variable. Weight and length keep them distinct;
          brightness is no longer what separates them. */}
      <span style={{
        color: 'var(--dn-text-bright)', fontWeight: 700, flexShrink: 0,
      }}>{label}</span>
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

// Exported for verification: NODE_MAX_W/H are measured against this component,
// and that measurement is only trustworthy if it can render the real thing.
export function HostNode({ data }: NodeProps) {
  const h = data as unknown as RangeHost
  const ingress = INGRESS_ROLES.has(h.role)
  const service = isManagedService(h)
  const onConnect = useContext(ConnectRequest)
  // Shown only when it differs from the displayed name. On a variant they
  // always do (dc01 vs "solar"), but the attack box and bastion are named after
  // their role in every range, and "attackbox ATTACKBOX" is noise. Compared
  // case-insensitively because the store holds it lower-case while reports and
  // playbooks use upper.
  const moniker = (h.key ?? '').toUpperCase()
  const showMoniker = moniker !== '' && moniker !== (h.hostname ?? '').toUpperCase()
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
      {/* Hostname and role/domain are clamped to one line each. Both are
          variable-length, and left to wrap they made the node taller than
          NODE_MAX_H — which the tier spacing is derived from, so a long name
          reintroduced the overlap this layout exists to prevent. A name with no
          break opportunity (no dots or dashes) overflowed the box sideways
          instead. Ellipsis + tooltip keeps the full value reachable. */}
      <div style={{ display: 'flex', alignItems: 'baseline', gap: 8, minWidth: 0 }}>
        <span style={{ fontSize: 18, flexShrink: 0, alignSelf: 'center' }}>{ROLE_ICON[h.role] ?? ROLE_ICON.other}</span>
        <span
          title={h.hostname}
          style={{
            fontSize: 13, fontWeight: 700, color: 'var(--dn-text-bright)',
            minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis',
            whiteSpace: 'nowrap',
          }}
        >{h.hostname}</span>
        {showMoniker && (
          // The lab-definition name for this host. A variant renames every
          // machine, so the box says "solar" while every answer key, playbook
          // and /health report says DC01 — this is the only place the two are
          // shown together.
          //
          // flexShrink 0 with the hostname free to shrink: the moniker is
          // short and fixed, the hostname is long and variable, so the
          // ellipsis belongs on the hostname. gap 6 rather than the row's 8 —
          // these two name one machine and should read as a pair.
          <span
            title={`Lab definition name: ${moniker} (this range renames it to ${h.hostname})`}
            style={{
              fontSize: 10, fontWeight: 700, letterSpacing: 0.3,
              color: 'var(--dg-node-label)', flexShrink: 0, marginLeft: -2,
            }}
          >{moniker}</span>
        )}
      </div>
      <div
        title={h.domain ? `${h.role} · ${h.domain}` : h.role}
        style={{
          fontSize: 11, color: 'var(--dg-node-label)', marginTop: 4,
          overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap',
        }}
      >
        {h.role}{h.domain ? ` · ${h.domain}` : ''}
      </div>
      {/* The connect button shares the status row rather than taking one of its
          own: NODE_MAX_H is measured from the tallest node, and the tier spacing
          is derived from it, so a new row here would reintroduce the overlap the
          layout exists to prevent.

          nowrap is what makes that true. Left to wrap, "running" + "degraded" +
          the button overflow 210px of content and the status drops to a second
          line — measured 215px against a 176px bound. The health word gives way
          first (it ellipsizes) so the button and the power state always fit. */}
      <div style={{
        display: 'flex', gap: 8, marginTop: 6, fontSize: 11,
        alignItems: 'center', flexWrap: 'nowrap', minWidth: 0,
      }}>
        {service
          ? <span style={{ color, whiteSpace: 'nowrap' }} title="Azure Bastion is a managed service, not a VM — it never shows in lab status">managed service</span>
          : <span style={{ color, whiteSpace: 'nowrap', flexShrink: 0 }}>● {h.status}</span>}
        {!service && h.health !== 'unknown' && (
          <span
            title={HEALTH_TITLE[h.health] ?? h.health}
            style={{
              // Coloured, because this is the field that says something is
              // wrong and it read identically to the one saying all is well —
              // both grey, beside a status dot coloured for power state, so
              // the only failure signal on the node was a word you had to
              // stop and read. Amber not red: the range is still up, and a
              // failing check here is usually one broken service on an
              // otherwise reachable host (8.6:1 on the node surface).
              color: HEALTH_COLOR[h.health] ?? 'var(--dg-node-label)',
              minWidth: 0,
              overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap',
            }}
          >{h.health}</span>
        )}
        {onConnect && isConnectable(h) && (
          <button
            // nodrag: React Flow starts a drag on pointerdown anywhere in the
            // node, which would swallow the click.
            className="nodrag"
            onClick={() => onConnect(h)}
            title="Show the Bastion tunnel + ssh commands for this host"
            style={{
              // Border matches the label rather than using the generic border
              // token: --dn-border-lt is 1.21:1 against the node surface, so the
              // outline was invisible and the word read as loose text sitting in
              // the status row instead of a control you can press.
              marginLeft: 'auto', flexShrink: 0, padding: '1px 7px',
              borderRadius: 3, border: '1px solid var(--dg-interactive)',
              background: 'transparent', color: 'var(--dg-interactive)',
              cursor: 'pointer', whiteSpace: 'nowrap',
              fontFamily: 'var(--font-mono)', fontSize: 10, lineHeight: 1.6,
            }}
          >connect</button>
        )}
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
        <div style={{ marginTop: 6, paddingTop: 5, borderTop: '1px solid var(--dg-node-rule)' }}>
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

/** One labelled field in the range header. */
interface HeaderField {
  label: string
  value: string
  title?: string
}

/**
 * One range-identity pair, label stacked above its value.
 *
 * Replaces a segmented pill (tinted label fused beside the value). The pill
 * put the label to the LEFT, so a wide key like "RESOURCE GROUP" spent ~110px
 * before its value began — five fields could not fit one row at any realistic
 * panel width, and the header always cost two. Stacking reclaims that width for
 * ~4px of height, which is paid once rather than per row: measured across
 * 620/820/1000px panels the header goes 106/50/50px → 54/24/24px.
 *
 * The key/value distinction now comes from type and colour rather than a box:
 * 9px uppercase electric-blue label over a 12px bright value.
 */
function Field({ label, value, title }: HeaderField) {
  return (
    // Hover reveals the untruncated pair — the value ellipsizes when a field is
    // wider than the header (a 90-char Azure resource group is legal). Themed
    // rather than a native `title`: this is the only way to read the full
    // value, so it belongs to the interface rather than being a browser aside.
    // copy is the bare value, not the labelled line: these get pasted into a
    // terminal or an az command, where "group: " would have to be deleted.
    <Tooltip label={title ?? `${label}: ${value}`} copy={value}>
    <span style={{
      display: 'flex', flexDirection: 'column', flexShrink: 0,
      // Both lines share this, so the two-line block stays compact and the
      // label sits tight under nothing and directly over its value.
      lineHeight: 1.2, minWidth: 0, maxWidth: '100%',
    }}>
      <span style={{
        color: 'var(--dn-electric)', fontSize: 9, fontWeight: 700,
        letterSpacing: 0.7, textTransform: 'uppercase', whiteSpace: 'nowrap',
      }}>{label}</span>
      <span style={{
        color: 'var(--dn-text-bright)', fontSize: 12, whiteSpace: 'nowrap',
        // minWidth 0 is what lets the value shrink below its content width, so
        // an over-long one truncates instead of pushing the row wider.
        minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis',
        // Digits line up between stacked fields (account ids, CIDRs).
        fontVariantNumeric: 'tabular-nums',
      }}>{value}</span>
    </span>
    </Tooltip>
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
  const [connectHost, setConnectHost] = useState<RangeHost | null>(null)
  const loadRef = useRef<() => void>(() => {})
  const activeSessionRef = useRef(sessionId)
  activeSessionRef.current = sessionId

  const layoutSaver = useMemo(() => {
    if (!sessionId) return null
    const saverSessionId = sessionId
    return new LatestLayoutSaver(
      (layout, revision) => api.saveLayout(saverSessionId, layout, revision)
        .then(result => result.layout_revision),
      () => {
        // A 409 means another tab wrote a newer revision; other failures are
        // equally unsafe to assume succeeded. Reload the authoritative layout.
        if (activeSessionRef.current === saverSessionId) loadRef.current()
      },
    )
  }, [sessionId])

  const load = useCallback(() => {
    if (!sessionId) { setRange(null); return }
    api.getRange(sessionId)
      .then(r => {
        layoutSaver?.setRevision(r.layout_revision ?? 0)
        setRange(r)
        setNodes(buildNodes(r))
        setError(null)
      })
      .catch(() => setError('range not found'))
  }, [sessionId, refreshKey, setNodes, layoutSaver])
  loadRef.current = load

  useEffect(() => { load() }, [load])

  const handleChange = useCallback((changes: NodeChange<Node>[]) => {
    onNodesChange(changes)
  }, [onNodesChange])

  // Stable identity: it goes into a context every node consumes, so a new
  // function each render would re-render the whole topology.
  const requestConnect = useCallback((host: RangeHost) => setConnectHost(host), [])

  // A session switch must not leave the previous range's commands on screen.
  useEffect(() => { setConnectHost(null) }, [sessionId])

  const persistLayout = useCallback((draggedNode: Node) => {
    if (!layoutSaver) return
    const layout: RangeLayout = {}
    for (const n of nodes) {
      // React may not have committed the final drag update yet; the callback's
      // node is authoritative for the node that just stopped moving.
      const current = n.id === draggedNode.id ? draggedNode : n
      layout[current.id] = {
        x: Math.round(current.position.x),
        y: Math.round(current.position.y),
      }
    }
    layoutSaver.enqueue(layout)
  }, [layoutSaver, nodes])

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

  // Key range identity, from the session anchor + snapshot. Ordered by when the
  // value becomes known: the config anchor first (env/cloud, known before
  // anything is deployed), then the placement the ingestion hook learns
  // post-deploy. Both providers report an account; only Azure has a resource
  // group, absent on AWS. No forced row break — four fields fit one row at a
  // normal panel width, and wrapping handles the narrow case on its own.
  const fields = useMemo<HeaderField[]>(() => {
    if (!session) return []
    const snap = session.snapshot ?? {}
    // Provider-neutral: `account` is an AWS account ID or an Azure subscription,
    // `group` an Azure resource group (absent on AWS, which has no equivalent).
    const account = snap.account
    // Provider and region are never useful apart — "azure" alone doesn't locate
    // anything — so they share one field and save a whole column of width.
    const cloud = [snap.provider, snap.region].filter(Boolean).join('/')
    // Values are nullable here and filtered below, so the list only ever holds
    // fields that actually have something to show.
    const rows: Array<Omit<HeaderField, 'value'> & { value?: string | null }> = [
      { label: 'env', value: session.anchor?.env },
      { label: 'cloud', value: cloud || null },
      { label: 'resource group', value: snap.group },
      // An Azure subscription GUID is 36 chars — far too wide for a header
      // field, so show the leading segment with the whole value in the tooltip.
      // An AWS account ID is 12 digits and has no dashes, so it renders in full.
      {
        label: 'account',
        value: account ? `${account.split('-')[0]}${account.includes('-') ? '…' : ''}` : null,
        title: account ?? undefined,
      },
    ]
    return rows
      .filter(r => !!r.value)
      .map(r => ({ label: r.label, value: r.value as string, title: r.title }))
  }, [session])

  if (!sessionId) {
    return <div style={emptyStyle}>No session selected</div>
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%', background: 'var(--dn-black)' }}>
      <div style={headerStyle}>
        {/* Wraps rather than clips: a long resource group or an Azure
            subscription still gets a second row rather than being cut off.
            columnGap is wide (18) because the fields have no borders now —
            whitespace is what separates them, so it has to be doing more work
            than it did between boxes. */}
        <div style={{
          display: 'flex', alignItems: 'center', flexWrap: 'wrap',
          columnGap: 18, rowGap: 8, minWidth: 0,
        }}>
          {/* Set apart from the fields by more than they are from each other,
              so it reads as the row's title rather than as another field.
              marginRight adds to the 18px columnGap → 32px of clear space. */}
          <span style={{
            color: 'var(--dn-electric)', fontSize: 13, fontWeight: 700,
            marginRight: 14, alignSelf: 'center',
          }}>RANGE</span>
          {fields.map(f => (
            <Field key={f.label} label={f.label} value={f.value} title={f.title} />
          ))}
        </div>
        {/* minHeight = one stacked field (9px label + 12px value at lineHeight
            1.2 ≈ 26), so the status centres against the FIRST row rather than
            drifting to the middle of a wrapped block. */}
        <div style={{
          display: 'flex', alignItems: 'center', gap: 12,
          flexShrink: 0, minHeight: 26,
        }}>
          {checked && (
            <Tooltip label={`Range state last refreshed: ${checked.exact}`}>
            <span
              style={{
                fontSize: 11, whiteSpace: 'nowrap',
                // Amber once stale — the view updates only after a command, so
                // an old timestamp is the only sign a run is wedged or absent.
                color: checked.stale ? 'var(--dn-warning)' : 'var(--dn-text-muted)',
              }}
            >{checked.label}</span>
            </Tooltip>
          )}
          <span style={{ color: 'var(--dg-node-label)', fontSize: 11 }}>{header}</span>
        </div>
      </div>
      <div style={{ flex: 1, position: 'relative' }}>
        {error ? (
          <div style={emptyStyle}>{error}</div>
        ) : (
          <ConnectRequest.Provider value={requestConnect}>
            <ReactFlow
              nodes={nodes}
              edges={[]}
              nodeTypes={nodeTypes}
              onNodesChange={handleChange}
              onNodeDragStop={(_, node) => persistLayout(node)}
              fitView
              proOptions={{ hideAttribution: true }}
            >
              <Background color="var(--dn-border)" gap={20} />
              <Controls position="bottom-right" />
            </ReactFlow>
          </ConnectRequest.Provider>
        )}
      </div>
      {connectHost && (
        <ConnectModal
          session={session}
          host={connectHost}
          onClose={() => setConnectHost(null)}
        />
      )}
    </div>
  )
}

const headerStyle: React.CSSProperties = {
  // flex-start, not center: when the fields wrap to a second row the header
  // grows downward and the status block stays put on the first row.
  display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between',
  gap: 16, padding: '12px 16px', borderBottom: '1px solid var(--dn-border)',
  background: 'var(--dn-black)', flexShrink: 0,
  // Shared with the AGENT pane header so the two banners align across the
  // split — see --dg-pane-header-h in index.css. minHeight, not height: this
  // one still grows when the fields wrap.
  minHeight: 'var(--dg-pane-header-h)',
}
const emptyStyle: React.CSSProperties = {
  display: 'flex', alignItems: 'center', justifyContent: 'center',
  height: '100%', color: 'var(--dg-node-label)', fontSize: 13,
}

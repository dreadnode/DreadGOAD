import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  Background,
  Controls,
  ReactFlow,
  useNodesState,
  type Node,
} from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import { api } from '../api'
import { LatestLayoutSaver } from '../layoutSaveQueue'
import {
  buildNodes,
  describeChecked,
  isManagedService,
} from '../rangePresentation'
import type { RangeDoc, RangeHost, RangeLayout, Session } from '../types'
import ConnectModal from './ConnectModal'
import HostDetailPanel from './HostDetailPanel'
import HostNode, { ConnectRequest, DetailRequest } from './RangeHostNode'
import RangeTable from './RangeTable'
import Tooltip from './Tooltip'

// Preserve the named API used by layout verification and potential consumers.
export { ConnectRequest, DetailRequest, default as HostNode } from './RangeHostNode'
export {
  NODE_GUTTER,
  NODE_MAX_H,
  NODE_MAX_W,
  buildNodes,
  describeChecked,
} from '../rangePresentation'
export type { CheckedLabel } from '../rangePresentation'

const nodeTypes = { host: HostNode }
const TICK_MS = 30_000

type ViewMode = 'graph' | 'table'

interface HeaderField {
  label: string
  value: string
  title?: string
}

function Field({ label, value, title }: HeaderField) {
  return (
    <Tooltip label={title ?? `${label}: ${value}`} copy={value}>
      <span style={{
        display: 'flex', flexDirection: 'column', flexShrink: 0,
        lineHeight: 1.2, minWidth: 0, maxWidth: '100%',
      }}>
        <span style={{
          color: 'var(--dn-electric)', fontSize: 9, fontWeight: 700,
          letterSpacing: 0.7, textTransform: 'uppercase', whiteSpace: 'nowrap',
        }}>{label}</span>
        <span style={{
          color: 'var(--dn-text-bright)', fontSize: 12, whiteSpace: 'nowrap',
          minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis',
          fontVariantNumeric: 'tabular-nums',
        }}>{value}</span>
      </span>
    </Tooltip>
  )
}

/** Stateful coordinator for the graph/table range views and their modals. */
export default function RangeView(
  { sessionId, session, refreshKey = 0 }:
    { sessionId: string | null; session?: Session; refreshKey?: number },
) {
  const [range, setRange] = useState<RangeDoc | null>(null)
  const [nodes, setNodes, onNodesChange] = useNodesState<Node>([])
  const [error, setError] = useState<string | null>(null)
  const [connectHost, setConnectHost] = useState<RangeHost | null>(null)
  const [detailNode, setDetailNode] = useState<string | null>(null)
  const [viewMode, setViewMode] = useState<ViewMode>('graph')
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
        // A failed write leaves the local revision unsafe. Reload the server's layout.
        if (activeSessionRef.current === saverSessionId) loadRef.current()
      },
    )
  }, [sessionId])

  const load = useCallback(() => {
    if (!sessionId) {
      setRange(null)
      return
    }
    api.getRange(sessionId)
      .then(nextRange => {
        layoutSaver?.setRevision(nextRange.layout_revision ?? 0)
        setRange(nextRange)
        setNodes(buildNodes(nextRange))
        setError(null)
      })
      .catch(() => setError('range not found'))
  }, [sessionId, refreshKey, setNodes, layoutSaver])
  loadRef.current = load

  useEffect(() => {
    if (!sessionId) {
      setRange(null)
      return
    }
    let cancelled = false
    api.getRange(sessionId)
      .then(nextRange => {
        if (cancelled) return
        layoutSaver?.setRevision(nextRange.layout_revision ?? 0)
        setRange(nextRange)
        setNodes(buildNodes(nextRange))
        setError(null)
      })
      .catch(() => {
        if (!cancelled) setError('range not found')
      })
    return () => { cancelled = true }
  }, [sessionId, refreshKey, setNodes, layoutSaver])

  const requestConnect = useCallback((host: RangeHost) => setConnectHost(host), [])

  // Node ids repeat across ranges, so dismiss state tied to the previous session.
  useEffect(() => {
    setConnectHost(null)
    setDetailNode(null)
  }, [sessionId])

  const persistLayout = useCallback((draggedNode: Node) => {
    if (!layoutSaver) return
    const layout: RangeLayout = {}
    for (const node of nodes) {
      // The drag callback is authoritative if React has not committed its final update.
      const current = node.id === draggedNode.id ? draggedNode : node
      layout[current.id] = {
        x: Math.round(current.position.x),
        y: Math.round(current.position.y),
      }
    }
    layoutSaver.enqueue(layout)
  }, [layoutSaver, nodes])

  const header = useMemo(() => {
    if (!range) return ''
    const virtualMachines = range.hosts.filter(host => !isManagedService(host))
    const running = virtualMachines.filter(host => host.status === 'running').length
    return `${running}/${virtualMachines.length} running`
  }, [range])

  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), TICK_MS)
    return () => clearInterval(id)
  }, [])

  const checked = useMemo(
    () => (range ? describeChecked(range.last_checked_at, now) : null),
    [range, now],
  )

  const fields = useMemo<HeaderField[]>(() => {
    if (!session) return []
    const snapshot = session.snapshot ?? {}
    const account = snapshot.account
    const cloud = [snapshot.provider, snapshot.region].filter(Boolean).join('/')
    const rows: Array<Omit<HeaderField, 'value'> & { value?: string | null }> = [
      { label: 'env', value: session.anchor?.env },
      { label: 'cloud', value: cloud || null },
      { label: 'profile', value: snapshot.aws?.profile ?? null },
      { label: 'resource group', value: snapshot.group },
      {
        label: 'account',
        value: account ? `${account.split('-')[0]}${account.includes('-') ? '…' : ''}` : null,
        title: account ?? undefined,
      },
    ]
    return rows
      .filter(row => !!row.value)
      .map(row => ({ label: row.label, value: row.value as string, title: row.title }))
  }, [session])

  if (!sessionId) {
    return <div style={emptyStyle}>No session selected</div>
  }

  return (
    <div style={{
      display: 'flex', flexDirection: 'column', height: '100%',
      background: 'var(--dn-black)',
    }}>
      <div style={headerStyle}>
        <div style={{
          display: 'flex', alignItems: 'center', flexWrap: 'wrap',
          columnGap: 18, rowGap: 8, minWidth: 0,
        }}>
          <span style={{
            color: 'var(--dn-electric)', fontSize: 13, fontWeight: 700,
            marginRight: 14, alignSelf: 'center',
          }}>RANGE</span>
          {fields.map(field => (
            <Field
              key={field.label}
              label={field.label}
              value={field.value}
              title={field.title}
            />
          ))}
        </div>
        <div style={{
          display: 'flex', alignItems: 'center', gap: 12,
          flexShrink: 0, minHeight: 26,
        }}>
          {checked && (
            <Tooltip label={`Range state last refreshed: ${checked.exact}`}>
              <span style={{
                fontSize: 11, whiteSpace: 'nowrap',
                color: checked.stale ? 'var(--dn-warning)' : 'var(--dn-text-muted)',
              }}>{checked.label}</span>
            </Tooltip>
          )}
          <span style={{ color: 'var(--dg-node-label)', fontSize: 11 }}>{header}</span>
          <button
            onClick={() => setViewMode(current => current === 'graph' ? 'table' : 'graph')}
            title={viewMode === 'graph' ? 'Switch to table view' : 'Switch to graph view'}
            style={{
              padding: '2px 8px', borderRadius: 3,
              border: '1px solid var(--dn-electric)', background: 'transparent',
              color: 'var(--dn-electric)', cursor: 'pointer',
              fontFamily: 'var(--font-mono)', fontSize: 10, whiteSpace: 'nowrap',
            }}
          >{viewMode === 'graph' ? '☰ TABLE' : '⬡ GRAPH'}</button>
        </div>
      </div>

      {error ? (
        <div style={{ ...emptyStyle, flex: 1 }}>{error}</div>
      ) : viewMode === 'table' && range ? (
        <RangeTable range={range} sessionId={sessionId} session={session} />
      ) : (
        <div style={{ flex: 1, position: 'relative' }}>
          <ConnectRequest.Provider value={requestConnect}>
            <DetailRequest.Provider value={setDetailNode}>
              <ReactFlow
                nodes={nodes}
                edges={[]}
                nodeTypes={nodeTypes}
                onNodesChange={onNodesChange}
                onNodeDragStop={(_, node) => persistLayout(node)}
                onNodeClick={(_, node) => setDetailNode(node.id)}
                fitView
                proOptions={{ hideAttribution: true }}
              >
                <Background color="var(--dn-border)" gap={20} />
                <Controls position="bottom-right" />
              </ReactFlow>
            </DetailRequest.Provider>
          </ConnectRequest.Provider>
        </div>
      )}

      {detailNode && viewMode === 'graph' && (
        <HostDetailPanel
          sessionId={sessionId}
          nodeId={detailNode}
          onClose={() => setDetailNode(null)}
        />
      )}
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
  display: 'flex',
  alignItems: 'flex-start',
  justifyContent: 'space-between',
  gap: 16,
  padding: '12px 16px',
  borderBottom: '1px solid var(--dn-border)',
  background: 'var(--dn-black)',
  flexShrink: 0,
  minHeight: 'var(--dg-pane-header-h)',
}

const emptyStyle: React.CSSProperties = {
  display: 'flex',
  alignItems: 'center',
  justifyContent: 'center',
  height: '100%',
  color: 'var(--dg-node-label)',
  fontSize: 13,
}

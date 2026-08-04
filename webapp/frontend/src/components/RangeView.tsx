import { useCallback, useEffect, useMemo, useState } from 'react'
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
import type { RangeDoc, RangeHost } from '../types'

const ROLE_ICON: Record<string, string> = {
  dc: '🏰', member: '🖥️', workstation: '💻', bastion: '🛡️',
  attackbox: '☠️', linux: '🐧', other: '❔',
}
const STATUS_COLOR: Record<string, string> = {
  running: 'var(--dn-success)', stopped: 'var(--dn-text-muted)',
  provisioning: 'var(--dn-warning)', absent: 'var(--dn-error)',
  unknown: 'var(--dn-text-dim)',
}

function HostNode({ data }: NodeProps) {
  const h = data as unknown as RangeHost
  const color = STATUS_COLOR[h.status] ?? STATUS_COLOR.unknown
  return (
    <div style={{
      background: 'var(--dn-surface)', border: `1px solid ${color}`,
      borderRadius: 6, padding: '8px 12px', minWidth: 140,
      fontFamily: 'var(--font-mono)', color: 'var(--dn-text)',
    }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
        <span style={{ fontSize: 18 }}>{ROLE_ICON[h.role] ?? ROLE_ICON.other}</span>
        <span style={{ fontSize: 13, fontWeight: 700, color: 'var(--dn-text-bright)' }}>{h.hostname}</span>
      </div>
      <div style={{ fontSize: 10, color: 'var(--dn-text-muted)', marginTop: 4 }}>
        {h.role}{h.domain ? ` · ${h.domain}` : ''}
      </div>
      <div style={{ display: 'flex', gap: 8, marginTop: 6, fontSize: 10 }}>
        <span style={{ color }}>● {h.status}</span>
        {h.health !== 'unknown' && <span style={{ color: 'var(--dn-text-muted)' }}>{h.health}</span>}
      </div>
      {h.ip_private && (
        <div style={{ fontSize: 10, color: 'var(--dn-text-dim)', marginTop: 2 }}>{h.ip_private}</div>
      )}
    </div>
  )
}

const nodeTypes = { host: HostNode }

function buildNodes(range: RangeDoc): Node[] {
  return range.hosts.map((h, i) => {
    const saved = range.layout?.[h.id]
    return {
      id: h.id,
      type: 'host',
      position: saved ?? { x: (i % 3) * 220, y: Math.floor(i / 3) * 140 },
      data: h as unknown as Record<string, unknown>,
    }
  })
}

export default function RangeView(
  { sessionId, refreshKey = 0 }: { sessionId: string | null; refreshKey?: number },
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
    const up = range.hosts.filter(h => h.status === 'running').length
    return `${up}/${range.hosts.length} running`
  }, [range])

  if (!sessionId) {
    return <div style={emptyStyle}>No session selected</div>
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%', background: 'var(--dn-black)' }}>
      <div style={headerStyle}>
        <span style={{ color: 'var(--dn-text-muted)', fontSize: 13 }}>RANGE</span>
        <span style={{ color: 'var(--dg-brand)', fontSize: 11 }}>{header}</span>
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
            <Controls />
          </ReactFlow>
        )}
      </div>
    </div>
  )
}

const headerStyle: React.CSSProperties = {
  display: 'flex', alignItems: 'center', justifyContent: 'space-between',
  padding: '12px 16px', borderBottom: '1px solid var(--dn-border)',
  background: 'var(--dn-black)', flexShrink: 0,
}
const emptyStyle: React.CSSProperties = {
  display: 'flex', alignItems: 'center', justifyContent: 'center',
  height: '100%', color: 'var(--dn-text-dim)', fontSize: 13,
}

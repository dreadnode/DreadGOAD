import { createContext, useContext } from 'react'
import type { NodeProps } from '@xyflow/react'
import type { RangeHost } from '../types'
import {
  HEALTH_COLOR,
  HEALTH_TITLE,
  ROLE_ICON,
  STATUS_COLOR,
  isManagedService,
} from '../rangePresentation'

// Keep interaction callbacks out of React Flow's otherwise data-only node payload.
export const ConnectRequest = createContext<((host: RangeHost) => void) | null>(null)
export const DetailRequest = createContext<((nodeId: string) => void) | null>(null)

const INGRESS_ROLES = new Set(['bastion', 'attackbox'])
const INGRESS_BADGE_ROLES = new Set(['bastion'])
const ROLE_OS: Record<string, string> = {
  dc: 'Windows Server',
  member: 'Windows Server',
  workstation: 'Windows',
  attackbox: 'Kali Linux',
  linux: 'Linux',
}

const isConnectable = (host: RangeHost) => host.role === 'attackbox'

function NodeRow({ label, value, title }: { label: string; value: string; title?: string }) {
  return (
    <div style={{ display: 'flex', gap: 6, fontSize: 10, marginTop: 2 }}>
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

/** One host card rendered by React Flow. */
export default function HostNode({ data }: NodeProps) {
  const host = data as unknown as RangeHost
  const ingress = INGRESS_ROLES.has(host.role)
  const service = isManagedService(host)
  const onConnect = useContext(ConnectRequest)
  const onDetail = useContext(DetailRequest)
  const moniker = (host.key ?? '').toUpperCase()
  const showMoniker = moniker !== '' && moniker !== (host.hostname ?? '').toUpperCase()
  const color = service
    ? 'var(--dn-electric)'
    : (STATUS_COLOR[host.status] ?? STATUS_COLOR.unknown)

  return (
    <div style={{
      background: 'var(--dn-surface)',
      border: `1px solid ${color}`,
      boxShadow: ingress ? `0 0 0 1px ${color}55` : 'none',
      borderRadius: 6,
      padding: '8px 12px',
      minWidth: 140,
      maxWidth: 210,
      fontFamily: 'var(--font-mono)',
      color: 'var(--dn-text)',
    }}>
      {INGRESS_BADGE_ROLES.has(host.role) && (
        <div style={{
          fontSize: 9, letterSpacing: 0.5, color, marginBottom: 4, fontWeight: 700,
        }}>◆ INGRESS</div>
      )}

      <div style={{ display: 'flex', alignItems: 'baseline', gap: 8, minWidth: 0 }}>
        <span style={{ fontSize: 18, flexShrink: 0, alignSelf: 'center' }}>
          {ROLE_ICON[host.role] ?? ROLE_ICON.other}
        </span>
        <span
          title={host.hostname}
          style={{
            fontSize: 13, fontWeight: 700, color: 'var(--dn-text-bright)',
            minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis',
            whiteSpace: 'nowrap',
          }}
        >{host.hostname}</span>
        {showMoniker && (
          <span
            title={`Lab definition name: ${moniker} (this range renames it to ${host.hostname})`}
            style={{
              fontSize: 10, fontWeight: 700, letterSpacing: 0.3,
              color: 'var(--dg-node-label)', flexShrink: 0, marginLeft: -2,
            }}
          >{moniker}</span>
        )}
      </div>

      <div
        title={host.domain ? `${host.role} · ${host.domain}` : host.role}
        style={{
          fontSize: 11, color: 'var(--dg-node-label)', marginTop: 4,
          overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap',
        }}
      >
        {host.role}{host.domain ? ` · ${host.domain}` : ''}
      </div>

      <div style={{
        display: 'flex', gap: 8, marginTop: 6, fontSize: 11,
        alignItems: 'center', flexWrap: 'nowrap', minWidth: 0,
      }}>
        {service
          ? (
            <span
              style={{ color, whiteSpace: 'nowrap' }}
              title="Azure Bastion is a managed service, not a VM — it never shows in lab status"
            >managed service</span>
          )
          : <span style={{ color, whiteSpace: 'nowrap', flexShrink: 0 }}>● {host.status}</span>}
        {!service && host.health !== 'unknown' && (
          <span
            title={HEALTH_TITLE[host.health] ?? host.health}
            style={{
              color: HEALTH_COLOR[host.health] ?? 'var(--dg-node-label)',
              minWidth: 0,
              overflow: 'hidden',
              textOverflow: 'ellipsis',
              whiteSpace: 'nowrap',
            }}
          >{host.health}</span>
        )}
        {onDetail && !service && (
          <button
            className="nodrag"
            onClick={event => { event.stopPropagation(); onDetail(host.id) }}
            title="Disks, NICs, and attached resources for this host"
            style={{
              marginLeft: 'auto', flexShrink: 0, padding: '1px 7px',
              borderRadius: 3, border: '1px solid var(--dg-node-label)',
              background: 'transparent', color: 'var(--dg-node-label)',
              cursor: 'pointer', whiteSpace: 'nowrap',
              fontFamily: 'var(--font-mono)', fontSize: 10, lineHeight: 1.6,
            }}
          >details</button>
        )}
        {onConnect && isConnectable(host) && (
          <button
            className="nodrag"
            onClick={event => { event.stopPropagation(); onConnect(host) }}
            title="Show the Bastion tunnel + ssh commands for this host"
            style={{
              marginLeft: onDetail ? undefined : 'auto', flexShrink: 0,
              padding: '1px 7px',
              borderRadius: 3, border: '1px solid var(--dg-interactive)',
              background: 'transparent', color: 'var(--dg-interactive)',
              cursor: 'pointer', whiteSpace: 'nowrap',
              fontFamily: 'var(--font-mono)', fontSize: 10, lineHeight: 1.6,
            }}
          >connect</button>
        )}
      </div>

      {host.ip_private && (
        <div style={{
          fontSize: 11, color: 'var(--dg-node-value)', marginTop: 3,
          fontVariantNumeric: 'tabular-nums', letterSpacing: 0.2,
        }}>{host.ip_private}</div>
      )}

      {!service && (ROLE_OS[host.role] || host.cloud_name || host.ip_public) && (
        <div style={{ marginTop: 6, paddingTop: 5, borderTop: '1px solid var(--dg-node-rule)' }}>
          {ROLE_OS[host.role] && (
            <NodeRow
              label="os"
              value={ROLE_OS[host.role]}
              title="Derived from the host's role in the lab definition, not read from the VM"
            />
          )}
          {host.cloud_name && <NodeRow label="vm" value={host.cloud_name} />}
          {host.ip_public && <NodeRow label="pub" value={host.ip_public} />}
        </div>
      )}
    </div>
  )
}

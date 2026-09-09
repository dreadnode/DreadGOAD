import { Fragment, useMemo, useState } from 'react'
import { api } from '../api'
import type { HostDetail } from '../api'
import { buildConnectPlan, type ConnectPlan } from '../connect'
import {
  HEALTH_COLOR,
  HEALTH_TITLE,
  ROLE_ICON,
  STATUS_COLOR,
  isManagedService,
} from '../rangePresentation'
import type { RangeDoc, RangeHost, Session } from '../types'
import CopyableCommand from './CopyableCommand'
import VMResourceDetails from './VMResourceDetails'

interface RangeTableProps {
  range: RangeDoc
  sessionId: string
  session?: Session
}

export default function RangeTable({ range, sessionId, session }: RangeTableProps) {
  const [expanded, setExpanded] = useState<Set<string>>(new Set())
  const [details, setDetails] = useState<Record<string, HostDetail | null>>({})
  const [errors, setErrors] = useState<Record<string, string>>({})

  const toggle = (id: string) => {
    setExpanded(previous => {
      const next = new Set(previous)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
    if (details[id] === undefined && !errors[id]) {
      api.hostDetail(sessionId, id)
        .then(detail => setDetails(previous => ({ ...previous, [id]: detail })))
        .catch(error => {
          setErrors(previous => ({ ...previous, [id]: error?.message || 'unavailable' }))
        })
    }
  }

  const sorted = useMemo(() => {
    const order = ['bastion', 'attackbox', 'dc', 'member', 'workstation', 'linux', 'other']
    return [...range.hosts].sort((a, b) => {
      const aIndex = order.indexOf(a.role)
      const bIndex = order.indexOf(b.role)
      return (aIndex === -1 ? 99 : aIndex) - (bIndex === -1 ? 99 : bIndex)
    })
  }, [range.hosts])

  return (
    <div style={{ flex: 1, overflow: 'auto', background: 'var(--dn-black)' }}>
      <table style={{
        width: '100%', borderCollapse: 'collapse',
        fontFamily: 'var(--font-mono)', fontSize: 12,
      }}>
        <thead>
          <tr style={{
            borderBottom: '1px solid var(--dn-border-lt)',
            position: 'sticky', top: 0, background: 'var(--dn-black)', zIndex: 1,
          }}>
            {['', 'Hostname', 'VM Name', 'Role', 'Status', 'Health', 'Domain', 'Private IP', 'Public IP'].map(column => (
              <th key={column} style={{
                textAlign: 'left', padding: '8px 10px', fontSize: 10,
                letterSpacing: '0.06em', textTransform: 'uppercase',
                color: 'var(--dn-electric)', fontWeight: 700, whiteSpace: 'nowrap',
              }}>{column}</th>
            ))}
          </tr>
        </thead>
        <tbody>
          {sorted.map(host => {
            const service = isManagedService(host)
            const color = service
              ? 'var(--dn-electric)'
              : (STATUS_COLOR[host.status] ?? STATUS_COLOR.unknown)
            const healthColor = HEALTH_COLOR[host.health] ?? 'var(--dg-node-label)'
            const isOpen = expanded.has(host.id)
            const detail = details[host.id]
            const detailError = errors[host.id]
            return (
              <Fragment key={host.id}>
                <tr
                  onClick={() => toggle(host.id)}
                  style={{
                    borderBottom: isOpen ? 'none' : '1px solid var(--dn-border)',
                    cursor: 'pointer',
                    background: isOpen ? 'var(--dn-surface)' : 'transparent',
                  }}
                  onMouseEnter={event => {
                    if (!isOpen) event.currentTarget.style.background = 'var(--dn-surface-alt)'
                  }}
                  onMouseLeave={event => {
                    if (!isOpen) event.currentTarget.style.background = 'transparent'
                  }}
                >
                  <td style={cellStyle}>{ROLE_ICON[host.role] ?? ROLE_ICON.other}</td>
                  <td style={{ ...cellStyle, color: 'var(--dn-text-bright)', fontWeight: 600 }}>
                    {host.hostname}
                  </td>
                  <td
                    style={{
                      ...cellStyle,
                      color: 'var(--dg-node-value)',
                      maxWidth: 200,
                      overflow: 'hidden',
                      textOverflow: 'ellipsis',
                      whiteSpace: 'nowrap',
                    }}
                    title={host.cloud_name ?? undefined}
                  >{host.cloud_name ?? ''}</td>
                  <td style={cellStyle}>{host.role}</td>
                  <td style={cellStyle}>
                    <span style={{ color, whiteSpace: 'nowrap' }}>
                      {service ? 'managed service' : `● ${host.status}`}
                    </span>
                  </td>
                  <td style={cellStyle}>
                    {host.health !== 'unknown' && (
                      <span style={{ color: healthColor }} title={HEALTH_TITLE[host.health] ?? host.health}>
                        {host.health}
                      </span>
                    )}
                  </td>
                  <td style={{ ...cellStyle, color: 'var(--dg-node-value)' }}>{host.domain ?? ''}</td>
                  <td style={{ ...cellStyle, fontVariantNumeric: 'tabular-nums' }}>{host.ip_private ?? ''}</td>
                  <td style={{ ...cellStyle, fontVariantNumeric: 'tabular-nums' }}>{host.ip_public ?? ''}</td>
                </tr>
                {isOpen && (
                  <tr style={{ background: 'var(--dn-surface)', borderBottom: '1px solid var(--dn-border)' }}>
                    <td colSpan={9} style={{ padding: '10px 10px 14px 42px' }}>
                      <AccordionDetail
                        detail={detail}
                        error={detailError}
                        host={host}
                        session={session}
                      />
                    </td>
                  </tr>
                )}
              </Fragment>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}

function AccordionDetail({
  detail,
  error,
  host,
  session,
}: {
  detail: HostDetail | null | undefined
  error?: string
  host: RangeHost
  session?: Session
}) {
  if (error) {
    return <span style={{ color: 'var(--dg-node-label)', fontSize: 11 }}>{error}</span>
  }
  if (detail === undefined || detail === null) {
    return (
      <span style={{
        display: 'flex', alignItems: 'center', gap: 8,
        color: 'var(--dg-node-label)', fontSize: 11,
      }}>
        <span className="dg-spinner" /> Fetching details…
      </span>
    )
  }

  const plan = host.role === 'attackbox' ? buildConnectPlan(session, host) : null
  if (detail.kind === 'bastion') {
    const deployed = !!detail.cloud_id
    return (
      <div style={{ display: 'flex', flexDirection: 'column', gap: 4, fontSize: 11 }}>
        <DetailRow label="Type" value="Azure Bastion (managed service)" />
        <DetailRow label="Status" value={deployed ? (detail.status || 'deployed') : 'not deployed'} />
        {detail.resource_group && <DetailRow label="Resource Group" value={detail.resource_group} />}
        {detail.ip_public && <DetailRow label="Public IP" value={detail.ip_public} />}
        {detail.cloud_id && <DetailRow label="Resource ID" value={detail.cloud_id} />}
      </div>
    )
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 8, fontSize: 11 }}>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
        {detail.vm_size && <DetailRow label="VM Size" value={detail.vm_size} />}
        {detail.location && <DetailRow label="Region" value={detail.location} />}
        {detail.resource_group && <DetailRow label="Resource Group" value={detail.resource_group} />}
        {detail.power_state && <DetailRow label="Power State" value={detail.power_state} />}
      </div>
      <VMResourceDetails detail={detail} variant="accordion" />
      {plan && <ConnectCommands plan={plan} />}
    </div>
  )
}

function ConnectCommands({ plan }: { plan: ConnectPlan }) {
  if (plan.kind === 'azure-bastion') {
    return (
      <div style={{ marginTop: 8, display: 'flex', flexDirection: 'column', gap: 6 }}>
        <CopyableCommand compact label="Bastion Tunnel" value={plan.tunnel} />
        <CopyableCommand compact label="SSH" value={plan.ssh} />
      </div>
    )
  }
  if (plan.kind === 'aws-ssm') {
    return (
      <div style={{ marginTop: 8 }}>
        <CopyableCommand compact label="SSM Session" value={plan.session} />
      </div>
    )
  }
  return null
}

function DetailRow({ label, value }: { label: string; value: string }) {
  return (
    <div style={{ display: 'flex', gap: 10, minWidth: 0 }}>
      <span style={{
        flexShrink: 0, width: 110, color: 'var(--dg-node-label)',
        fontSize: 10, letterSpacing: '0.05em', textTransform: 'uppercase',
      }}>{label}</span>
      <span style={{
        color: 'var(--dn-text)', minWidth: 0, overflowWrap: 'anywhere',
      }}>{value}</span>
    </div>
  )
}

const cellStyle: React.CSSProperties = {
  padding: '7px 10px',
  whiteSpace: 'nowrap',
  color: 'var(--dn-text)',
}

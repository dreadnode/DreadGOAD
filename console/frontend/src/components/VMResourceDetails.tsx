import type { ReactNode } from 'react'
import type { HostDetail } from '../api'
import { shortResourceId } from '../format'

interface VMResourceDetailsProps {
  detail: Pick<HostDetail, 'disks' | 'nics'>
  variant: 'modal' | 'accordion'
}

/** Shared disk and network-interface details for the modal and table accordion. */
export default function VMResourceDetails({
  detail,
  variant,
}: VMResourceDetailsProps) {
  const disks = detail.disks || []
  const nics = detail.nics || []
  const compact = variant === 'accordion'
  const showEmptySections = variant === 'modal'

  return (
    <>
      {(showEmptySections || disks.length > 0) && (
        <ResourceSection title={`Disks (${disks.length})`} compact={compact} empty={disks.length === 0}>
          {disks.map(disk => (
            <ResourceCard
              key={`${disk.role}-${disk.name}-${disk.lun ?? 'os'}`}
              title={disk.name}
              tag={disk.role}
              compact={compact}
            >
              {disk.size_gb != null && <ResourceRow label="Size" value={`${disk.size_gb} GiB`} compact={compact} />}
              {disk.storage_type && <ResourceRow label="Type" value={disk.storage_type} compact={compact} />}
              {disk.caching && <ResourceRow label="Caching" value={disk.caching} compact={compact} />}
              {disk.lun != null && <ResourceRow label="LUN" value={String(disk.lun)} compact={compact} />}
            </ResourceCard>
          ))}
        </ResourceSection>
      )}

      {(showEmptySections || nics.length > 0) && (
        <ResourceSection
          title={`Network interfaces (${nics.length})`}
          compact={compact}
          empty={nics.length === 0}
        >
          {nics.map(nic => (
            <ResourceCard
              key={nic.id}
              title={nic.name}
              tag={nic.primary ? 'primary' : undefined}
              compact={compact}
            >
              {(nic.private_ips?.length ?? 0) > 0 && (
                <ResourceRow label="Private IP" value={nic.private_ips.join(', ')} compact={compact} />
              )}
              {nic.mac_address && <ResourceRow label="MAC" value={nic.mac_address} compact={compact} />}
              {nic.subnet_id && (
                <ResourceRow
                  label="Subnet"
                  value={shortResourceId(nic.subnet_id)}
                  full={compact ? undefined : nic.subnet_id}
                  compact={compact}
                />
              )}
              {nic.nsg_id && (
                <ResourceRow
                  label="NSG"
                  value={shortResourceId(nic.nsg_id)}
                  full={compact ? undefined : nic.nsg_id}
                  compact={compact}
                />
              )}
              {nic.accelerated_networking && (
                <ResourceRow label="Accel net" value="enabled" compact={compact} />
              )}
              {nic.public_ip_id && (
                <ResourceRow
                  label="Public IP"
                  value={shortResourceId(nic.public_ip_id)}
                  full={compact ? undefined : nic.public_ip_id}
                  compact={compact}
                />
              )}
            </ResourceCard>
          ))}
        </ResourceSection>
      )}
    </>
  )
}

function ResourceRow({
  label,
  value,
  full,
  compact,
}: {
  label: string
  value: string
  full?: string
  compact: boolean
}) {
  return (
    <div style={{ display: 'flex', gap: 10, alignItems: compact ? undefined : 'baseline', minWidth: 0 }}>
      <span style={{
        flexShrink: 0,
        width: compact ? 110 : 96,
        fontSize: 10,
        letterSpacing: compact ? '0.05em' : '0.06em',
        textTransform: 'uppercase',
        color: 'var(--dg-node-label)',
      }}>{label}</span>
      <span
        title={full}
        style={{
          color: compact ? 'var(--dn-text)' : undefined,
          minWidth: 0,
          overflowWrap: 'anywhere',
          fontFamily: compact ? undefined : 'var(--font-mono)',
        }}
      >{value}</span>
    </div>
  )
}

function ResourceSection({
  title,
  children,
  compact,
  empty,
}: {
  title: string
  children: ReactNode
  compact: boolean
  empty: boolean
}) {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: compact ? 6 : 7 }}>
      <span style={{
        fontSize: 10,
        letterSpacing: '0.08em',
        textTransform: 'uppercase',
        color: 'var(--dg-interactive)',
      }}>{title}</span>
      {empty
        ? <span style={{ color: 'var(--dg-node-label)' }}>None attached.</span>
        : children}
    </div>
  )
}

function ResourceCard({
  title,
  tag,
  children,
  compact,
}: {
  title: string
  tag?: string
  children: ReactNode
  compact: boolean
}) {
  return (
    <div style={{
      border: '1px solid var(--dn-border-lt)',
      borderRadius: 4,
      padding: compact ? '7px 10px' : '9px 11px',
      display: 'flex',
      flexDirection: 'column',
      gap: compact ? 4 : 5,
    }}>
      <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
        <span style={{ fontFamily: 'var(--font-mono)', fontWeight: 600 }}>{title}</span>
        {tag && (
          <span style={{
            fontSize: 10,
            letterSpacing: '0.06em',
            textTransform: 'uppercase',
            padding: '1px 6px',
            borderRadius: 3,
            border: '1px solid var(--dg-interactive)',
            color: 'var(--dg-interactive)',
          }}>{tag}</span>
        )}
      </div>
      {children}
    </div>
  )
}

import { useEffect, useState } from 'react'
import { api } from '../api'
import type { HostDetail } from '../api'

// Read-only: everything here describes what Azure already has. Nothing in this
// panel mutates a resource, so there is no confirm gate and no destructive path
// to guard — clicking a node is as safe as hovering one.

function Row({ label, value, full }: { label: string; value: string; full?: string }) {
  return (
    <div style={{ display: 'flex', gap: 10, alignItems: 'baseline', minWidth: 0 }}>
      <span
        style={{
          flexShrink: 0, width: 96, fontSize: 10, letterSpacing: '0.06em',
          textTransform: 'uppercase', color: 'var(--dg-node-label)',
        }}
      >{label}</span>
      {/* Resource IDs are long and unbreakable; wrapping anywhere keeps them
          inside the panel instead of forcing it to scroll sideways. */}
      <span
        title={full}
        style={{ minWidth: 0, overflowWrap: 'anywhere', fontFamily: 'var(--font-mono)' }}
      >
        {value}
      </span>
    </div>
  )
}

function Card({ title, tag, children }: {
  title: string; tag?: string; children: React.ReactNode
}) {
  return (
    <div
      style={{
        border: '1px solid var(--dn-border-lt)', borderRadius: 4,
        padding: '9px 11px', display: 'flex', flexDirection: 'column', gap: 5,
      }}
    >
      <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
        <span style={{ fontFamily: 'var(--font-mono)', fontWeight: 600 }}>{title}</span>
        {tag && (
          <span
            style={{
              fontSize: 10, letterSpacing: '0.06em', textTransform: 'uppercase',
              padding: '1px 6px', borderRadius: 3,
              border: '1px solid var(--dg-interactive)', color: 'var(--dg-interactive)',
            }}
          >{tag}</span>
        )}
      </div>
      {children}
    </div>
  )
}

function BastionDetail({ detail }: { detail: HostDetail }) {
  const deployed = !!detail.cloud_id
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
      <div style={{
        fontSize: 10, letterSpacing: '0.08em', textTransform: 'uppercase',
        color: 'var(--dg-interactive)',
      }}>Managed Service</div>
      <Row label="Status" value={deployed ? (detail.status || 'deployed') : 'not deployed'} />
      {detail.resource_group && <Row label="Resource grp" value={detail.resource_group} />}
      {detail.ip_public && <Row label="Public IP" value={detail.ip_public} />}
      {detail.cloud_id && (
        <Row label="Resource ID" value={shortName(detail.cloud_id)} full={detail.cloud_id} />
      )}
      {detail.last_checked_at && (
        <Row label="Last sync" value={new Date(detail.last_checked_at).toLocaleString()} />
      )}
      {!deployed && (
        <div style={{
          marginTop: 4, color: 'var(--dg-node-label)', fontSize: 11, lineHeight: 1.5,
        }}>
          The bastion has not been discovered yet. Run <code style={{
            padding: '1px 5px', borderRadius: 3, background: 'var(--dn-bg)',
            border: '1px solid var(--dn-border-lt)', fontFamily: 'var(--font-mono)',
          }}>/instances</code> to refresh range state.
        </div>
      )}
    </div>
  )
}

/**
 * Disks and NICs attached to one range host, or bastion service summary.
 *
 * Azure only, and the backend says so in words rather than returning an empty
 * payload: a blank panel would read as "this VM has no disks" instead of "we
 * cannot tell you yet".
 */
export default function HostDetailPanel(
  { sessionId, nodeId, onClose }:
    { sessionId: string | null; nodeId: string; onClose: () => void },
) {
  const [detail, setDetail] = useState<HostDetail | null>(null)
  const [reason, setReason] = useState<string | null>(null)

  useEffect(() => {
    if (!sessionId) return
    // Clicking a second node while the first is in flight must not render the
    // first node's disks under the second node's name.
    let live = true
    setDetail(null)
    setReason(null)
    api.hostDetail(sessionId, nodeId)
      .then(d => {
        if (!live) return
        // The backend echoes the node it answered for. A mismatch should be
        // impossible, but dropping it silently would leave the panel reading
        // "Reading Azure…" forever, so it fails loudly instead.
        if (d.node_id === nodeId) setDetail(d)
        else setReason(`this answer describes ${d.node_id}, not ${nodeId}`)
      })
      .catch(e => { if (live) setReason(e?.message || 'could not read this host') })
    return () => { live = false }
  }, [sessionId, nodeId])

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose() }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  return (
    <div
      onClick={onClose}
      style={{
        position: 'fixed', inset: 0, zIndex: 40,
        background: 'rgba(0,0,0,0.55)', display: 'flex',
        alignItems: 'center', justifyContent: 'center', padding: 24,
      }}
    >
      <div
        // The backdrop closes; the panel itself must not, or every click inside
        // it dismisses the thing being read.
        onClick={e => e.stopPropagation()}
        role="dialog"
        aria-label={`Attached resources for ${detail?.name || nodeId}`}
        style={{
          width: 'min(760px, 100%)', maxHeight: '82vh', overflowY: 'auto',
          background: 'var(--dn-surface)', border: '1px solid var(--dn-border)',
          borderRadius: 6, padding: 18, color: 'var(--dn-text)', fontSize: 12,
          display: 'flex', flexDirection: 'column', gap: 14,
        }}
      >
        <div style={{ display: 'flex', alignItems: 'baseline', gap: 10 }}>
          <span style={{ fontFamily: 'var(--font-mono)', fontSize: 14, fontWeight: 600 }}>
            {detail?.name || nodeId}
          </span>
          {detail?.power_state && (
            <span style={{ color: 'var(--dg-node-value)' }}>{detail.power_state}</span>
          )}
          <button
            onClick={onClose}
            style={{
              marginLeft: 'auto', padding: '2px 9px', borderRadius: 3,
              border: '1px solid var(--dg-interactive)', background: 'transparent',
              color: 'var(--dg-interactive)', cursor: 'pointer',
            }}
          >CLOSE</button>
        </div>

        {reason && (
          <div style={{
            color: 'var(--dg-node-label)', fontFamily: 'var(--font-mono)',
            fontSize: 11, lineHeight: 1.5, padding: '8px 10px',
            border: '1px solid var(--dn-border-lt)', borderRadius: 4,
            background: 'var(--dn-bg)',
          }}>{reason}</div>
        )}
        {!detail && !reason && (
          <div style={{ display: 'flex', alignItems: 'center', gap: 8, color: 'var(--dg-node-label)' }}>
            <span className="dg-spinner" />
            Fetching details…
          </div>
        )}

        {detail && detail.kind === 'bastion' && <BastionDetail detail={detail} />}
        {detail && detail.kind !== 'bastion' && (
          <>
            <div style={{ display: 'flex', flexDirection: 'column', gap: 3 }}>
              {detail.vm_size && <Row label="Size" value={detail.vm_size} />}
              {detail.location && <Row label="Region" value={detail.location} />}
              <Row label="Resource grp" value={detail.resource_group} />
            </div>

            <Section title={`Disks (${(detail.disks || []).length})`}>
              {(detail.disks || []).map(d => (
                <Card key={`${d.role}-${d.name}-${d.lun ?? 'os'}`} title={d.name} tag={d.role}>
                  {d.size_gb != null && <Row label="Size" value={`${d.size_gb} GiB`} />}
                  {d.storage_type && <Row label="Type" value={d.storage_type} />}
                  {d.caching && <Row label="Caching" value={d.caching} />}
                  {d.lun != null && <Row label="LUN" value={String(d.lun)} />}
                </Card>
              ))}
            </Section>

            <Section title={`Network interfaces (${(detail.nics || []).length})`}>
              {(detail.nics || []).map(n => (
                <Card key={n.id} title={n.name} tag={n.primary ? 'primary' : undefined}>
                  {n.private_ips.length > 0 && (
                    <Row label="Private IP" value={n.private_ips.join(', ')} />
                  )}
                  {n.mac_address && <Row label="MAC" value={n.mac_address} />}
                  {n.subnet_id && <Row label="Subnet" value={shortName(n.subnet_id)} full={n.subnet_id} />}
                  {n.nsg_id && <Row label="NSG" value={shortName(n.nsg_id)} full={n.nsg_id} />}
                  {n.accelerated_networking && <Row label="Accel net" value="enabled" />}
                  {n.public_ip_id && <Row label="Public IP" value={shortName(n.public_ip_id)} full={n.public_ip_id} />}
                </Card>
              ))}
            </Section>
          </>
        )}
      </div>
    </div>
  )
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  const items = Array.isArray(children) ? children : [children]
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 7 }}>
      <span
        style={{
          fontSize: 10, letterSpacing: '0.08em', textTransform: 'uppercase',
          color: 'var(--dg-interactive)',
        }}
      >{title}</span>
      {items.length === 0
        ? <span style={{ color: 'var(--dg-node-label)' }}>None attached.</span>
        : children}
    </div>
  )
}

// Resource IDs are ~200 characters of subscription and provider path. The last
// segment is the part an operator reads; the full ID is in the tooltip.
function shortName(id: string): string {
  const parts = id.split('/')
  return parts[parts.length - 1] || id
}

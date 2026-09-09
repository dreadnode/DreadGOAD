import { useEffect, useState } from 'react'
import { api } from '../api'
import type { HostDetail } from '../api'
import { shortResourceId } from '../format'
import Modal from './Modal'
import VMResourceDetails from './VMResourceDetails'

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
        <Row label="Resource ID" value={shortResourceId(detail.cloud_id)} full={detail.cloud_id} />
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

  return (
    <Modal
      onClose={onClose}
      width="min(760px, 100%)"
      maxHeight="82vh"
      zIndex={40}
      ariaLabel={`Attached resources for ${detail?.name || nodeId}`}
      borderColor="var(--dn-border)"
      backdropOpacity={0.55}
      backdropPadding={24}
      panelPadding={18}
      gap={14}
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

            <VMResourceDetails detail={detail} variant="modal" />
          </>
        )}
    </Modal>
  )
}

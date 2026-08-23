// "Connect" modal for the attack box: shows the Bastion tunnel command and the
// ssh command that goes through it, each with a copy button. The console runs
// neither — the operator pastes them into their own terminal, which is why this
// adds no execution surface to the server.
import type { RangeHost, Session } from '../types'
import { BASTION_LOCAL_PORT, buildConnectPlan } from '../connect'
import Modal from './Modal'
import CopyableCommand from './CopyableCommand'

export default function ConnectModal(
  { session, host, onClose }:
    { session?: Session; host: RangeHost; onClose: () => void },
) {
  const plan = buildConnectPlan(session, host)

  return (
    <Modal onClose={onClose} width={620}>
        <div style={{
          color: 'var(--dg-brand)', fontWeight: 700, fontSize: 13, marginBottom: 4,
        }}>Connect to {host.hostname}</div>
        {/* Every secondary line in this modal uses --dg-node-label (6.5:1) or
            --dg-node-value (8.4:1). The generic --dn-text-muted / --dn-text-dim
            tokens measure 3.0:1 and 1.8:1 on --dn-surface — below the 4.5:1 AA
            floor, which is exactly why the node metadata has its own pair (see
            index.css). This is a wall of prose someone reads once and follows
            precisely, so it needs the calibrated greys more than the nodes do. */}
        <div style={{
          color: 'var(--dg-node-label)', fontSize: 11, lineHeight: 1.6,
          marginBottom: 18,
        }}>
          {plan.kind === 'azure-bastion'
            ? `Run these in your own terminal — the console does not run them for
               you. Each needs its own terminal: the tunnel keeps running while
               you use it.`.replace(/\s+/g, ' ')
            : plan.kind === 'aws-ssm'
              ? `Run this in your own terminal — the console does not run it for
                 you.`.replace(/\s+/g, ' ')
              : 'Nothing to copy for this host yet.'}
        </div>

        {plan.kind === 'azure-bastion' && (
          <>
            <CopyableCommand
              step={1}
              label="Open the Bastion tunnel"
              value={plan.tunnel}
              hint="keeps running — leave this terminal open"
            />
            <CopyableCommand
              step={2}
              label="SSH through the tunnel"
              value={plan.ssh}
              hint="second terminal"
            />
            <div style={{
              color: 'var(--dg-node-label)', fontSize: 10.5, lineHeight: 1.75,
              marginTop: 4, paddingTop: 12,
              borderTop: '1px solid var(--dn-border)',
            }}>
              Port {BASTION_LOCAL_PORT} is local to your machine. If it is already
              taken — a leftover tunnel, or a second range open — change it in
              both commands.
              <br />
              The key is written by terraform at deploy time; if{' '}
              <span style={{ color: 'var(--dg-node-value)' }}>{plan.keyPath}</span>{' '}
              is missing, this range was deployed from a different machine.
            </div>
          </>
        )}

        {plan.kind === 'aws-ssm' && (
          <>
            <CopyableCommand
              step={1}
              label="Open an SSM session"
              value={plan.session}
              hint="one terminal — no tunnel, no key"
            />
            <div style={{
              color: 'var(--dg-node-label)', fontSize: 10.5, lineHeight: 1.75,
              marginTop: 4, paddingTop: 12,
              borderTop: '1px solid var(--dn-border)',
            }}>
              Needs the AWS CLI's{' '}
              <span style={{ color: 'var(--dg-node-value)' }}>session-manager-plugin</span>{' '}
              installed locally, and the instance must be registered with SSM —
              the agent is not preinstalled on Kali images.
            </div>
          </>
        )}

        {plan.kind === 'no-attack-box' && (
          <div style={{ color: 'var(--dn-warning)', fontSize: 11, lineHeight: 1.7 }}>
            This range has been read and has no attack box in it.
            <div style={{ color: 'var(--dg-node-label)', marginTop: 6 }}>
              {plan.provider === 'azure'
                ? 'The Kali box is optional on Azure — deploy it with `dreadgoad infra apply --with-kali`.'
                : 'DreadGOAD does not provision an attack box on AWS yet. Any instance in this environment whose Name contains "kali" or "attack" is picked up automatically, and this will fill in on the next read.'}
            </div>
          </div>
        )}

        {plan.kind === 'unsupported-provider' && (
          <div style={{ color: 'var(--dn-warning)', fontSize: 11, lineHeight: 1.7 }}>
            No connect recipe for{' '}
            <span style={{ color: 'var(--dg-node-value)' }}>{plan.provider}</span>{' '}
            ranges — only Azure (Bastion) and AWS (SSM) are supported here.
          </div>
        )}

        {plan.kind === 'incomplete' && (
          <div style={{ color: 'var(--dn-warning)', fontSize: 11, lineHeight: 1.7 }}>
            Not enough is known about this range yet. The resource group and the
            VM's cloud id are learned when the range is first read — run{' '}
            <span style={{ color: 'var(--dg-node-value)' }}>/instances</span> and
            try again.
          </div>
        )}

        <div style={{ display: 'flex', justifyContent: 'flex-end', marginTop: 18 }}>
          <button
            onClick={onClose}
            style={{
              padding: '5px 14px', borderRadius: 3, cursor: 'pointer', fontSize: 11,
              border: '1px solid var(--dn-border-lt)', background: 'transparent',
              color: 'var(--dg-node-label)', fontFamily: 'var(--font-mono)',
            }}
          >CLOSE</button>
        </div>
    </Modal>
  )
}

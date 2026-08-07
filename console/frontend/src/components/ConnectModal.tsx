// "Connect" modal for the attack box: shows the Bastion tunnel command and the
// ssh command that goes through it, each with a copy button. The console runs
// neither — the operator pastes them into their own terminal, which is why this
// adds no execution surface to the server.
import { useEffect, useState } from 'react'
import type { RangeHost, Session } from '../types'
import { BASTION_LOCAL_PORT, buildConnectPlan } from '../connect'

/** One read-only command with a copy button. */
function CommandField(
  { step, label, value, hint }:
    { step: number; label: string; value: string; hint: string },
) {
  const [state, setState] = useState<'idle' | 'copied' | 'failed'>('idle')

  // Revert the button after a beat so a second copy still reads as an action.
  useEffect(() => {
    if (state === 'idle') return
    const id = setTimeout(() => setState('idle'), 1600)
    return () => clearTimeout(id)
  }, [state])

  const copy = () => {
    // localhost is a secure context, so the async clipboard API is available —
    // but it still rejects if the document is not focused or permission is
    // denied, and a copy button that silently does nothing is worse than one
    // that admits it. The text stays selectable either way.
    navigator.clipboard?.writeText(value)
      .then(() => setState('copied'))
      .catch(() => setState('failed'))
  }

  return (
    <div style={{ marginBottom: 16 }}>
      <div style={{
        display: 'flex', alignItems: 'baseline', gap: 8, marginBottom: 6,
      }}>
        {/* The step number carries the ordering, so it stays coloured while the
            label itself goes bright — bold alone on a mid-blue reads as emphasis
            at 11px but not as a heading. */}
        <span style={{ color: 'var(--dn-electric)', fontSize: 12, fontWeight: 700 }}>
          {step}.
        </span>
        <span style={{
          color: 'var(--dn-text-bright)', fontSize: 12, fontWeight: 700,
          letterSpacing: 0.2,
        }}>{label}</span>
        {/* --dg-node-label, not --dn-text-dim: the dim token measures 1.8:1 on
            this surface (see index.css) and this hint was unreadable. */}
        <span style={{ color: 'var(--dg-node-label)', fontSize: 10 }}>{hint}</span>
      </div>
      <div className="dg-cmd" style={{
        display: 'flex', alignItems: 'stretch',
        border: '1px solid var(--dn-border-lt)', borderRadius: 4,
        // A shade below the modal so the block reads as a terminal inset rather
        // than another panel sitting on top of it.
        background: 'var(--dn-black)',
        overflow: 'hidden',
      }}>
        {/* A textarea, not a div: it is selectable and scrollable with the
            keyboard, so the command is still reachable when the clipboard API
            refuses. readOnly rather than disabled keeps it focusable. */}
        <textarea
          readOnly
          value={value}
          spellCheck={false}
          // Sized to the content: the tunnel command is ~5 wrapped lines and the
          // ssh one is 2, and a fixed height clipped the longer one mid-word
          // behind a scrollbar — which hides exactly the part (the resource id)
          // an operator would want to eyeball before pasting. ~58 characters per
          // line at this width; capped so a pathological value can't push the
          // buttons off screen.
          rows={Math.min(7, Math.max(2, Math.ceil(value.length / 58)))}
          onFocus={e => e.currentTarget.select()}
          style={{
            flex: 1, minWidth: 0, resize: 'none', border: 'none', outline: 'none',
            background: 'transparent', color: 'var(--dn-text)',
            fontFamily: 'var(--font-mono)', fontSize: 11.5, lineHeight: 1.65,
            padding: '10px 0 10px 12px',
          }}
        />
        <button
          className="dg-copy"
          onClick={copy}
          title="Copy to clipboard"
          aria-label={`Copy: ${label}`}
          style={{
            flexShrink: 0, width: 44, border: 'none', cursor: 'pointer',
            borderLeft: '1px solid var(--dn-border-lt)', background: 'transparent',
            color: state === 'copied' ? 'var(--dn-success)'
              : state === 'failed' ? 'var(--dn-error)' : 'var(--dg-node-label)',
            fontFamily: 'var(--font-mono)', fontSize: 14,
          }}
        >{state === 'copied' ? '✓' : state === 'failed' ? '✕' : '⧉'}</button>
      </div>
      {state === 'failed' && (
        <div style={{ color: 'var(--dn-error)', fontSize: 10, marginTop: 4 }}>
          clipboard blocked — select the text and copy manually
        </div>
      )}
    </div>
  )
}

export default function ConnectModal(
  { session, host, onClose }:
    { session?: Session; host: RangeHost; onClose: () => void },
) {
  const plan = buildConnectPlan(session, host)

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose() }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [onClose])

  return (
    <div
      onClick={onClose}
      style={{
        position: 'fixed', inset: 0, zIndex: 100, background: 'rgba(0,0,0,0.6)',
        display: 'flex', alignItems: 'center', justifyContent: 'center',
      }}
    >
      <div
        onClick={e => e.stopPropagation()}
        style={{
          background: 'var(--dn-surface)', border: '1px solid var(--dn-border-lt)',
          borderRadius: 6, padding: 20, width: 620, maxWidth: '92vw',
          fontFamily: 'var(--font-mono)', fontSize: 12, color: 'var(--dn-text)',
        }}
      >
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
            <CommandField
              step={1}
              label="Open the Bastion tunnel"
              value={plan.tunnel}
              hint="keeps running — leave this terminal open"
            />
            <CommandField
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
            <CommandField
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
      </div>
    </div>
  )
}

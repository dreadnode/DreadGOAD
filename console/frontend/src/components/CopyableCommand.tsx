import { useEffect, useRef, useState } from 'react'

type CopyState = 'idle' | 'copied' | 'failed'

/**
 * Read-only command textarea with a copy button.
 *
 * Used in two contexts: the connect modal (full-size, with step numbers and
 * hints) and the accordion detail row (compact). The `compact` prop switches
 * between the two visual treatments.
 */
export default function CopyableCommand(
  { label, value, compact, step, hint }:
    {
      label: string
      value: string
      compact?: boolean
      step?: number
      hint?: string
    },
) {
  const [state, setState] = useState<CopyState>('idle')
  const timerRef = useRef<ReturnType<typeof setTimeout>>()
  useEffect(() => () => clearTimeout(timerRef.current), [])

  const showState = (next: Exclude<CopyState, 'idle'>) => {
    setState(next)
    clearTimeout(timerRef.current)
    timerRef.current = setTimeout(() => setState('idle'), 1600)
  }

  const copy = (e?: React.MouseEvent) => {
    e?.stopPropagation()
    if (!navigator.clipboard) {
      showState('failed')
      return
    }
    navigator.clipboard.writeText(value)
      .then(() => showState('copied'))
      .catch(() => showState('failed'))
  }

  const btnColor = state === 'copied' ? 'var(--dn-success)'
    : state === 'failed' ? 'var(--dn-error)' : 'var(--dg-node-label)'

  return (
    <div style={compact ? undefined : { marginBottom: 16 }}>
      {compact ? (
        <span style={{
          fontSize: 10, letterSpacing: '0.05em', textTransform: 'uppercase',
          color: 'var(--dn-electric)', fontWeight: 700,
        }}>{label}</span>
      ) : (
        <div style={{
          display: 'flex', alignItems: 'baseline', gap: 8, marginBottom: 6,
        }}>
          {step != null && (
            <span style={{ color: 'var(--dn-electric)', fontSize: 12, fontWeight: 700 }}>
              {step}.
            </span>
          )}
          <span style={{
            color: 'var(--dn-text-bright)', fontSize: 12, fontWeight: 700,
            letterSpacing: 0.2,
          }}>{label}</span>
          {hint && (
            <span style={{ color: 'var(--dg-node-label)', fontSize: 10 }}>{hint}</span>
          )}
        </div>
      )}
      <div className="dg-cmd" style={{
        display: 'flex', alignItems: 'stretch',
        marginTop: compact ? 3 : undefined,
        border: '1px solid var(--dn-border-lt)',
        borderRadius: compact ? 3 : 4,
        background: 'var(--dn-black)',
        overflow: 'hidden',
      }}>
        <textarea
          readOnly
          value={value}
          spellCheck={false}
          rows={Math.min(7, Math.max(2, Math.ceil(value.length / 58)))}
          onFocus={e => e.currentTarget.select()}
          style={{
            flex: 1, minWidth: 0, resize: 'none', border: 'none', outline: 'none',
            background: 'transparent', color: 'var(--dn-text)',
            fontFamily: 'var(--font-mono)',
            fontSize: compact ? 11 : 11.5,
            lineHeight: 1.65,
            padding: compact ? '6px 8px' : '10px 0 10px 12px',
          }}
        />
        <button
          className="dg-copy"
          onClick={copy}
          title="Copy to clipboard"
          aria-label={`Copy: ${label}`}
          style={{
            flexShrink: 0, width: compact ? 32 : 44,
            border: 'none', cursor: 'pointer',
            borderLeft: '1px solid var(--dn-border-lt)', background: 'transparent',
            color: btnColor,
            fontFamily: 'var(--font-mono)', fontSize: compact ? 13 : 14,
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

import { useCallback, useEffect, useRef } from 'react'
import Modal from './Modal'
import { btnStyle } from './FormFields'

export function confirmActionForKey(key: string): 'confirm' | 'deny' | null {
  const normalized = key.toLowerCase()
  if (normalized === 'c') return 'confirm'
  if (normalized === 'd') return 'deny'
  return null
}

export function formatArgvForShell(argv: string[]): string {
  return commandTokens(argv).map(token => token.text).join('')
}

export type CommandToken = {
  kind: 'executable' | 'subcommand' | 'flag' | 'value'
  text: string
}

export function commandTokens(argv: string[]): CommandToken[] {
  return argv.map((arg, index) => {
    const formatted = arg && /^[A-Za-z0-9_@%+=:,./-]+$/.test(arg)
      ? arg
      : `'${arg.replace(/'/g, `'\\''`)}'`
    const kind = index === 0
      ? 'executable'
      : index === 1 && !arg.startsWith('-')
        ? 'subcommand'
        : arg.startsWith('-')
          ? 'flag'
          : 'value'
    return { kind, text: `${index === 0 ? '' : ' '}${formatted}` }
  })
}

const commandTokenColor: Record<CommandToken['kind'], string> = {
  executable: 'var(--dg-interactive)',
  subcommand: 'var(--dn-warning)',
  flag: 'var(--dn-electric)',
  value: 'var(--dn-success)',
}

export default function ConfirmModal({ title, message, codeLabel, commandArgv, confirmLabel = 'CONFIRM', destructive, onConfirm, onCancel }: {
  title: string
  message: string
  codeLabel?: string
  commandArgv?: string[]
  confirmLabel?: string
  destructive?: boolean
  onConfirm: () => void | boolean
  onCancel: () => void | boolean
}) {
  const btnRef = useRef<HTMLButtonElement>(null)
  const settledRef = useRef(false)
  const choose = useCallback((action: 'confirm' | 'deny') => {
    if (settledRef.current) return
    const handled = action === 'confirm' ? onConfirm() : onCancel()
    // A caller may report that transport was unavailable. Leave the modal live
    // and retryable; backend approval still remains pending and fail-closed.
    if (handled === false) return
    settledRef.current = true
  }, [onConfirm, onCancel])
  useEffect(() => { btnRef.current?.focus() }, [])
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.repeat || event.altKey || event.ctrlKey || event.metaKey) return
      const action = confirmActionForKey(event.key)
      if (action === null) return
      event.preventDefault()
      choose(action)
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [choose])

  return (
    <Modal onClose={() => choose('deny')} width={400} ariaLabel={title}>
      <div style={{
        borderLeft: `3px solid ${destructive ? 'var(--dn-error)' : 'var(--dg-brand)'}`,
        paddingLeft: 14, minWidth: 0,
        marginBottom: 20,
      }}>
        <div style={{
          color: destructive ? 'var(--dn-error)' : 'var(--dg-brand)',
          fontWeight: 700, fontSize: 13, marginBottom: 8,
        }}>{title}</div>
        <div style={{
          color: 'var(--dn-text)', fontSize: 12, lineHeight: 1.6,
          whiteSpace: 'pre-wrap', overflowWrap: 'anywhere', wordBreak: 'break-word',
          maxWidth: '100%',
        }}>{message}</div>
        {commandArgv && (
          <div style={{ marginTop: 14 }}>
            {codeLabel && (
              <div style={{ color: 'var(--dn-text-muted)', fontSize: 10, marginBottom: 5 }}>
                {codeLabel}
              </div>
            )}
            <pre style={{
              margin: 0, padding: '10px 12px', borderRadius: 4,
              border: '1px solid var(--dn-border)', background: 'var(--dn-bg)',
              color: 'var(--dn-text-bright)', fontFamily: 'var(--font-mono)',
              fontSize: 11, lineHeight: 1.55, whiteSpace: 'pre-wrap',
              overflowWrap: 'anywhere', wordBreak: 'break-word', maxWidth: '100%',
              boxSizing: 'border-box', userSelect: 'text',
            }}><code>{commandTokens(commandArgv).map((token, index) => (
              <span key={index} data-command-token={token.kind} style={{ color: commandTokenColor[token.kind] }}>
                {token.text}
              </span>
            ))}</code></pre>
          </div>
        )}
      </div>
      <div style={{
        color: 'var(--dn-text-dim)', fontSize: 10, marginBottom: 10,
        textAlign: 'right',
      }}>
        Press <strong>C</strong> to confirm · <strong>D</strong> to deny
      </div>
      <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8 }}>
        <button onClick={() => choose('deny')} style={btnStyle(false)}>CANCEL</button>
        <button
          ref={btnRef}
          onClick={() => choose('confirm')}
          style={{
            ...btnStyle(true),
            ...(destructive ? { background: 'var(--dn-error)' } : {}),
          }}
        >{confirmLabel}</button>
      </div>
    </Modal>
  )
}

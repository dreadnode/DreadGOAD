import { useEffect, useRef } from 'react'
import Modal from './Modal'
import { btnStyle } from './FormFields'

export default function ConfirmModal({ title, message, confirmLabel = 'CONFIRM', destructive, onConfirm, onCancel }: {
  title: string
  message: string
  confirmLabel?: string
  destructive?: boolean
  onConfirm: () => void
  onCancel: () => void
}) {
  const btnRef = useRef<HTMLButtonElement>(null)
  useEffect(() => { btnRef.current?.focus() }, [])

  return (
    <Modal onClose={onCancel} width={400} ariaLabel={title}>
      <div style={{
        borderLeft: `3px solid ${destructive ? 'var(--dn-error)' : 'var(--dg-brand)'}`,
        paddingLeft: 14,
        marginBottom: 20,
      }}>
        <div style={{
          color: destructive ? 'var(--dn-error)' : 'var(--dg-brand)',
          fontWeight: 700, fontSize: 13, marginBottom: 8,
        }}>{title}</div>
        <div style={{
          color: 'var(--dn-text)', fontSize: 12, lineHeight: 1.6,
          whiteSpace: 'pre-wrap',
        }}>{message}</div>
      </div>
      <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8 }}>
        <button onClick={onCancel} style={btnStyle(false)}>CANCEL</button>
        <button
          ref={btnRef}
          onClick={onConfirm}
          style={{
            ...btnStyle(true),
            ...(destructive ? { background: 'var(--dn-error)' } : {}),
          }}
        >{confirmLabel}</button>
      </div>
    </Modal>
  )
}

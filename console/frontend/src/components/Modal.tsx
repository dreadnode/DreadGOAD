import { useEffect, type CSSProperties, type ReactNode } from 'react'

export default function Modal(
  { onClose, width, maxHeight, zIndex = 100, ariaLabel, borderColor,
    backdropPadding, backdropOpacity = 0.6, panelPadding = 20, gap, children }:
    {
      onClose: () => void
      width: CSSProperties['width']
      maxHeight?: CSSProperties['maxHeight']
      zIndex?: number
      ariaLabel: string
      borderColor?: string
      backdropPadding?: number
      backdropOpacity?: number
      panelPadding?: number
      gap?: number
      children: ReactNode
    },
) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose() }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [onClose])

  return (
    <div
      onClick={onClose}
      style={{
        position: 'fixed', inset: 0, zIndex,
        background: `rgba(0,0,0,${backdropOpacity})`,
        display: 'flex', alignItems: 'center', justifyContent: 'center',
        padding: backdropPadding,
      }}
    >
      <div
        onClick={e => e.stopPropagation()}
        role="dialog"
        aria-label={ariaLabel}
        style={{
          width, maxWidth: '92vw',
          maxHeight, overflowY: maxHeight ? 'auto' : undefined,
          background: 'var(--dn-surface)',
          border: `1px solid ${borderColor ?? 'var(--dn-border-lt)'}`,
          borderRadius: 6, padding: panelPadding,
          fontFamily: 'var(--font-mono)', fontSize: 12,
          color: 'var(--dn-text)',
          display: gap != null ? 'flex' : undefined,
          flexDirection: gap != null ? 'column' : undefined,
          gap,
        }}
      >
        {children}
      </div>
    </div>
  )
}

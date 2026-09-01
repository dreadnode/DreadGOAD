import { useId } from 'react'

export function Field({ label, value, onChange, placeholder, type, onBlur, suggestions }: {
  label: string
  value: string
  onChange: (v: string) => void
  placeholder?: string
  type?: string
  onBlur?: () => void
  /** Offered via a datalist — the field stays free text, so these never constrain. */
  suggestions?: string[]
}) {
  // useId, not a module counter: a datalist is referenced by id, so each field
  // needs its own or they attach the wrong suggestions to each other. Deriving
  // it from a counter meant writing to a module global during render — impure,
  // and double-counted under StrictMode (main.tsx enables it). useId exists for
  // exactly this and is stable across renders without the side effect.
  const listId = useId()
  const hasList = !!suggestions?.length

  return (
    <div style={{ marginBottom: 12 }}>
      {/* Bright and bold: --dn-text-dim measured 1.94:1 against the modal
          surface, far under the 4.5:1 floor, which left the field labels
          barely visible. These name what you are about to type into a form
          that creates a range — the last thing that should be guessed at. */}
      <label style={{
        display: 'block', marginBottom: 4,
        color: 'var(--dn-text-bright)', fontWeight: 700,
      }}>{label}</label>
      <input type={type} value={value} placeholder={placeholder} onChange={e => onChange(e.target.value)} onBlur={onBlur}
        list={hasList ? listId : undefined} style={{
        width: '100%', boxSizing: 'border-box', padding: '6px 8px', background: 'var(--dn-bg)',
        border: '1px solid var(--dn-border)', borderRadius: 3, color: 'var(--dn-text)',
        fontFamily: 'var(--font-mono)', fontSize: 12,
      }} />
      {hasList && (
        <datalist id={listId}>
          {suggestions!.map(s => <option key={s} value={s} />)}
        </datalist>
      )}
    </div>
  )
}

/** A labelled <select>, matching Field's label treatment and input chrome. */
export function Select({ label, value, onChange, options, disabled }: {
  label: string
  value: string
  onChange: (v: string) => void
  options: Array<{ value: string; label: string; disabled?: boolean }>
  disabled?: boolean
}) {
  return (
    <div style={{ marginBottom: 12 }}>
      <label style={{
        display: 'block', marginBottom: 4,
        color: 'var(--dn-text-bright)', fontWeight: 700,
      }}>{label}</label>
      <select
        value={value}
        disabled={disabled}
        onChange={e => onChange(e.target.value)}
        style={{
          width: '100%', boxSizing: 'border-box', padding: '6px 8px', background: 'var(--dn-bg)',
          border: '1px solid var(--dn-border)', borderRadius: 3, color: 'var(--dn-text)',
          fontFamily: 'var(--font-mono)', fontSize: 12,
        }}
      >
        {options.length === 0 && <option value="">—</option>}
        {options.map(o => (
          <option key={o.value} value={o.value} disabled={o.disabled}>{o.label}</option>
        ))}
      </select>
    </div>
  )
}

export function btnStyle(primary: boolean): React.CSSProperties {
  return {
    background: primary ? 'var(--dg-brand)' : 'transparent',
    border: primary ? 'none' : '1px solid var(--dn-border-lt)',
    color: primary ? 'var(--dn-black)' : 'var(--dn-text-dim)',
    fontFamily: 'var(--font-mono)', fontSize: 11, fontWeight: primary ? 700 : 400,
    padding: '4px 12px', borderRadius: 3, cursor: 'pointer',
  }
}

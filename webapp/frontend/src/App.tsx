import { useEffect, useState } from 'react'

// Phase 0 placeholder shell. Phase 5 replaces this with the two-pane
// layout (TerminalChat + RangeView) and the session tab bar.
export default function App() {
  const [version, setVersion] = useState<string>('')
  const [ok, setOk] = useState<boolean | null>(null)

  useEffect(() => {
    fetch('/api/health')
      .then(r => r.json())
      .then(d => { setOk(d.status === 'ok'); setVersion(d.version || '') })
      .catch(() => setOk(false))
  }, [])

  return (
    <div style={{
      position: 'fixed', inset: 0, display: 'flex',
      alignItems: 'center', justifyContent: 'center', flexDirection: 'column',
      gap: 12, background: 'var(--dn-black)',
    }}>
      <div style={{ color: 'var(--dg-brand)', fontSize: 24, fontWeight: 700, letterSpacing: '0.1em' }}>
        DreadGOAD
      </div>
      <div style={{ color: 'var(--dn-text-dim)', fontSize: 12 }}>
        {ok === null ? 'connecting…' : ok ? `backend online · v${version}` : 'backend offline'}
      </div>
    </div>
  )
}

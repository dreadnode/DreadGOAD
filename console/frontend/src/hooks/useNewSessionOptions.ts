import { useEffect, useState } from 'react'
import { api, type SessionOptions } from '../api'

export interface NewSessionOptionsState {
  options: SessionOptions | null
  loading: boolean
  error: string
}

/** Load the range catalog and existing environment anchors as one snapshot. */
export function useNewSessionOptions(): NewSessionOptionsState {
  const [options, setOptions] = useState<SessionOptions | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  useEffect(() => {
    let cancelled = false
    api.sessionOptions()
      .then(result => {
        if (cancelled) return
        setOptions(result)
        setError('')
      })
      .catch(reason => {
        if (cancelled) return
        setOptions(null)
        setError(reason instanceof Error ? reason.message : String(reason))
      })
      .finally(() => { if (!cancelled) setLoading(false) })
    return () => { cancelled = true }
  }, [])

  return { options, loading, error }
}

import { useCallback, useEffect, useRef, useState } from 'react'

export type ConnectionStatus = 'connecting' | 'connected' | 'disconnected'

// Single reconnecting WebSocket, multiplexed by session_id (design §7).
export function useWebSocket(
  path: string,
  onMessage?: (data: string) => void,
  onOpen?: (send: (data: string) => void) => void,
) {
  const wsRef = useRef<WebSocket | null>(null)
  const [status, setStatus] = useState<ConnectionStatus>('disconnected')
  const onMessageRef = useRef(onMessage)
  const onOpenRef = useRef(onOpen)
  const unmountedRef = useRef(false)

  onMessageRef.current = onMessage
  onOpenRef.current = onOpen

  const send = useCallback((data: string) => {
    if (wsRef.current?.readyState === WebSocket.OPEN) wsRef.current.send(data)
  }, [])

  const connect = useCallback(() => {
    if (unmountedRef.current) return
    const state = wsRef.current?.readyState
    if (state === WebSocket.OPEN || state === WebSocket.CONNECTING) return

    setStatus('connecting')
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const ws = new WebSocket(`${protocol}//${window.location.host}${path}`)

    ws.onopen = () => {
      if (!unmountedRef.current) {
        setStatus('connected')
        onOpenRef.current?.(send)
      }
    }
    ws.onmessage = (event) => onMessageRef.current?.(event.data)
    ws.onclose = () => {
      if (unmountedRef.current) return
      setStatus('disconnected')
      wsRef.current = null
      setTimeout(connect, 2000)
    }
    ws.onerror = () => ws.close()
    wsRef.current = ws
  }, [path, send])

  useEffect(() => {
    unmountedRef.current = false
    connect()
    return () => {
      unmountedRef.current = true
      wsRef.current?.close()
      wsRef.current = null
    }
  }, [connect])

  return { status, send }
}

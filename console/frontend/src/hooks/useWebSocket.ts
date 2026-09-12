import { useCallback, useEffect, useRef, useState } from 'react'
import { websocketProtocols } from '../auth'

export type ConnectionStatus = 'connecting' | 'connected' | 'disconnected'

// Single reconnecting WebSocket, multiplexed by session_id (design §7).
export function useWebSocket(
  path: string,
  onMessage?: (data: string) => void,
  onOpen?: (send: (data: string) => void) => void,
  onClose?: () => void,
) {
  const wsRef = useRef<WebSocket | null>(null)
  const [status, setStatus] = useState<ConnectionStatus>('disconnected')
  const onMessageRef = useRef(onMessage)
  const onOpenRef = useRef(onOpen)
  const onCloseRef = useRef(onClose)
  const unmountedRef = useRef(false)
  const reconnectTimer = useRef<ReturnType<typeof setTimeout> | null>(null)

  onMessageRef.current = onMessage
  onOpenRef.current = onOpen
  onCloseRef.current = onClose

  const send = useCallback((data: string) => {
    if (wsRef.current?.readyState !== WebSocket.OPEN) return false
    try {
      wsRef.current.send(data)
      return true
    } catch {
      return false
    }
  }, [])

  const connect = useCallback(() => {
    if (unmountedRef.current) return
    const state = wsRef.current?.readyState
    if (state === WebSocket.OPEN || state === WebSocket.CONNECTING) return

    setStatus('connecting')
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const url = `${protocol}//${window.location.host}${path}`
    const authProtocols = websocketProtocols()
    const ws = authProtocols ? new WebSocket(url, authProtocols) : new WebSocket(url)

    ws.onopen = () => {
      if (!unmountedRef.current) {
        setStatus('connected')
        onOpenRef.current?.(send)
      }
    }
    ws.onmessage = (event) => onMessageRef.current?.(event.data)
    ws.onclose = () => {
      if (unmountedRef.current || wsRef.current !== ws) return
      setStatus('disconnected')
      wsRef.current = null
      onCloseRef.current?.()
      reconnectTimer.current = setTimeout(connect, 2000)
    }
    ws.onerror = () => ws.close()
    wsRef.current = ws
  }, [path, send])

  useEffect(() => {
    unmountedRef.current = false
    connect()
    return () => {
      unmountedRef.current = true
      if (reconnectTimer.current) clearTimeout(reconnectTimer.current)
      wsRef.current?.close()
      wsRef.current = null
    }
  }, [connect])

  return { status, send }
}

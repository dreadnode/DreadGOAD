import { useCallback, useEffect, useReducer, useRef } from 'react'
import { api } from '../api'
import {
  consoleEventReducer,
  initialConsoleEventState,
  parseConsoleEvent,
} from '../consoleEventState'
import type { ApprovalRequest, Session } from '../types'
import { useWebSocket } from './useWebSocket'

/** Own the multiplexed chat transport and its per-session event state. */
export function useConsoleSessionEvents(
  sessions: Session[],
  onSessionsRefreshed: (sessions: Session[]) => void,
) {
  const [state, dispatch] = useReducer(consoleEventReducer, initialConsoleEventState)
  const sessionsRef = useRef(sessions)
  const resumedRef = useRef<Set<string>>(new Set())
  const refreshRequest = useRef(0)
  sessionsRef.current = sessions

  useEffect(() => () => { ++refreshRequest.current }, [])

  const handleMessage = useCallback((data: string) => {
    const event = parseConsoleEvent(data)
    const sessionId = event?.session_id
    if (!event || !sessionId) return
    dispatch({ type: 'receive', sessionId, event, now: Date.now() })

    // A check also updates session-level cloud facts in the backend snapshot.
    if (event.kind === 'check_run') {
      const request = ++refreshRequest.current
      api.listSessions()
        .then(result => {
          if (request === refreshRequest.current) onSessionsRefreshed(result.sessions)
        })
        .catch(() => {})
    }
  }, [onSessionsRefreshed])

  const resumeWith = useCallback((send: (data: string) => void, sessionId: string) => {
    if (resumedRef.current.has(sessionId)) return
    resumedRef.current.add(sessionId)
    send(JSON.stringify({ type: 'resume', session_id: sessionId }))
  }, [])

  const handleOpen = useCallback((send: (data: string) => void) => {
    // A reconnect starts a new server subscription set; restore every tab.
    resumedRef.current.clear()
    for (const session of sessionsRef.current) resumeWith(send, session.id)
  }, [resumeWith])

  const { status, send } = useWebSocket('/ws/chat', handleMessage, handleOpen)

  const resume = useCallback((sessionId: string) => {
    if (status === 'connected') resumeWith(send, sessionId)
  }, [resumeWith, send, status])

  const beginTurn = useCallback((sessionId: string) => {
    dispatch({ type: 'turn_started', sessionId, now: Date.now() })
  }, [])

  const forgetSession = useCallback((sessionId: string) => {
    resumedRef.current.delete(sessionId)
    dispatch({ type: 'session_removed', sessionId })
  }, [])

  const sendApproval = useCallback((
    sessionId: string,
    request: ApprovalRequest,
    decision: 'confirm' | 'deny',
  ) => {
    const sent = send(JSON.stringify({
      type: 'approval',
      session_id: sessionId,
      approval_id: request.approval_id,
      decision,
    }))
    if (sent) {
      dispatch({
        type: 'approval_sent',
        sessionId,
        approvalId: request.approval_id,
      })
    }
    return sent
  }, [send])

  return {
    state,
    status,
    send,
    resume,
    beginTurn,
    forgetSession,
    sendApproval,
  }
}

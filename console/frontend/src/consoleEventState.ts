import type { ApprovalRequest, ChatEvent, Session } from './types'

const MAX_PROGRESS = 200

type SessionMap<T> = Record<string, T>

export interface ConsoleEventState {
  messages: SessionMap<ChatEvent[]>
  rangeRefresh: SessionMap<number>
  processing: SessionMap<boolean>
  command: SessionMap<string>
  turnStartedAt: SessionMap<number>
  verbSeed: SessionMap<number>
  approvals: SessionMap<ApprovalRequest>
  nextClientId: number
}

export const initialConsoleEventState: ConsoleEventState = {
  messages: {},
  rangeRefresh: {},
  processing: {},
  command: {},
  turnStartedAt: {},
  verbSeed: {},
  approvals: {},
  nextClientId: 0,
}

export type ConsoleEventAction =
  | { type: 'receive'; sessionId: string; event: ChatEvent; now: number }
  | { type: 'turn_started'; sessionId: string; now: number }
  | { type: 'approval_sent'; sessionId: string; approvalId: string }
  | { type: 'session_removed'; sessionId: string }

function without<T>(values: SessionMap<T>, sessionId: string): SessionMap<T> {
  if (!(sessionId in values)) return values
  const next = { ...values }
  delete next[sessionId]
  return next
}

function tagged(event: ChatEvent, id: number): ChatEvent {
  return { ...event, _cid: id }
}

function receiveHistory(
  state: ConsoleEventState,
  sessionId: string,
  event: Extract<ChatEvent, { kind: 'history' }>,
  now: number,
): ConsoleEventState {
  let nextClientId = state.nextClientId
  const events = (event.events || []).map(item => tagged(item, ++nextClientId))
  const running = event.active === true
  const parsedStart = running ? Date.parse(event.started_at || '') : NaN
  const startedAt = running && !Number.isNaN(parsedStart) ? parsedStart : (running ? now : 0)
  const approvals = { ...state.approvals }
  if (event.approval) approvals[sessionId] = event.approval
  else delete approvals[sessionId]

  return {
    ...state,
    messages: { ...state.messages, [sessionId]: events },
    processing: { ...state.processing, [sessionId]: running },
    command: { ...state.command, [sessionId]: running ? event.command || '' : '' },
    turnStartedAt: { ...state.turnStartedAt, [sessionId]: startedAt },
    verbSeed: {
      ...state.verbSeed,
      [sessionId]: running ? state.verbSeed[sessionId] || now : 0,
    },
    approvals,
    nextClientId,
  }
}

function appendProgress(
  messages: ChatEvent[],
  event: ChatEvent,
): ChatEvent[] {
  const updated = [...messages, event]
  let run = 0
  for (let i = updated.length - 1; i >= 0 && updated[i].kind === 'command_progress'; i--) run++
  return run > MAX_PROGRESS
    ? [...updated.slice(0, updated.length - run), ...updated.slice(-MAX_PROGRESS)]
    : updated
}

function receiveEvent(
  state: ConsoleEventState,
  sessionId: string,
  event: ChatEvent,
  now: number,
): ConsoleEventState {
  if (event.kind === 'history') return receiveHistory(state, sessionId, event, now)

  if (event.kind === 'approval_required') {
    if (!event.approval_id || !event.command || !Array.isArray(event.argv)) return state
    return {
      ...state,
      approvals: {
        ...state.approvals,
        [sessionId]: {
          approval_id: event.approval_id,
          command: event.command,
          argv: event.argv,
          detail: event.detail,
          requested_at: event.requested_at,
        },
      },
    }
  }

  if (event.kind === 'approval_resolved') {
    if (state.approvals[sessionId]?.approval_id !== event.approval_id) return state
    return { ...state, approvals: without(state.approvals, sessionId) }
  }

  const nextClientId = state.nextClientId + 1
  const nextEvent = tagged(event, nextClientId)
  const existing = state.messages[sessionId] || []
  const messages = event.kind === 'command_progress'
    ? appendProgress(existing, nextEvent)
    : [...existing, nextEvent]
  let next: ConsoleEventState = {
    ...state,
    messages: { ...state.messages, [sessionId]: messages },
    nextClientId,
  }

  if (event.kind === 'check_run') {
    next = {
      ...next,
      rangeRefresh: {
        ...state.rangeRefresh,
        [sessionId]: (state.rangeRefresh[sessionId] || 0) + 1,
      },
    }
  } else if (event.kind === 'command_run') {
    next = {
      ...next,
      command: {
        ...state.command,
        [sessionId]: event.phase === 'start' ? event.command : '',
      },
    }
  } else if (event.kind === 'agent_end') {
    next = {
      ...next,
      processing: { ...state.processing, [sessionId]: false },
      command: { ...state.command, [sessionId]: '' },
      turnStartedAt: { ...state.turnStartedAt, [sessionId]: 0 },
      verbSeed: { ...state.verbSeed, [sessionId]: 0 },
    }
  }
  return next
}

export function consoleEventReducer(
  state: ConsoleEventState,
  action: ConsoleEventAction,
): ConsoleEventState {
  switch (action.type) {
    case 'receive':
      return receiveEvent(state, action.sessionId, action.event, action.now)
    case 'turn_started':
      return {
        ...state,
        processing: { ...state.processing, [action.sessionId]: true },
        turnStartedAt: { ...state.turnStartedAt, [action.sessionId]: action.now },
        verbSeed: { ...state.verbSeed, [action.sessionId]: action.now },
      }
    case 'approval_sent':
      return state.approvals[action.sessionId]?.approval_id === action.approvalId
        ? { ...state, approvals: without(state.approvals, action.sessionId) }
        : state
    case 'session_removed':
      return {
        ...state,
        messages: without(state.messages, action.sessionId),
        rangeRefresh: without(state.rangeRefresh, action.sessionId),
        processing: without(state.processing, action.sessionId),
        command: without(state.command, action.sessionId),
        turnStartedAt: without(state.turnStartedAt, action.sessionId),
        verbSeed: without(state.verbSeed, action.sessionId),
        approvals: without(state.approvals, action.sessionId),
      }
  }
}

export function parseConsoleEvent(data: string): ChatEvent | null {
  try {
    const value: unknown = JSON.parse(data)
    if (!value || typeof value !== 'object') return null
    const candidate = value as { kind?: unknown; session_id?: unknown }
    return typeof candidate.kind === 'string' && typeof candidate.session_id === 'string'
      && candidate.session_id.length > 0
      ? value as ChatEvent
      : null
  } catch {
    return null
  }
}

/** Apply check-derived snapshots without reviving or dropping local sessions. */
export function mergeSessionSnapshots(current: Session[], refreshed: Session[]): Session[] {
  const byId = new Map(refreshed.map(session => [session.id, session]))
  return current.map(session => {
    const latest = byId.get(session.id)
    return latest ? { ...session, snapshot: latest.snapshot } : session
  })
}

import assert from 'node:assert/strict'
import {
  consoleEventReducer,
  initialConsoleEventState,
  mergeSessionSnapshots,
  parseConsoleEvent,
  type ConsoleEventState,
} from '../src/consoleEventState'
import type { ApprovalRequest, ChatEvent, Session } from '../src/types'

const receive = (
  state: ConsoleEventState,
  event: ChatEvent,
  now = 1_000,
): ConsoleEventState => consoleEventReducer(state, {
  type: 'receive',
  sessionId: event.session_id || 's1',
  event,
  now,
})

assert.equal(parseConsoleEvent('{broken'), null)
assert.equal(parseConsoleEvent(JSON.stringify({ kind: 'status' })), null)
assert.equal(parseConsoleEvent(JSON.stringify({ kind: 'status', session_id: 's1', content: 'ok' }))?.kind, 'status')

const historyApproval: ApprovalRequest = {
  approval_id: 'approval-1',
  command: '/up',
  argv: ['dreadgoad', 'up'],
  detail: 'Creates infrastructure',
  requested_at: '2026-09-09T12:00:00Z',
}
const history = receive(initialConsoleEventState, {
  kind: 'history',
  session_id: 's1',
  events: [{ kind: 'status', content: 'restored' }],
  active: true,
  started_at: '2026-09-09T12:00:00Z',
  command: '/up',
  approval: historyApproval,
})
assert.equal(history.messages.s1[0]._cid, 1)
assert.equal(history.processing.s1, true)
assert.equal(history.command.s1, '/up')
assert.equal(history.turnStartedAt.s1, Date.parse('2026-09-09T12:00:00Z'))
assert.equal(history.verbSeed.s1, 1_000)
assert.equal(history.approvals.s1, historyApproval)

const fallbackStart = receive(history, {
  kind: 'history',
  session_id: 's2',
  events: [],
  active: true,
  started_at: 'invalid',
  command: null,
  approval: null,
}, 2_000)
assert.equal(fallbackStart.turnStartedAt.s2, 2_000)
assert.equal(fallbackStart.command.s2, '')

let progress = receive(fallbackStart, { kind: 'status', session_id: 's1', content: 'before' })
for (let i = 0; i < 205; i++) {
  progress = receive(progress, { kind: 'command_progress', session_id: 's1', line: String(i) })
}
assert.equal(progress.messages.s1.length, 202)
assert.equal(progress.messages.s1[0].kind, 'status')
assert.equal(progress.messages.s1[1].kind, 'status')
assert.equal(progress.messages.s1[2].kind === 'command_progress' && progress.messages.s1[2].line, '5')

const check = receive(progress, { kind: 'check_run', session_id: 's1', hosts_updated: 2 })
assert.equal(check.rangeRefresh.s1, 1)
const started = receive(check, {
  kind: 'command_run',
  phase: 'start',
  session_id: 's1',
  command: '/destroy',
  argv: ['dreadgoad', 'destroy'],
  cwd: '/repo',
})
assert.equal(started.command.s1, '/destroy')

const active = consoleEventReducer(started, { type: 'turn_started', sessionId: 's1', now: 3_000 })
assert.equal(active.processing.s1, true)
assert.equal(active.turnStartedAt.s1, 3_000)
const ended = receive(active, { kind: 'agent_end', session_id: 's1', failed: false })
assert.equal(ended.processing.s1, false)
assert.equal(ended.command.s1, '')
assert.equal(ended.turnStartedAt.s1, 0)
assert.equal(ended.verbSeed.s1, 0)

const disconnected = consoleEventReducer(active, { type: 'connection_lost' })
assert.deepEqual(disconnected.processing, {})
assert.deepEqual(disconnected.command, {})
assert.deepEqual(disconnected.turnStartedAt, {})
assert.deepEqual(disconnected.verbSeed, {})
assert.equal(disconnected.messages.s1, active.messages.s1)

const reconnected = receive(disconnected, {
  kind: 'history',
  session_id: 's1',
  events: [],
  active: true,
  started_at: '2026-09-09T12:00:00Z',
  command: '/up',
  approval: null,
})
assert.equal(reconnected.processing.s1, true)
assert.equal(reconnected.command.s1, '/up')
assert.equal(reconnected.turnStartedAt.s1, Date.parse('2026-09-09T12:00:00Z'))

const approval = receive(ended, {
  kind: 'approval_required',
  session_id: 's1',
  ...historyApproval,
})
const serverIgnored = receive(approval, {
  kind: 'approval_resolved',
  session_id: 's1',
  approval_id: 'different',
  command: '/up',
  decision: 'denied',
})
assert.equal(serverIgnored, approval)
const serverResolved = receive(approval, {
  kind: 'approval_resolved',
  session_id: 's1',
  approval_id: 'approval-1',
  command: '/up',
  decision: 'approved',
})
assert.equal(serverResolved.approvals.s1, undefined)
const wrongResolution = consoleEventReducer(approval, {
  type: 'approval_sent', sessionId: 's1', approvalId: 'wrong',
})
assert.equal(wrongResolution, approval)
const resolved = consoleEventReducer(approval, {
  type: 'approval_sent', sessionId: 's1', approvalId: 'approval-1',
})
assert.equal(resolved.approvals.s1, undefined)

const removed = consoleEventReducer(approval, { type: 'session_removed', sessionId: 's1' })
assert.equal(removed.messages.s1, undefined)
assert.equal(removed.rangeRefresh.s1, undefined)
assert.equal(removed.processing.s1, undefined)
assert.equal(removed.command.s1, undefined)
assert.equal(removed.turnStartedAt.s1, undefined)
assert.equal(removed.verbSeed.s1, undefined)
assert.equal(removed.approvals.s1, undefined)
assert.ok(removed.messages.s2, 'other session state is preserved')

const session = (id: string, model: string, region: string): Session => ({
  id,
  label: `local-${id}`,
  model,
  status: 'active',
  anchor: { config_path: '/local/config.yaml', env: 'lab' },
  snapshot: { provider: 'aws', region },
})
const mergedSessions = mergeSessionSnapshots(
  [session('existing', 'new-model', 'old-region'), session('created-locally', 'model', 'local')],
  [
    { ...session('existing', 'stale-model', 'fresh-region'), label: 'stale-label' },
    session('deleted-locally', 'model', 'remote'),
  ],
)
assert.deepEqual(mergedSessions.map(item => item.id), ['existing', 'created-locally'])
assert.equal(mergedSessions[0].model, 'new-model')
assert.equal(mergedSessions[0].label, 'local-existing')
assert.equal(mergedSessions[0].snapshot.region, 'fresh-region')

console.log('PASS console event parsing, restoration, lifecycle, progress, approval, and cleanup contracts')

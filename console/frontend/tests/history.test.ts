// Command-recall semantics, extracted from TerminalChat's key handling so the
// state machine can be exercised without a DOM. The component holds the same
// four pieces of state (history, histIndex, draft, input) and calls the same
// two transitions; this asserts what those transitions do.
import assert from 'node:assert/strict'
import { mergeHistory } from '../src/components/TerminalChat'
import type { ChatEvent } from '../src/types'

const said = (content: string) => ({ kind: 'user_message', content }) as ChatEvent
const noise = () => ({ kind: 'agent_end' }) as ChatEvent

interface Recall {
  history: string[]
  histIndex: number | null
  draft: string
  input: string
}

/** Mirrors recallOlder in TerminalChat. */
function older(s: Recall): boolean {
  if (s.history.length === 0) return false
  if (s.histIndex === null) s.draft = s.input
  const next = s.histIndex === null ? 0 : s.histIndex + 1
  if (next >= s.history.length) return true
  s.histIndex = next
  s.input = s.history[s.history.length - 1 - next]
  return true
}

/** Mirrors recallNewer in TerminalChat. */
function newer(s: Recall): boolean {
  if (s.histIndex === null) return false
  if (s.histIndex === 0) {
    s.histIndex = null
    s.input = s.draft
    return true
  }
  s.histIndex -= 1
  s.input = s.history[s.history.length - 1 - s.histIndex]
  return true
}

const fresh = (input = ''): Recall => ({
  history: ['/instances', '/health', 'check the range'],
  histIndex: null,
  draft: '',
  input,
})

function testUpWalksBackNewestFirst(): void {
  const s = fresh()
  older(s); assert.equal(s.input, 'check the range')
  older(s); assert.equal(s.input, '/health')
  older(s); assert.equal(s.input, '/instances')
  console.log('PASS up walks back, newest first')
}

function testStopsAtOldest(): void {
  const s = fresh()
  older(s); older(s); older(s)
  // A fourth Up must not wrap to the newest entry or blank the box, and must
  // still report handled so the caret doesn't jump instead.
  assert.equal(older(s), true)
  assert.equal(s.input, '/instances')
  assert.equal(s.histIndex, 2)
  console.log('PASS stops at oldest without wrapping')
}

function testDownRestoresHalfTypedDraft(): void {
  const s = fresh('half typed')
  older(s); assert.equal(s.input, 'check the range')
  newer(s)
  assert.equal(s.input, 'half typed', 'the draft must come back')
  assert.equal(s.histIndex, null)
  console.log('PASS down restores the half-typed draft')
}

function testDownIsInertWhenNotBrowsing(): void {
  const s = fresh('typing')
  // Returning false is what lets the caret move normally instead.
  assert.equal(newer(s), false)
  assert.equal(s.input, 'typing')
  console.log('PASS down is inert when not browsing')
}

function testUpIsInertWithEmptyHistory(): void {
  const s: Recall = { history: [], histIndex: null, draft: '', input: 'x' }
  assert.equal(older(s), false)
  assert.equal(s.input, 'x')
  console.log('PASS up is inert with empty history')
}

function testRoundTripReturnsToStart(): void {
  const s = fresh('draft')
  older(s); older(s); older(s)
  newer(s); newer(s); newer(s)
  assert.equal(s.input, 'draft')
  assert.equal(s.histIndex, null)
  console.log('PASS full round trip returns to the draft')
}

// --- mergeHistory: chronological order across both sources ------------------

function testMergeKeepsChronology(): void {
  // /help typed FIRST, before either message was sent. Appending client-only
  // entries to the end made it surface as the newest entry behind Up.
  const messages = [said('/instances'), noise(), said('/health')]
  const merged = mergeHistory(messages, [{ text: '/help', after: 0 }])
  assert.deepEqual(merged, ['/help', '/instances', '/health'])
  console.log('PASS merge keeps /help typed first in first position')
}

function testMergePlacesTrailingEntryLast(): void {
  const messages = [said('/instances')]
  const merged = mergeHistory(messages, [{ text: '/help', after: 1 }])
  assert.deepEqual(merged, ['/instances', '/help'])
  console.log('PASS merge places a trailing /help last')
}

function testMergeIgnoresNonUserEventsAndBlanks(): void {
  const messages = [noise(), said('   '), said('real'), noise()]
  assert.deepEqual(mergeHistory(messages, []), ['real'])
  console.log('PASS merge ignores agent events and blank content')
}

function testMergeSurvivesShorterReplay(): void {
  // A replay can hand back a shorter transcript than `after` was recorded
  // against; the entry must still appear rather than vanish.
  const merged = mergeHistory([said('a')], [{ text: '/help', after: 9 }])
  assert.deepEqual(merged, ['a', '/help'])
  console.log('PASS merge survives a transcript shorter than `after`')
}

function testMergeEmpty(): void {
  assert.deepEqual(mergeHistory([], []), [])
  console.log('PASS merge of nothing is empty')
}

// --- the menu must not steal Up while browsing ------------------------------

function testMenuIsSuppressedWhileBrowsing(): void {
  const commands = ['/exec', '/health', '/help', '/instances', '/up']
  const menuOpen = (input: string, idx: number | null) => {
    const first = input.split(' ')[0]
    const filtered = input.startsWith('/')
      ? commands.filter(c => c.startsWith(first)) : []
    return filtered.length > 0 && !input.includes(' ') && idx === null
  }
  // Recalling "/health" must NOT open the menu, or the next Up is swallowed by
  // it and recall dead-ends after one step.
  assert.equal(menuOpen('/health', 0), false, 'menu must stay shut while browsing')
  // Typing clears the index, and then it behaves as before.
  assert.equal(menuOpen('/health', null), true, 'menu returns once not browsing')
  assert.equal(menuOpen('', null), false, 'empty prompt leaves Up to history')
  console.log('PASS command menu is suppressed while browsing history')
}

testMergeKeepsChronology()
testMergePlacesTrailingEntryLast()
testMergeIgnoresNonUserEventsAndBlanks()
testMergeSurvivesShorterReplay()
testMergeEmpty()
testMenuIsSuppressedWhileBrowsing()

testUpWalksBackNewestFirst()
testStopsAtOldest()
testDownRestoresHalfTypedDraft()
testDownIsInertWhenNotBrowsing()
testUpIsInertWithEmptyHistory()
testRoundTripReturnsToStart()
console.log('ALL PASS')

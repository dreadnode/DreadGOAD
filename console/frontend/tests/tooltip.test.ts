import assert from 'node:assert/strict'
import { placeTooltip } from '../src/components/Tooltip'
import type { Box } from '../src/components/Tooltip'

/** A trigger rect, given left/top/width. The header fields are ~26px tall. */
const at = (left: number, top: number, width = 120, height = 26): Box => ({
  left, top, width, height, right: left + width, bottom: top + height,
})

const VIEW = { width: 1000 }
const BUBBLE = { width: 300, height: 40 }
const OFFSET = 8
const MARGIN = 8

function testCentredAboveWhenThereIsRoom(): void {
  // The ordinary case: mid-pane field with space above it.
  const p = placeTooltip(at(400, 200), BUBBLE, VIEW)
  assert.equal(p.below, false)
  // Centre of trigger is 460; bubble is 300 wide → 460 - 150.
  assert.equal(p.left, 310)
  assert.equal(p.top, 200 - 40 - OFFSET)
  // Arrow sits on the trigger's centre, which is the bubble's centre here.
  assert.equal(p.arrowLeft, 150)
  console.log('PASS centred above when there is room')
}

function testFlipsBelowNearTheTopOfTheWindow(): void {
  // The header row IS at the top of the window — this is the common case here,
  // not an edge case, so a tooltip that only ever draws above would be clipped
  // by the viewport for every field it exists to serve.
  const p = placeTooltip(at(400, 12), BUBBLE, VIEW)
  assert.equal(p.below, true)
  assert.equal(p.top, 12 + 26 + OFFSET, 'must sit under the trigger')
  console.log('PASS flips below near the top of the window')
}

function testClampsAtTheLeftEdgeAndArrowFollows(): void {
  // RANGE label + first field sit hard against the left of the pane.
  const p = placeTooltip(at(4, 200), BUBBLE, VIEW)
  assert.equal(p.left, MARGIN, 'clamped to the margin')
  // The arrow must stay over the trigger, not recentre on the bubble.
  assert.equal(p.arrowLeft, 4 + 60 - MARGIN)
  assert.ok(p.arrowLeft < BUBBLE.width / 2, 'arrow left of bubble centre')
  console.log('PASS clamps at the left edge, arrow follows the trigger')
}

function testClampsAtTheRightEdgeAndArrowFollows(): void {
  // The "checked Nm ago" field is right-aligned at the end of the header.
  const p = placeTooltip(at(940, 200), BUBBLE, VIEW)
  assert.equal(p.left, VIEW.width - BUBBLE.width - MARGIN, 'clamped right')
  assert.equal(p.arrowLeft, 940 + 60 - p.left)
  assert.ok(p.arrowLeft > BUBBLE.width / 2, 'arrow right of bubble centre')
  // And still fully on screen.
  assert.ok(p.left + BUBBLE.width <= VIEW.width - MARGIN)
  console.log('PASS clamps at the right edge, arrow follows the trigger')
}

function testNeverLeavesTheViewportOnANarrowWindow(): void {
  // Bubble wider than the window: the left edge must win, so the start of the
  // text stays readable rather than the bubble hanging off to the left.
  const p = placeTooltip(at(10, 200), { width: 500, height: 40 }, { width: 320 })
  assert.equal(p.left, MARGIN)
  assert.ok(p.left >= 0, 'never negative')
  console.log('PASS never leaves the viewport on a narrow window')
}

function testTallBubbleAlsoFlips(): void {
  // A wrapped 90-char resource group makes a tall bubble; "room above" has to
  // account for its height, not just the trigger's position.
  const tall = { width: 300, height: 120 }
  const p = placeTooltip(at(400, 100), tall, VIEW)
  assert.equal(p.below, true, '100 - 120 - 8 is off-screen, so flip')
  const short = placeTooltip(at(400, 100), { width: 300, height: 40 }, VIEW)
  assert.equal(short.below, false, 'the same trigger fits a short bubble above')
  console.log('PASS a tall bubble flips where a short one does not')
}

testCentredAboveWhenThereIsRoom()
testFlipsBelowNearTheTopOfTheWindow()
testClampsAtTheLeftEdgeAndArrowFollows()
testClampsAtTheRightEdgeAndArrowFollows()
testNeverLeavesTheViewportOnANarrowWindow()
testTallBubbleAlsoFlips()
console.log('ALL PASS')

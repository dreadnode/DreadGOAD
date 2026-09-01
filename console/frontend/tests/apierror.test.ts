import assert from 'node:assert/strict'
import { errorMessage } from '../src/api'

// The message an operator actually saw: a typo'd config path rendered as the
// status code, the JSON envelope, and a Python errno all at once.
function testFastApiDetailIsUnwrapped(): void {
  const body = JSON.stringify({
    detail: "No such file: /Users/m/dev/DreadGOAD/dreadgoad-dreadindex-2.yaml."
      + " The directory /Users/m/dev/DreadGOAD exists — check the filename.",
  })
  const msg = errorMessage(400, body)
  assert.ok(!msg.includes('400'), msg)
  assert.ok(!msg.includes('detail'), msg)
  assert.ok(!msg.includes('{'), msg)
  assert.ok(msg.startsWith('No such file:'), msg)
  console.log('PASS fastapi detail is unwrapped')
}

// FastAPI's request-validation errors put a list of objects in `detail`.
// Stringifying those yields "[object Object]", which tells nobody anything.
function testValidationErrorListIsFlattened(): void {
  const body = JSON.stringify({
    detail: [
      { loc: ['query', 'config_path'], msg: 'field required', type: 'value_error' },
      { loc: ['query', 'env'], msg: 'field required', type: 'value_error' },
    ],
  })
  const msg = errorMessage(422, body)
  assert.equal(msg, 'field required; field required')
  assert.ok(!msg.includes('object Object'), msg)
  console.log('PASS validation error list is flattened')
}

// A proxy or dev server can return HTML or plain text. There is no detail to
// unwrap, so the status is the only signal and must be kept.
function testNonJsonBodyKeepsStatus(): void {
  const msg = errorMessage(502, '<html>Bad Gateway</html>')
  assert.equal(msg, '502: <html>Bad Gateway</html>')
  console.log('PASS non-json body keeps the status')
}

function testEmptyBodyStillSaysSomething(): void {
  // Never render an empty error: a blank red line reads as a rendering fault.
  assert.equal(errorMessage(500, ''), 'request failed (500)')
  assert.equal(errorMessage(500, '   '), 'request failed (500)')
  console.log('PASS empty body still says something')
}

function testJsonWithoutDetailFallsBack(): void {
  assert.equal(errorMessage(400, '{"error":"nope"}'), 'request failed (400)')
  assert.equal(errorMessage(400, '{"detail":"   "}'), 'request failed (400)')
  assert.equal(errorMessage(400, 'null'), 'request failed (400)')
  console.log('PASS json without a usable detail falls back')
}

testFastApiDetailIsUnwrapped()
testValidationErrorListIsFlattened()
testNonJsonBodyKeepsStatus()
testEmptyBodyStillSaysSomething()
testJsonWithoutDetailFallsBack()
console.log('ALL PASS')

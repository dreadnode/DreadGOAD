import assert from 'node:assert/strict'
import {
  authenticatedHeaders,
  authToken,
  parseAuthFragment,
  websocketProtocols,
} from '../src/auth'

function testFragmentParsingPreservesUnrelatedState(): void {
  assert.deepEqual(parseAuthFragment('#token=secret-token&panel=range'), {
    token: 'secret-token',
    remainingHash: '#panel=range',
  })
  assert.deepEqual(parseAuthFragment('#panel=range'), {
    token: null,
    remainingHash: '#panel=range',
  })
  console.log('PASS auth fragment parsing preserves unrelated state')
}

function testBrowserCaptureStoresAndScrubsToken(): void {
  const stored = new Map<string, string>()
  let replacement = ''
  const fakeWindow = {
    location: {
      hash: '#token=launch-secret&panel=range',
      pathname: '/console',
      search: '?view=active',
    },
    sessionStorage: {
      setItem: (key: string, value: string) => stored.set(key, value),
      getItem: (key: string) => stored.get(key) ?? null,
    },
    history: {
      state: null,
      replaceState: (
        _state: unknown,
        _unused: string,
        url?: string | URL | null,
      ) => {
        replacement = String(url)
      },
    },
  }
  Object.defineProperty(globalThis, 'window', {
    value: fakeWindow,
    configurable: true,
  })

  assert.equal(authToken(), 'launch-secret')
  assert.equal([...stored.values()][0], 'launch-secret')
  assert.equal(replacement, '/console?view=active#panel=range')
  assert.ok(!replacement.includes('launch-secret'))
  console.log('PASS browser captures and removes launch token')
}

function testTransportCredentialsAreExact(): void {
  const headers = authenticatedHeaders(
    { 'Content-Type': 'application/json' },
    'abc123',
  )
  assert.equal(headers.get('Authorization'), 'Bearer abc123')
  assert.equal(headers.get('Content-Type'), 'application/json')
  assert.deepEqual(websocketProtocols('abc123'), [
    'dreadgoad.auth',
    'dreadgoad.token.abc123',
  ])
  assert.equal(websocketProtocols(null), undefined)
  console.log('PASS REST and WebSocket credentials are exact')
}

testFragmentParsingPreservesUnrelatedState()
testBrowserCaptureStoresAndScrubsToken()
testTransportCredentialsAreExact()
console.log('ALL PASS')

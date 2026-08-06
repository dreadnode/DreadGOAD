import assert from 'node:assert/strict'
import { LatestLayoutSaver } from '../src/layoutSaveQueue'
import type { RangeLayout } from '../src/types'

interface Deferred<T> {
  promise: Promise<T>
  resolve: (value: T) => void
  reject: (error: unknown) => void
}

function deferred<T>(): Deferred<T> {
  let resolve!: (value: T) => void
  let reject!: (error: unknown) => void
  const promise = new Promise<T>((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
}

const layout = (x: number): RangeLayout => ({ dc01: { x, y: 0 } })
const tick = async () => { await Promise.resolve(); await Promise.resolve() }

async function testLatestPendingLayoutWins(): Promise<void> {
  const pending: Deferred<number>[] = []
  const calls: Array<{ layout: RangeLayout; revision: number }> = []
  const errors: unknown[] = []
  const saver = new LatestLayoutSaver((snapshot, revision) => {
    calls.push({ layout: snapshot, revision })
    const request = deferred<number>()
    pending.push(request)
    return request.promise
  }, error => errors.push(error))

  saver.setRevision(7)
  saver.enqueue(layout(100))
  await tick()
  assert.deepEqual(calls, [{ layout: layout(100), revision: 7 }])

  // These arrive while A is in flight. B is superseded; only C is sent.
  saver.enqueue(layout(200))
  saver.enqueue(layout(300))
  assert.equal(calls.length, 1)
  pending[0].resolve(8)
  await tick()
  assert.deepEqual(calls[1], { layout: layout(300), revision: 8 })
  pending[1].resolve(9)
  await saver.whenIdle()
  assert.equal(calls.length, 2)
  assert.deepEqual(errors, [])
  console.log('PASS latest pending layout wins with sequential revisions')
}

async function testFailureDropsUnsafePendingWrite(): Promise<void> {
  const first = deferred<number>()
  const calls: RangeLayout[] = []
  const errors: unknown[] = []
  const saver = new LatestLayoutSaver((snapshot) => {
    calls.push(snapshot)
    return first.promise
  }, error => errors.push(error))

  saver.enqueue(layout(1))
  await tick()
  saver.enqueue(layout(2))
  first.reject(new Error('409 stale revision'))
  await saver.whenIdle()
  assert.equal(calls.length, 1, 'pending write must not retry against an unknown revision')
  assert.equal(errors.length, 1)
  console.log('PASS failed save drops pending work and requests authoritative reload')
}

await testLatestPendingLayoutWins()
await testFailureDropsUnsafePendingWrite()

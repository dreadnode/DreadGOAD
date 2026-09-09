import assert from 'node:assert/strict'
import { createElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server.browser'
import {
  ConnectRequest,
  DetailRequest,
  HostNode,
  buildNodes,
  describeChecked,
} from '../src/components/RangeView'
import RangeTable from '../src/components/RangeTable'
import type { RangeDoc, RangeHost } from '../src/types'

const host = (id: string, role: string, status = 'running'): RangeHost => ({
  id,
  hostname: id,
  role,
  source: 'infra',
  status,
  health: 'unknown',
})

const range: RangeDoc = {
  session_id: 'session-1',
  hosts: [host('member-1', 'member'), host('attackbox', 'attackbox'), host('dc-1', 'dc')],
  edges: [],
  layout: { 'dc-1': { x: 17, y: 23 } },
}

const nodes = buildNodes(range)
assert.deepEqual(nodes.find(node => node.id === 'dc-1')?.position, { x: 17, y: 23 })
assert.ok(
  (nodes.find(node => node.id === 'attackbox')?.position.y ?? Infinity)
    < (nodes.find(node => node.id === 'member-1')?.position.y ?? -Infinity),
  'default layout keeps ingress above members',
)

assert.equal(describeChecked(undefined, 1_000)?.label, 'never checked')
assert.equal(describeChecked('invalid', 1_000), null)
assert.equal(describeChecked(new Date(2_000).toISOString(), 1_000)?.label, 'updated just now')

const attackbox = {
  ...host('attackbox', 'attackbox'),
  cloud_name: 'range-kali',
  ip_private: '10.0.0.4',
}
const hostNodeProps = { data: attackbox } as unknown as Parameters<typeof HostNode>[0]
const hostMarkup = renderToStaticMarkup(
  createElement(ConnectRequest.Provider, { value: () => {} },
    createElement(DetailRequest.Provider, { value: () => {} },
      createElement(HostNode, hostNodeProps))),
)
assert.ok(hostMarkup.includes('>details</button>'))
assert.ok(hostMarkup.includes('>connect</button>'))
assert.ok(hostMarkup.includes('range-kali'))

const tableMarkup = renderToStaticMarkup(createElement(RangeTable, {
  range,
  sessionId: 'session-1',
}))
assert.ok(tableMarkup.indexOf('attackbox') < tableMarkup.indexOf('dc-1'))
assert.ok(tableMarkup.indexOf('dc-1') < tableMarkup.indexOf('member-1'))

console.log('PASS extracted range presentation, host node, and table contracts')

import { createElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server.browser'
import VMResourceDetails from '../src/components/VMResourceDetails'

function assert(condition: boolean, label: string) {
  if (!condition) throw new Error(label)
}

const subnetId = '/subscriptions/demo/resourceGroups/range/providers/Microsoft.Network/virtualNetworks/vnet/subnets/internal'
const populated = {
  disks: [{ name: 'osdisk', role: 'os', size_gb: 128, storage_type: 'Premium_LRS', caching: 'ReadWrite' }],
  nics: [{
    id: 'nic-1',
    name: 'eth0',
    private_ips: ['10.0.0.4'],
    subnet_id: subnetId,
    primary: true,
    accelerated_networking: true,
  }],
}

const compactMarkup = renderToStaticMarkup(createElement(VMResourceDetails, {
  detail: populated,
  variant: 'accordion',
}))
assert(compactMarkup.includes('Disks (1)'), 'compact view renders disk count')
assert(compactMarkup.includes('128 GiB'), 'compact view renders disk details')
assert(compactMarkup.includes('Network interfaces (1)'), 'compact view renders NIC count')
assert(compactMarkup.includes('10.0.0.4'), 'compact view renders NIC details')
assert(!compactMarkup.includes(`title="${subnetId}"`), 'compact view does not add full-ID tooltips')

const fullMarkup = renderToStaticMarkup(createElement(VMResourceDetails, {
  detail: populated,
  variant: 'modal',
}))
assert(fullMarkup.includes(`title="${subnetId}"`), 'full view preserves full-ID tooltip')

const emptyCompactMarkup = renderToStaticMarkup(createElement(VMResourceDetails, {
  detail: { disks: [], nics: [] },
  variant: 'accordion',
}))
assert(emptyCompactMarkup === '', 'compact view hides empty resource sections')

const emptyFullMarkup = renderToStaticMarkup(createElement(VMResourceDetails, {
  detail: { disks: [], nics: [] },
  variant: 'modal',
}))
assert(emptyFullMarkup.includes('Disks (0)'), 'full view renders empty disk section')
assert(emptyFullMarkup.includes('Network interfaces (0)'), 'full view renders empty NIC section')
assert((emptyFullMarkup.match(/None attached\./g) || []).length === 2, 'full view labels both empty sections')

const missingPrivateIpsMarkup = renderToStaticMarkup(createElement(VMResourceDetails, {
  detail: {
    nics: [{ id: 'legacy-nic', name: 'legacy-nic', private_ips: undefined as unknown as string[] }],
  },
  variant: 'accordion',
}))
assert(missingPrivateIpsMarkup.includes('legacy-nic'), 'legacy NIC without private_ips still renders')

console.log('PASS shared VM resource detail rendering')

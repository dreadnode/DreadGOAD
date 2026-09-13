import assert from 'node:assert/strict'
import { deriveNewSessionModel, type NewSessionModelInput } from '../src/newSessionModel'
import type { SessionOptions } from '../src/api'

const options: SessionOptions = {
  providers: ['aws', 'azure'],
  credential_hints: { aws: null, azure: 'az login' },
  regions: { aws: ['us-west-1'], azure: ['centralus'] },
  environments: [{
    name: 'scope-dev', lab: 'SCOPE-RANGE', provider: 'azure',
    deployment: 'scope-range-deployment', region: 'centralus', variant: false,
    config_path: '/repo/dreadgoad.yaml',
  }],
  ranges: [
    {
      name: 'GOAD', display_name: 'GOAD', dir: 'ad/GOAD', kind: 'active-directory',
      providers: ['aws', 'azure'], hosts: ['dc01'], variant_supported: true,
      generated: false,
      provider_settings: {
        aws: {
          deployment: 'goad-deployment', scaffold_profile: 'active-directory',
          template_environment: 'staging', default_region: 'us-west-1',
          network: { editable: true },
        },
        azure: {
          deployment: 'goad-deployment', scaffold_profile: 'active-directory',
          template_environment: 'test', default_region: 'centralus',
          network: { editable: true },
        },
      },
    },
    {
      name: 'SCOPE-RANGE', display_name: 'GOAT', dir: 'ad/SCOPE-RANGE',
      kind: 'service-range', providers: ['azure'], hosts: ['web01'],
      variant_supported: false, generated: false,
      provider_settings: {
        azure: {
          deployment: 'scope-range-deployment', scaffold_profile: 'template',
          template_environment: 'scope-dev', default_region: 'centralus',
          network: { cidr: '10.50.0.0/16', editable: false },
        },
      },
    },
  ],
}

const base: NewSessionModelInput = {
  options, mode: 'new', existingIndex: '', importPath: '', importEnvironment: '',
  importReady: false, rangeName: '', provider: '', region: '',
  environmentName: '', customization: 'standard', cidr: '', submitting: false,
}

const empty = deriveNewSessionModel(base)
assert.equal(empty.valid, false)
assert.equal(empty.effectiveProvider, '', 'AWS must not be selected before a range')

const scope = deriveNewSessionModel({
  ...base, rangeName: 'SCOPE-RANGE', environmentName: 'kraken',
})
assert.equal(scope.valid, true)
assert.equal(scope.effectiveProvider, 'azure')
assert.equal(scope.variantAvailable, false)
assert.equal(scope.cidrEditable, false)
assert.deepEqual(scope.payload, {
  mode: 'create_range', range: 'SCOPE-RANGE', provider: 'azure',
  region: 'centralus', env: 'kraken', customization: 'standard',
  vpc_cidr: '10.50.0.0/16',
})

const goadNeedsProvider = deriveNewSessionModel({
  ...base, rangeName: 'GOAD', environmentName: 'redteam',
})
assert.equal(goadNeedsProvider.valid, false)
assert.equal(goadNeedsProvider.effectiveProvider, '')

const variant = deriveNewSessionModel({
  ...base, rangeName: 'GOAD', provider: 'aws', environmentName: 'redteam',
  customization: 'randomized',
})
assert.equal(variant.valid, true)
assert.equal(variant.effectiveRegion, 'us-west-1')
assert.equal(variant.payload.customization, 'randomized')

const existing = deriveNewSessionModel({
  ...base, mode: 'existing', existingIndex: '0',
})
assert.equal(existing.valid, true)
assert.deepEqual(existing.payload, {
  config_path: '/repo/dreadgoad.yaml', env: 'scope-dev',
})

const imported = deriveNewSessionModel({
  ...base, mode: 'import', importPath: '/other/dreadgoad.yaml',
  importEnvironment: 'prod', importReady: true,
})
assert.equal(imported.valid, true)
assert.deepEqual(imported.payload, {
  config_path: '/other/dreadgoad.yaml', env: 'prod',
})

assert.equal(deriveNewSessionModel({
  ...base, mode: 'import', importPath: '/other/dreadgoad.yaml',
  importEnvironment: 'prod', importReady: false,
}).valid, false)

console.log('newSessionModel tests passed')

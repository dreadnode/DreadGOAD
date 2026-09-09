import assert from 'node:assert/strict'
import type { AppConfig, ConfigListing, LabSummary } from '../src/api'
import {
  NEW_CONFIG,
  NEW_ENV,
  deriveNewSessionModel,
  slugForConfig,
  type NewSessionModelInput,
} from '../src/newSessionModel'

const cfg: AppConfig = {
  version: 'test',
  default_model: 'test',
  default_config_path: '/repo/dreadgoad.yaml',
  api_key_set: true,
  providers: ['aws', 'azure'],
}

const listing: ConfigListing = {
  configs_root: '/managed',
  providers: ['aws', 'azure'],
  credential_hints: { aws: 'AWS credentials are unavailable', azure: null },
  configs: [{
    path: '/repo/dreadgoad.yaml',
    name: 'dreadgoad.yaml',
    source: 'default',
    provider: 'aws',
    environments: ['existing'],
  }],
}

const base: NewSessionModelInput = {
  cfg,
  listing,
  choice: '/repo/dreadgoad.yaml',
  customPath: '',
  newConfigName: '',
  provider: 'aws',
  region: '',
  envs: ['existing'],
  configOk: true,
  envChoice: 'existing',
  loadedProvider: 'aws',
  loadedRegions: { existing: 'us-east-1' },
  newEnv: '',
  source: 'ad/GOAD',
  labList: [],
  variantName: '',
  cidr: '10.100.0.0/16',
  submitting: false,
}

assert.equal(slugForConfig('  My Azure_Lab!  '), 'my-azure-lab')
assert.equal(slugForConfig('x'.repeat(60)).length, 48)

const attach = deriveNewSessionModel(base)
assert.equal(attach.valid, true)
assert.deepEqual(attach.payload, { config_path: '/repo/dreadgoad.yaml', env: 'existing' })
assert.ok(attach.effects.some(effect => effect.includes('no files are written')))
assert.equal(attach.missingRegion, false)
assert.equal(attach.credentialHint, 'AWS credentials are unavailable')

const newEnvironment = deriveNewSessionModel({
  ...base,
  envChoice: NEW_ENV,
  newEnv: ' redteam ',
  variantName: '',
  cidr: ' ',
})
assert.equal(newEnvironment.valid, true)
assert.deepEqual(newEnvironment.payload, {
  mode: 'new',
  config_path: '/repo/dreadgoad.yaml',
  env: 'redteam',
  env_fields: {
    variant: true,
    variant_source: 'ad/GOAD',
    variant_target: 'ad/GOAD-redteam',
    variant_name: 'redteam',
    vpc_cidr: '10.100.0.0/16',
  },
})
assert.ok(newEnvironment.effects.some(effect => effect.includes('redteam/<region>/')))

const unsupportedLab: LabSummary = {
  name: 'Azure-only',
  dir: 'ad/AzureOnly',
  providers: ['azure'],
  hosts: ['dc'],
  generated: false,
}
const rejectedSource = deriveNewSessionModel({
  ...base,
  envChoice: NEW_ENV,
  newEnv: 'redteam',
  source: unsupportedLab.dir,
  labList: [unsupportedLab],
})
assert.equal(rejectedSource.sourceUnsupported, true)
assert.equal(rejectedSource.valid, false)

const newConfig = deriveNewSessionModel({
  ...base,
  choice: NEW_CONFIG,
  newConfigName: ' Azure Range ',
  provider: 'azure',
  region: ' eastus ',
  newEnv: 'blue',
  source: 'ad/GOAD-Light',
  variantName: 'demo',
})
assert.equal(newConfig.valid, true)
assert.deepEqual(newConfig.payload, {
  mode: 'new_config',
  config_name: 'Azure Range',
  provider: 'azure',
  region: 'eastus',
  env: 'blue',
  env_fields: {
    variant: true,
    variant_source: 'ad/GOAD-Light',
    variant_target: 'ad/GOAD-Light-demo',
    variant_name: 'demo',
    vpc_cidr: '10.100.0.0/16',
  },
})
assert.equal(newConfig.effects[0], 'create /managed/azure-range.yaml (provider azure, region eastus)')

const duplicateConfig = deriveNewSessionModel({
  ...base,
  choice: NEW_CONFIG,
  newConfigName: 'DreadGOAD',
  region: 'us-east-1',
  newEnv: 'new-env',
})
assert.equal(duplicateConfig.configTaken, true)
assert.equal(duplicateConfig.valid, false)

const incompleteExisting = deriveNewSessionModel({
  ...base,
  loadedRegions: {},
})
assert.equal(incompleteExisting.missingRegion, true)
assert.equal(incompleteExisting.valid, true)

console.log('PASS new-session derivation, validation, payload, and effect contracts')

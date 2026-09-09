import assert from 'node:assert/strict'
import { BASTION_LOCAL_PORT, buildConnectPlan } from '../src/connect'
import type { RangeHost, Session } from '../src/types'

// Shaped from a real Azure session + its attack-box host record, so the
// expected strings below are the ones an operator would actually be handed.
const azureSession = (): Session => ({
  id: 's-1',
  label: 'dreadindex2',
  status: 'ready',
  anchor: { config_path: '/dreadgoad.yaml', env: 'dreadindex2' },
  snapshot: {
    provider: 'azure',
    region: 'centralus',
    account: '70a9c8a4-6bc6-4a48-ae24-27996cea8c02',
    group: 'DREADINDEX2-DREADGOAD-RG',
    attack_box: '/subscriptions/70a9c8a4/…/dreadindex2-dreadgoad-kali-vm',
    azure: { ssh_user: 'kali', ssh_key: null },
  },
})

const kali = (): RangeHost => ({
  id: 'attackbox',
  hostname: 'attackbox',
  role: 'attackbox',
  source: 'infra',
  status: 'running',
  health: 'unknown',
  ip_private: '10.2.4.4',
  cloud_name: 'dreadindex2-dreadgoad-kali-vm',
  cloud_id: '/subscriptions/70a9c8a4/resourceGroups/DREADINDEX2-DREADGOAD-RG'
    + '/providers/Microsoft.Compute/virtualMachines/dreadindex2-dreadgoad-kali-vm',
})

function testAzureCommands(): void {
  const plan = buildConnectPlan(azureSession(), kali())
  assert.equal(plan.kind, 'azure-bastion')
  if (plan.kind !== 'azure-bastion') return

  assert.equal(
    plan.tunnel,
    'az network bastion tunnel --name dreadindex2-dreadgoad-bastion'
    + ' --resource-group DREADINDEX2-DREADGOAD-RG'
    + ' --target-resource-id /subscriptions/70a9c8a4/resourceGroups'
    + '/DREADINDEX2-DREADGOAD-RG/providers/Microsoft.Compute/virtualMachines'
    + '/dreadindex2-dreadgoad-kali-vm'
    + ` --resource-port 22 --port ${BASTION_LOCAL_PORT}`,
  )
  assert.equal(
    plan.ssh,
    '~/.dreadgoad/keys/azure-dreadindex2-dreadgoad-kali'
      .replace(/^/, `ssh -i `)
    + ` -p ${BASTION_LOCAL_PORT} kali@127.0.0.1`,
  )
  // Nothing may reach the clipboard with a hole in it.
  assert.ok(!JSON.stringify(plan).includes('undefined'))
  console.log('PASS azure commands')
}

/** An AWS range: no resource group, EC2 instance id instead of an ARM id. */
const awsSession = (): Session => {
  const s = azureSession()
  s.snapshot.provider = 'aws'
  s.snapshot.region = 'us-east-2'
  s.snapshot.account = '123456789012'
  delete s.snapshot.group
  delete s.snapshot.azure
  delete s.snapshot.attack_box
  s.snapshot.aws = { profile: null }
  return s
}

const awsKali = (): RangeHost => ({
  ...kali(),
  cloud_name: 'dreadgoad-kali',
  cloud_id: 'i-0abc123def4567890',
})

// The command shape is the one AWSProvider.StartInteractiveShell already shells
// out to (cli/internal/aws/provider.go:158), region included.
function testAwsSsmSession(): void {
  const plan = buildConnectPlan(awsSession(), awsKali())
  assert.equal(plan.kind, 'aws-ssm')
  if (plan.kind !== 'aws-ssm') return
  assert.equal(
    plan.session,
    'aws ssm start-session --target i-0abc123def4567890 --region us-east-2',
  )
  assert.ok(!plan.session.includes('undefined'))
  console.log('PASS aws builds an ssm start-session command')
}

// Today's state: AWS provisions no attack box, so nothing is discovered. This
// must NOT say "run /instances" — that advice can never succeed. It must also
// start working on its own once an attack box appears, with no code change.
function testAwsWithNoAttackBoxDiscovered(): void {
  const host = { ...kali(), cloud_id: null, cloud_name: null }
  const plan = buildConnectPlan(awsSession(), host)
  assert.equal(plan.kind, 'no-attack-box')
  if (plan.kind === 'no-attack-box') assert.equal(plan.provider, 'aws')
  console.log('PASS aws with no attack box says so, not "run /instances"')
}

// The same host once the infrastructure lands and the hook discovers it: the
// only thing that changed is cloud_id, and commands appear.
function testAwsLightsUpWhenCloudIdAppears(): void {
  const before = buildConnectPlan(awsSession(), { ...kali(), cloud_id: null })
  const after = buildConnectPlan(awsSession(), awsKali())
  assert.equal(before.kind, 'no-attack-box')
  assert.equal(after.kind, 'aws-ssm')
  console.log('PASS aws lights up on cloud_id alone, no other change needed')
}

// region lives at the top of dreadgoad.yaml and is never learned from a read,
// so a missing one must not block — the AWS CLI's configured region is the
// right fallback, and "run /instances" would be advice that cannot help.
function testAwsWithoutRegionStillBuilds(): void {
  const s = awsSession()
  delete s.snapshot.region
  const plan = buildConnectPlan(s, awsKali())
  assert.equal(plan.kind, 'aws-ssm')
  if (plan.kind !== 'aws-ssm') return
  assert.equal(plan.session, 'aws ssm start-session --target i-0abc123def4567890')
  assert.ok(!plan.session.includes('--region'))
  assert.ok(!plan.session.includes('undefined'))
  console.log('PASS aws without a configured region still builds a command')
}

function testAwsBeforeFirstReadIsIncomplete(): void {
  // Never read: no account, no cloud id. Reading it genuinely does help here.
  const s = awsSession()
  delete s.snapshot.account
  const plan = buildConnectPlan(s, { ...kali(), cloud_id: null })
  assert.equal(plan.kind, 'incomplete')
  console.log('PASS aws before the first read is incomplete')
}

function testUnknownProviderIsUnsupported(): void {
  const s = azureSession()
  s.snapshot.provider = 'proxmox'
  const plan = buildConnectPlan(s, kali())
  assert.equal(plan.kind, 'unsupported-provider')
  if (plan.kind === 'unsupported-provider') assert.equal(plan.provider, 'proxmox')
  console.log('PASS unknown provider is unsupported')
}

// Azure's Kali box is optional (--with-kali), so the same "read it, found none"
// case exists there and must not be reported as a missing read either.
function testAzureWithoutKaliSaysNoAttackBox(): void {
  const host = { ...kali(), cloud_id: null }
  const s = azureSession()
  delete s.snapshot.attack_box
  const plan = buildConnectPlan(s, host)
  assert.equal(plan.kind, 'no-attack-box')
  if (plan.kind === 'no-attack-box') assert.equal(plan.provider, 'azure')
  console.log('PASS azure without --with-kali says no attack box')
}

function testIncompleteBeforeFirstRead(): void {
  // Azure, but the ingestion hook has not run at all: no account, no group, no
  // cloud id. `account` is the marker — without it nothing has been read, so
  // "run /instances" is the right advice here and only here.
  const session = azureSession()
  delete session.snapshot.account
  delete session.snapshot.group
  delete session.snapshot.attack_box
  const host = kali()
  delete host.cloud_id

  assert.equal(buildConnectPlan(session, host).kind, 'incomplete')
  console.log('PASS incomplete before first read')
}

function testOffPatternVmNameRefuses(): void {
  // A VM name that does not fit "{env}-{deployment}-kali-vm" must refuse rather
  // than build a plausible-looking key path from a partial match.
  for (const name of ['someone-elses-vm', 'dreadindex2-kali-vm', 'kali-vm']) {
    const host = { ...kali(), cloud_name: name }
    assert.equal(
      buildConnectPlan(azureSession(), host).kind,
      'incomplete',
      `expected refusal for ${name}`,
    )
  }
  console.log('PASS off-pattern vm names refuse')
}

function testMissingSessionRefuses(): void {
  assert.equal(buildConnectPlan(undefined, kali()).kind, 'incomplete')
  console.log('PASS missing session refuses')
}

function testCustomPortReachesBothCommands(): void {
  const plan = buildConnectPlan(azureSession(), kali(), 2299)
  if (plan.kind !== 'azure-bastion') throw new Error('expected commands')
  assert.ok(plan.tunnel.endsWith('--port 2299'))
  assert.ok(plan.ssh.includes('-p 2299'))
  console.log('PASS custom port reaches both commands')
}

testAzureCommands()
testCustomPortReachesBothCommands()
testOffPatternVmNameRefuses()
testAzureWithoutKaliSaysNoAttackBox()

testAwsSsmSession()
testAwsWithoutRegionStillBuilds()
testAwsWithNoAttackBoxDiscovered()
testAwsLightsUpWhenCloudIdAppears()
testAwsBeforeFirstReadIsIncomplete()

testUnknownProviderIsUnsupported()
testIncompleteBeforeFirstRead()
testMissingSessionRefuses()
console.log('ALL PASS')

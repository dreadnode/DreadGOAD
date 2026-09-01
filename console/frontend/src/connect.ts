// The two shell commands that get an operator an SSH session on the attack box:
// an Azure Bastion tunnel, then ssh through it. The console does not run them —
// it renders them to copy, so this file only assembles strings.
//
// Every value is derived from the session the console already holds, EXCEPT the
// two naming conventions below. Those mirror the Go CLI, and the mirror is the
// risk: if the terraform modules ever rename, these go stale silently and the
// copied command fails against a range that is perfectly healthy.
//
//   bastion name  {env}-{deployment}-bastion
//   ssh key path  ~/.dreadgoad/keys/azure-{env}-{deployment}-kali
//                 — cli/internal/azure/kali.go:15 (KaliKeyPath)
//
// `deployment` is recovered from the VM name exactly the way KaliKeyPath does
// it, by stripping the env prefix and the role suffix, so the two derivations
// agree or fail together rather than disagreeing quietly.
import type { RangeHost, Session } from './types'

/** Local port the tunnel listens on; ssh then targets 127.0.0.1 there. */
export const BASTION_LOCAL_PORT = 2223

/**
 * What the modal can offer for this host.
 *
 * More outcomes than "commands or not", because "no commands" has several very
 * different causes and each needs different words. A range that has not been
 * read yet is fixed by reading it; a range with no attack box deployed is fixed
 * by deploying one; an unrecognised provider is fixed by neither. Collapsing
 * them into one "not enough info — run /instances" sent an operator round a
 * loop that could not terminate.
 */
export type ConnectPlan =
  | {
    kind: 'azure-bastion'
    /** Opens the Bastion tunnel. Long-running — it holds the terminal. */
    tunnel: string
    /** Connects through the tunnel. Needs a second terminal. */
    ssh: string
    /** Path the ssh command expects the private key at. */
    keyPath: string
  }
  | {
    kind: 'aws-ssm'
    /** One interactive SSM session. No tunnel, no key, no local port. */
    session: string
  }
  /** The range has been read, and it has no attack box in it. */
  | { kind: 'no-attack-box'; provider: string }
  /** Neither Azure nor AWS — no recipe here. */
  | { kind: 'unsupported-provider'; provider: string }
  /** The post-deploy placement is not known yet; reading the range fixes it. */
  | { kind: 'incomplete' }

/**
 * Recover the deployment segment from a provider VM name.
 *
 * Mirrors KaliKeyPath's `TrimSuffix(TrimPrefix(name, env+"-"), suffix)`:
 * `dreadindex2-dreadgoad-kali-vm` with env `dreadindex2` yields `dreadgoad`.
 * Returns `null` when the name does not fit the pattern, rather than a partial
 * string that would build a wrong-looking-but-plausible path.
 */
function deploymentOf(vmName: string, env: string, suffix: string): string | null {
  const prefix = `${env}-`
  if (!vmName.startsWith(prefix) || !vmName.endsWith(suffix)) return null
  const deployment = vmName.slice(prefix.length, vmName.length - suffix.length)
  return deployment === '' ? null : deployment
}

/**
 * Decide what can be offered for `host`.
 *
 * The `incomplete` outcome matters more than it looks: the values it is missing
 * are the ones learned post-deploy (resource group, cloud id), so a range that
 * has not been read yet would otherwise render a command with `undefined` in it.
 */
export function buildConnectPlan(
  session: Session | undefined,
  host: RangeHost,
  localPort: number = BASTION_LOCAL_PORT,
): ConnectPlan {
  if (!session) return { kind: 'incomplete' }
  const snap = session.snapshot ?? {}
  const provider = snap.provider || 'unknown'

  // Provider first, and before any of the value checks below. On AWS `group` is
  // never populated — it is an Azure resource group, and parse_cloud_account
  // fills it only from an instance `group` field or an ARM resource id, neither
  // of which AWS produces. Checked later, an AWS range would fall through to
  // `incomplete` and be told to run /instances, which can never help it.
  if (provider !== 'azure' && provider !== 'aws') {
    return { kind: 'unsupported-provider', provider }
  }

  const env = session.anchor?.env
  // Prefer the host's own id over the snapshot's attack_box: they are the same
  // VM here, but the host record is what the range view is actually showing.
  const targetId = host.cloud_id || snap.attack_box
  if (!targetId) {
    // `account` is learned by the ingestion hook on the first read, so it
    // separates "nobody has looked yet" from "we looked and there is no attack
    // box in this range". Both lack a target id; only one is fixed by looking.
    return snap.account
      ? { kind: 'no-attack-box', provider }
      : { kind: 'incomplete' }
  }

  // AWS reaches its hosts through SSM: one interactive session, addressed by
  // EC2 instance id, with no tunnel, key or local port to arrange. The command
  // is the one the CLI already shells out to in AWSProvider.StartInteractiveShell
  // (cli/internal/aws/provider.go:158), region included.
  //
  // --region is appended only when known, never required. It comes from the top
  // of dreadgoad.yaml and is NEVER learned from reading the range, so refusing
  // without it would tell an operator to run /instances to fix something
  // /instances cannot touch — the same dead end the provider check above
  // exists to avoid. Omitted, the AWS CLI falls back to its configured region,
  // which is the right answer whenever the config left it unset.
  if (provider === 'aws') {
    const region = snap.region ? ` --region ${snap.region}` : ''
    return {
      kind: 'aws-ssm',
      session: `aws ssm start-session --target ${targetId}${region}`,
    }
  }

  const group = snap.group
  if (!env || !group || !host.cloud_name) return { kind: 'incomplete' }

  const deployment = deploymentOf(host.cloud_name, env, '-kali-vm')
  if (!deployment) return { kind: 'incomplete' }

  const user = snap.azure?.ssh_user || 'kali'
  const keyPath = `~/.dreadgoad/keys/azure-${env}-${deployment}-kali`

  return {
    kind: 'azure-bastion',
    tunnel: [
      'az network bastion tunnel',
      `--name ${env}-${deployment}-bastion`,
      `--resource-group ${group}`,
      `--target-resource-id ${targetId}`,
      '--resource-port 22',
      `--port ${localPort}`,
    ].join(' '),
    ssh: `ssh -i ${keyPath} -p ${localPort} ${user}@127.0.0.1`,
    keyPath,
  }
}

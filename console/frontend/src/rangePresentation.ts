import type { Node } from '@xyflow/react'
import type { RangeDoc, RangeHost } from './types'

export const ROLE_ICON: Record<string, string> = {
  dc: '🌐', member: '🖥️', workstation: '💻', bastion: '🛡️',
  attackbox: '☠️', linux: '🐧', other: '❔',
}

// These semantic colours clear AA on --dn-surface; the neutral UI tokens do not.
export const STATUS_COLOR: Record<string, string> = {
  running: 'var(--dn-success)',
  stopped: 'var(--dg-node-value)',
  provisioning: 'var(--dn-warning)',
  absent: 'var(--dn-error)',
  unknown: 'var(--dg-node-label)',
}

export const HEALTH_COLOR: Record<string, string> = {
  healthy: 'var(--dn-success)',
  unhealthy: 'var(--dn-warning)',
  unknown: 'var(--dg-node-label)',
}

export const HEALTH_TITLE: Record<string, string> = {
  healthy: 'healthy — every /health check on this host passed',
  unhealthy: 'unhealthy — at least one /health check on this host failed; '
    + 'run /health for the per-check detail',
  unknown: 'unknown — /health has not run since this range was read',
}

export const isManagedService = (host: RangeHost) => host.role === 'bastion'

const TIER: Record<string, number> = {
  bastion: 0,
  attackbox: 0,
  dc: 1,
  member: 2,
  workstation: 2,
  linux: 3,
  other: 3,
}

// Border-box bounds for HostNode. Re-measure if it gains a row or allows wrapping.
export const NODE_MAX_W = 236
export const NODE_MAX_H = 200
export const NODE_GUTTER = 28
const COL_W = NODE_MAX_W + NODE_GUTTER
const TIER_Y = NODE_MAX_H + NODE_GUTTER

/** Lay hosts out by access tier; a saved position always wins. */
export function buildNodes(range: RangeDoc): Node[] {
  const tiers = new Map<number, RangeHost[]>()
  for (const host of range.hosts) {
    const tier = TIER[host.role] ?? 3
    if (!tiers.has(tier)) tiers.set(tier, [])
    tiers.get(tier)!.push(host)
  }

  const widest = Math.max(...[...tiers.values()].map(hosts => hosts.length), 1)
  const nodes: Node[] = []
  for (const [tier, hosts] of [...tiers.entries()].sort((a, b) => a[0] - b[0])) {
    const offset = ((widest - hosts.length) * COL_W) / 2
    hosts.forEach((host, index) => {
      nodes.push({
        id: host.id,
        type: 'host',
        position: range.layout?.[host.id] ?? {
          x: offset + index * COL_W,
          y: tier * TIER_Y,
        },
        data: host as unknown as Record<string, unknown>,
      })
    })
  }
  return nodes
}

export interface CheckedLabel {
  label: string
  stale: boolean
  exact: string
}

const STALE_AFTER_MS = 5 * 60_000

/** Describe when range state was last refreshed. */
export function describeChecked(
  iso: string | null | undefined,
  now: number,
): CheckedLabel | null {
  if (!iso) {
    return { label: 'never checked', stale: true, exact: 'No command has run yet' }
  }
  const at = new Date(iso).getTime()
  if (Number.isNaN(at)) return null
  const age = Math.max(now - at, 0)
  return {
    label: `updated ${formatAge(age)}`,
    stale: age > STALE_AFTER_MS,
    exact: new Date(iso).toLocaleString(),
  }
}

function formatAge(ms: number): string {
  const secs = Math.floor(ms / 1000)
  if (secs < 45) return 'just now'
  const mins = Math.floor(secs / 60)
  if (mins < 60) return `${Math.max(mins, 1)}m ago`
  const hours = Math.floor(mins / 60)
  if (hours < 24) return `${hours}h ago`
  return `${Math.floor(hours / 24)}d ago`
}

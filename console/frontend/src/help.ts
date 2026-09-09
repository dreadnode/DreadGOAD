// The operator's guide: what a range run looks like end to end.
//
// Rendered in two places from this one source — the empty chat pane when a
// session has no history yet, and the `/help` command. They must never drift,
// which is why the content lives here rather than inline in either.

import type { CommandDef } from './api'

/** One phase of the range lifecycle, in the order an operator meets it. */
export interface WorkflowPhase {
  title: string
  /** Commands central to this phase, in the order you'd reach for them. */
  commands: string[]
  /** What the phase is for, and the thing people get wrong. One or two lines. */
  detail: string
}

// Ordered deliberately: this is a cycle, not a list. Score → scrub → reset
// returns you to step 3 for the next agent run without redeploying.
export const WORKFLOW: WorkflowPhase[] = [
  {
    title: '1. Deploy the range',
    commands: ['/variant', '/up', '/extensions'],
    detail:
      '/variant first if this engagement needs fresh names and passwords — it ' +
      'rewrites the answer key, so never run it against a range already deployed. ' +
      '/up then builds and provisions end to end; it runs for tens of minutes and ' +
      'starts billing. /extensions adds optional machines (ELK, Wazuh, …).',
  },
  {
    title: '2. Confirm it came up',
    commands: ['/status', '/instances', '/health', '/secure'],
    detail:
      '/status runs /instances then /health in one pass. Use them separately when ' +
      'you only need one: /instances is the cloud view (power state, IPs), ' +
      '/health is the truth (AD, DNS, replication checks). /secure audits network ' +
      'security posture (NSGs, public IPs, bastion).',
  },
  {
    title: '3. Validate the lab content',
    commands: ['/validate'],
    detail:
      'Checks the intentional vulnerabilities are actually in place. ' +
      'A failure means a vulnerability is MISSING — the lab is under-broken, not broken.',
  },
  {
    title: '4. Fix what is wrong',
    commands: ['/exec', '/restart', '/provision'],
    detail:
      '/exec runs a script on a named host through the cloud control plane, so it ' +
      'reaches a host whose WinRM is down. /restart reboots one host. /provision ' +
      're-runs the playbooks across the range.',
  },
  {
    title: '5. Score an agent run',
    commands: ['/score'],
    detail:
      "Grades an attacking agent's report against the answer key. " +
      'Give it the report path on the attack box.',
  },
  {
    title: '6. Reset for the next run',
    commands: ['/scrub', '/reset'],
    detail:
      '/scrub deletes the agent artifacts left on the attack box and Windows hosts — ' +
      'it APPLIES by default here, so pass "dry" to preview. /reset restores the AD ' +
      'baseline when a run has changed the directory itself.',
  },
  {
    title: '7. Park it or tear it down',
    commands: ['/stop', '/start', '/destroy'],
    detail:
      '/stop halts compute billing while keeping disks and range state; /start ' +
      'brings the same range back. /destroy is irreversible — it deletes the VMs, ' +
      'disks and network, and the next run starts again from step 1.',
  },
]

/** Freeform prompts that show the agent is more than a command runner. */
export const NATURAL_LANGUAGE_EXAMPLES = [
  'is the range healthy?',
  'DC02 is not responding — find out why',
  'which subscription is this deployed into?',
  'clean the attack box so I can rerun the agent',
]

/** What a rendered line is, so the view styles it without guessing. */
export type HelpLineKind = 'title' | 'command' | 'detail' | 'blank'

export interface HelpLine {
  text: string
  kind: HelpLineKind
}

/**
 * Render the guide as classified lines.
 *
 * ``catalog`` is the live command registry from /api/commands, used only to
 * describe commands the workflow references — so a command that is renamed or
 * dropped shows up here as missing rather than as a stale hand-written line.
 *
 * The kind is carried explicitly rather than inferred from the text. Detail
 * paragraphs routinely open with a command name ("/scrub deletes …"), so any
 * pattern that spots commands by a leading slash mis-styles them as rows.
 */
export function buildHelpLines(catalog: CommandDef[]): HelpLine[] {
  const byName = new Map(catalog.map(c => [c.name, c]))
  const lines: HelpLine[] = [
    { text: 'DREADGOAD CONSOLE — a range run, start to finish', kind: 'title' },
    { text: '', kind: 'blank' },
  ]

  for (const phase of WORKFLOW) {
    lines.push({ text: phase.title, kind: 'title' })
    // Only surface commands the backend actually offers; if one disappears
    // from the registry it silently drops out rather than lying about it.
    const present = phase.commands.filter(n => byName.has(n))
    if (present.length > 0) {
      const width = Math.max(...present.map(n => n.length)) + 3
      for (const name of present) {
        lines.push({
          text: `  ${name.padEnd(width)}${byName.get(name)!.description}`,
          kind: 'command',
        })
      }
    }
    lines.push({ text: `  ${phase.detail}`, kind: 'detail' })
    lines.push({ text: '', kind: 'blank' })
  }

  lines.push({ text: 'ASK IN NATURAL LANGUAGE', kind: 'title' })
  lines.push({ text: '', kind: 'blank' })
  for (const ex of NATURAL_LANGUAGE_EXAMPLES) {
    lines.push({ text: `  ${ex}`, kind: 'command' })
  }
  lines.push({ text: '', kind: 'blank' })
  lines.push({
    text: '  Type / for the full command list, or /help to see this again.',
    kind: 'detail',
  })
  return lines
}

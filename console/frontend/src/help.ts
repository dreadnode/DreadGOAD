// The operator's guide: what a range run looks like end to end.
//
// Rendered in two places from this one source — the empty chat pane when a
// session has no history yet, and the `/help` command. They must never drift,
// which is why the content lives here rather than inline in either.

import type { CommandDef } from './api'

export interface WorkflowCommand {
  name: string
  /** Context or caution beyond the command catalog's short description. */
  guidance: string
}

/** One phase of the range lifecycle, in the order an operator meets it. */
export interface WorkflowPhase {
  title: string
  /** Commands central to this phase, in the order you'd reach for them. */
  commands: WorkflowCommand[]
}

// Ordered deliberately: this is a cycle, not a list. Score → scrub → reset
// returns you to step 3 for the next agent run without redeploying.
export const WORKFLOW: WorkflowPhase[] = [
  {
    title: 'Deploy the range',
    commands: [
      {
        name: '/variant',
        guidance: 'Use first when the engagement needs fresh names and passwords; never run it against a deployed range.',
      },
      {
        name: '/up',
        guidance: 'Builds and provisions end to end; it runs for tens of minutes and starts billing.',
      },
      {
        name: '/extensions',
        guidance: 'Adds optional machines such as ELK or Wazuh.',
      },
    ],
  },
  {
    title: 'Confirm it came up',
    commands: [
      {
        name: '/status',
        guidance: 'Runs the available instance and health checks in one pass.',
      },
      {
        name: '/instances',
        guidance: 'Shows the cloud view: power state and IP addresses.',
      },
      {
        name: '/health',
        guidance: "Runs the selected range's core service checks.",
      },
      {
        name: '/secure',
        guidance: 'Audits network security posture, including public IPs and bastion access.',
      },
    ],
  },
  {
    title: 'Validate the lab content',
    commands: [
      {
        name: '/validate',
        guidance: "Checks the selected range's complete expected services, apps, users, and seeded data.",
      },
    ],
  },
  {
    title: 'Fix what is wrong',
    commands: [
      {
        name: '/exec',
        guidance: 'Runs a script through the cloud control plane, even when host management is down.',
      },
      { name: '/restart', guidance: 'Reboots one host.' },
      { name: '/provision', guidance: 'Re-runs the playbooks across the range.' },
    ],
  },
  {
    title: 'Score an agent run',
    commands: [
      {
        name: '/score',
        guidance: "Grades an agent report with the selected range's scorer; provide its path on the attack box.",
      },
    ],
  },
  {
    title: 'Reset for the next run',
    commands: [
      {
        name: '/scrub',
        guidance: 'Deletes engagement artifacts and applies by default; pass "dry" to preview.',
      },
      {
        name: '/reset',
        guidance: "Invokes the selected range's baseline restore.",
      },
    ],
  },
  {
    title: 'Park it or tear it down',
    commands: [
      { name: '/stop', guidance: 'Halts compute billing while keeping disks and range state.' },
      { name: '/start', guidance: 'Brings the same stopped range back.' },
      { name: '/destroy', guidance: 'Irreversibly deletes the machines, disks, and network.' },
    ],
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

  let step = 1
  for (const phase of WORKFLOW) {
    // Phase text is capability-bound too: omitting only unsupported command
    // rows would leave headings and guidance that promise unavailable flows.
    const present = phase.commands.filter(command => byName.has(command.name))
    if (present.length === 0) continue
    lines.push({ text: `${step}. ${phase.title}`, kind: 'title' })
    step += 1
    const width = Math.max(...present.map(command => command.name.length)) + 3
    for (const command of present) {
      lines.push({
        text: `  ${command.name.padEnd(width)}${byName.get(command.name)!.description}`,
        kind: 'command',
      })
      lines.push({ text: `  ${command.guidance}`, kind: 'detail' })
    }
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

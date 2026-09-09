import type { CommandDef } from '../api'

// Client-side commands have no backend CLI verb and never reach the agent.
export const HELP_COMMAND: CommandDef = {
  name: '/help',
  description: 'How a range run works, start to finish',
  detail: 'read-only; shown automatically in an empty session',
  cli: '',
  dispatch: 'direct',
  long_running: false,
  takes_args: false,
}

export const COPY_COMMAND: CommandDef = {
  name: '/copy',
  description: 'Copy last N agent messages (default 1, "all" for entire chat)',
  detail: 'copies to clipboard; nothing is sent to the backend',
  cli: '',
  dispatch: 'direct',
  long_running: false,
  takes_args: true,
}

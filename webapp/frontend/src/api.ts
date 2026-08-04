// REST client for session lifecycle + RangeView reads (design §7).

import type { RangeDoc, Session } from './types'

async function json<T>(res: Response): Promise<T> {
  if (!res.ok) throw new Error(`${res.status} ${await res.text()}`)
  return res.json() as Promise<T>
}

export interface AppConfig {
  version: string
  default_model: string
  default_config_path: string
}

export interface CommandDef {
  name: string
  description: string
  dispatch: 'direct' | 'agent'
  long_running: boolean
  takes_args: boolean
}

export const api = {
  config: (): Promise<AppConfig> => fetch('/api/config').then(r => json<AppConfig>(r)),

  commands: (): Promise<{ commands: CommandDef[] }> =>
    fetch('/api/commands').then(r => json(r)),

  listSessions: (): Promise<{ sessions: Session[] }> =>
    fetch('/api/sessions').then(r => json(r)),

  createSession: (body: Record<string, unknown>): Promise<Session> =>
    fetch('/api/sessions', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    }).then(r => json<Session>(r)),

  deleteSession: (id: string): Promise<unknown> =>
    fetch(`/api/sessions/${id}`, { method: 'DELETE' }).then(r => json(r)),

  getRange: (id: string): Promise<RangeDoc> =>
    fetch(`/api/ranges/${id}`).then(r => json<RangeDoc>(r)),

  saveLayout: (id: string, layout: Record<string, { x: number; y: number }>): Promise<unknown> =>
    fetch(`/api/ranges/${id}/layout`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ layout }),
    }).then(r => json(r)),
}

// REST client for session lifecycle + RangeView reads (design §7).

import type { RangeDoc, RangeLayout, Session } from './types'

/**
 * The human-readable part of a failed response.
 *
 * FastAPI puts the message in `detail`, so the raw body is JSON. Rendering it
 * verbatim put things like
 *
 *   400 {"detail":"[Errno 2] No such file or directory: '/path/to.yaml'"}
 *
 * in front of the operator — the status code, the envelope and the quoting all
 * competing with the one sentence that matters. `detail` may itself be a list
 * of objects (FastAPI's validation errors), so those are flattened to their
 * messages rather than stringified into "[object Object]".
 */
export function errorMessage(status: number, body: string): string {
  let parsed: unknown
  try {
    parsed = JSON.parse(body)
  } catch {
    // Not JSON — a proxy error page or a plain-text body. Use it as-is, but
    // keep the status, which is the only signal such a response carries.
    const text = body.trim()
    return text ? `${status}: ${text}` : `request failed (${status})`
  }

  const detail = (parsed as { detail?: unknown } | null)?.detail
  if (typeof detail === 'string' && detail.trim()) return detail.trim()
  if (Array.isArray(detail)) {
    const parts = detail
      .map(d => (typeof d === 'string' ? d : (d as { msg?: string })?.msg))
      .filter((m): m is string => typeof m === 'string' && m.trim().length > 0)
    if (parts.length) return parts.join('; ')
  }
  return `request failed (${status})`
}

async function json<T>(res: Response): Promise<T> {
  if (!res.ok) throw new Error(errorMessage(res.status, await res.text()))
  return res.json() as Promise<T>
}

export interface AppConfig {
  version: string
  default_model: string
  default_config_path: string
  api_key_set: boolean
  // Providers the console can drive end to end. Sent by the backend rather
  // than hardcoded here so the two cannot drift; the CLI supports more
  // (proxmox, ludus) than the console can render.
  providers?: string[]
}

/** One `dreadgoad.yaml` the console can attach to, as listed by /api/configs. */
export interface ConfigSummary {
  path: string
  name: string
  /** `default` = the repo-root one, `managed` = console-created, `session` = learned from an existing session's anchor. */
  source: 'default' | 'managed' | 'session'
  provider?: string | null
  region?: string | null
  environments: string[]
  /** Set when the file could not be read or parsed; it is still listed. */
  error?: string | null
}

/** A lab that can be used as a `variant_source`, from `dreadgoad lab list --json`. */
export interface LabSummary {
  name: string
  /** Repo-relative dir — the value written to `variant_source`, e.g. `ad/GOAD`. */
  dir: string
  /** Providers the lab ships terraform for; a lab missing the session's provider cannot deploy. */
  providers: string[]
  hosts: string[]
  /** True when this is itself a generated variant (it has a mapping.json). */
  generated: boolean
}

export interface ConfigListing {
  configs: ConfigSummary[]
  configs_root: string
  providers: string[]
  /** Provider → advisory note when its credentials aren't visible, else null. */
  credential_hints: Record<string, string | null>
  /** Provider → suggested regions. Suggestions only; the field stays free text. */
  regions?: Record<string, string[]>
}

export interface CommandDef {
  name: string
  description: string
  detail: string        // consequence/prerequisite, shown under the description
  cli: string           // the dreadgoad verb this maps to
  dispatch: 'direct' | 'agent'
  long_running: boolean
  takes_args: boolean
  /** Cannot be undone. The UI confirms before running a `direct` one. */
  destructive?: boolean
}

export const api = {
  config: (): Promise<AppConfig> => fetch('/api/config').then(r => json<AppConfig>(r)),

  commands: (): Promise<{ commands: CommandDef[] }> =>
    fetch('/api/commands').then(r => json(r)),

  configs: (): Promise<ConfigListing> => fetch('/api/configs').then(r => json<ConfigListing>(r)),

  labs: (configPath?: string): Promise<{ labs: LabSummary[] }> =>
    fetch('/api/labs' + (configPath ? `?config_path=${encodeURIComponent(configPath)}` : ''))
      .then(r => json(r)),

  environments: (configPath: string): Promise<{
    environments: string[]
    provider?: string
    region?: string
    /** Per environment, the region the CLI resolves — env key first, file key as fallback. */
    env_regions?: Record<string, string | null>
  }> =>
    fetch(`/api/environments?config_path=${encodeURIComponent(configPath)}`).then(r => json(r)),

  setSettings: (body: { api_key?: string; api_key_env?: string }): Promise<{ ok: boolean; api_key_env: string }> =>
    fetch('/api/settings', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    }).then(r => json(r)),

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

  setModel: (id: string, model: string): Promise<{ ok: boolean; model: string }> =>
    fetch(`/api/sessions/${id}/model`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ model }),
    }).then(r => json(r)),

  getRange: (id: string): Promise<RangeDoc> =>
    fetch(`/api/ranges/${id}`).then(r => json<RangeDoc>(r)),

  saveLayout: (
    id: string,
    layout: RangeLayout,
    revision: number,
  ): Promise<{ ok: boolean; layout_revision: number }> =>
    fetch(`/api/ranges/${id}/layout`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ layout, revision }),
    }).then(r => json(r)),
}

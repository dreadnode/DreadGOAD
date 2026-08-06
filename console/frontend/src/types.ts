// Shared types mirroring the backend schema (design §6.3).

export interface SessionSnapshot {
  provider?: string
  region?: string
  lab?: string
  variant_name?: string
  vpc_cidr?: string
  attack_box?: string | null
  // Where the range landed, learned by the ingestion hook post-deploy and so
  // absent until the range has been read at least once. Provider-neutral:
  // `account` is an AWS account ID or an Azure subscription ID; `group` is an
  // Azure resource group and stays absent on AWS, which has no equivalent.
  account?: string | null
  group?: string | null
  // Provider blocks hold connection selectors only — placement lives in the
  // neutral `account`/`group` above.
  azure?: {
    ssh_key?: string | null
    ssh_user?: string | null
  }
  aws?: { profile?: string | null }
}

export interface Session {
  id: string
  label: string
  model?: string
  status: string
  anchor: { config_path: string; env: string }
  snapshot: SessionSnapshot
  session_dir?: string
}

export interface RangeHost {
  id: string
  hostname: string
  role: string
  source: string
  domain?: string | null
  status: string
  health: string
  ip_private?: string | null
  ip_public?: string | null
  cloud_id?: string | null
  cloud_name?: string | null   // provider VM name, e.g. env-dreadgoad-DC01-vm
  key?: string                 // config key / CLI host role, e.g. dc01
  last_checked_at?: string | null
}

export interface RangeEdge {
  from: string
  to: string
  type: string
}

export interface RangeDoc {
  session_id: string
  hosts: RangeHost[]
  edges: RangeEdge[]
  layout: Record<string, { x: number; y: number }>
  last_checked_at?: string | null
}

// One instance from `lab status --json` (/instances) — raw cloud fields.
export interface Instance {
  name: string
  id: string
  state: string
  private_ip: string
}

// One row of a /health report (health-check --json).
export interface HealthCheck {
  name: string
  host: string
  status: 'OK' | 'FAIL' | 'SKIP'
  detail: string
}

// One category rollup from a /validate report, worst-state-first.
export interface ValidateCategory {
  category: string
  state: 'failed' | 'passed' | 'skipped'
  passed: number
  failed: number
  total: number
}

// A single failed check from /validate.
export interface ValidateFailure {
  category: string
  name: string
}

// One host's cleanup result from /scrub.
export interface ScrubHost {
  host: string
  found: number
  removed: number
  clean: boolean
  errors: string[]
}

// Chat event as sent over the WebSocket (kind + kind-specific fields).
export interface ChatEvent {
  _cid?: number // client-assigned stable key (App-side)
  session_id?: string
  kind: string
  content?: string
  tool?: string
  args?: string
  result?: string
  message?: string
  command?: string
  exit_code?: number
  cancelled?: boolean
  line?: string
  hosts_updated?: number
  passed?: number
  failed?: number
  skipped?: number
  checks?: HealthCheck[]
  instances?: Instance[]
  total?: number
  running?: number
  warnings?: number
  categories?: ValidateCategory[]
  failures?: ValidateFailure[]
  mode?: string
  hosts?: ScrubHost[]
  found?: number
  removed?: number
  usage?: { input_tokens?: number; output_tokens?: number }
  events?: ChatEvent[]
  [key: string]: unknown
}

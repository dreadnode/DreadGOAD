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
  os?: string | null           // explicit lab metadata; absent on older ranges
  last_checked_at?: string | null
}

export type RangeLayout = Record<string, { x: number; y: number }>

export interface RangeDoc {
  session_id: string
  hosts: RangeHost[]
  edges: unknown[]
  layout: RangeLayout
  layout_revision?: number
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
  status: 'OK' | 'FAIL' | 'WARN' | 'SKIP'
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

// One host's result from /exec. `stdout`/`stderr` are raw output from a
// deliberately vulnerable range — render as text, never as markup.
export interface ExecResult {
  host: string
  instance_id: string
  status: string
  stdout: string
  stderr: string
}

// One check from a /secure report (security-check --json).
export interface SecurityCheck {
  name: string
  resource: string
  status: 'OK' | 'FAIL' | 'WARN' | 'SKIP'
  severity: 'critical' | 'high' | 'info'
  detail: string
}

interface ChatEventMeta {
  _cid?: number // client-assigned key for live events (App-side)
  seq?: number  // server-assigned key for persisted events (DB-side)
  ts?: string
  session_id?: string
}

export interface ApprovalRequest {
  approval_id: string
  command: string
  argv: string[]
  detail: string
  requested_at: string
}

interface UserMessageEvent { kind: 'user_message'; content: string }
interface GenerationEvent {
  kind: 'generation'
  content: string
  usage?: { input_tokens?: number; output_tokens?: number } | null
}
export interface ToolStartEvent { kind: 'tool_start'; tool: string; args: string }
interface ToolEndEvent { kind: 'tool_end'; tool: string; result: string }
interface ErrorEvent { kind: 'error'; message: string }
interface AgentEndEvent { kind: 'agent_end'; failed: boolean; cancelled?: boolean }
interface StatusEvent { kind: 'status'; content: string }
interface CommandStartEvent {
  kind: 'command_run'
  phase: 'start'
  command: string
  argv: string[]
  cwd: string
  approval_id?: string
}
interface CommandEndEvent {
  kind: 'command_run'
  phase: 'end'
  command: string
  exit_code: number
  cancelled: boolean
  /** Cancelled, but the work it started is still finishing outside this
   *  process (a cloud lifecycle op, a playbook already running on a host). */
  still_running?: boolean
  tail?: string
  approval_id?: string
}
interface CommandProgressEvent { kind: 'command_progress'; line: string }
interface CheckRunEvent {
  kind: 'check_run'
  hosts_updated?: number
  changes?: unknown[]
  error?: string
}
export interface HealthReportEvent {
  kind: 'health_report'
  passed: number
  failed: number
  warned: number
  skipped: number
  checks: HealthCheck[]
}
export interface InstancesReportEvent {
  kind: 'instances_report'
  instances: Instance[]
  total: number
  running: number
}
export interface ValidateReportEvent {
  kind: 'validate_report'
  passed: number
  failed: number
  warnings: number
  total: number
  categories: ValidateCategory[]
  failures: ValidateFailure[]
}
export interface ScrubReportEvent {
  kind: 'scrub_report'
  mode: string
  hosts: ScrubHost[]
  found: number
  removed: number
}
export interface ExecReportEvent {
  kind: 'exec_report'
  results: ExecResult[]
  succeeded: number
  total: number
}
export interface SecurityReportEvent {
  kind: 'security_report'
  passed: number
  failed: number
  warned: number
  skipped: number
  security_checks: SecurityCheck[]
}
interface ApprovalRequiredEvent extends ApprovalRequest {
  kind: 'approval_required'
}
interface ApprovalResolvedEvent {
  kind: 'approval_resolved'
  approval_id: string
  command: string
  decision: 'approved' | 'denied' | 'expired' | 'cancelled'
}
interface HistoryEvent {
  kind: 'history'
  events: ChatEvent[]
  active: boolean
  started_at: string | null
  command: string | null
  approval: ApprovalRequest | null
}

type ChatEventPayload =
  | UserMessageEvent | GenerationEvent | ToolStartEvent | ToolEndEvent
  | ErrorEvent | AgentEndEvent | StatusEvent
  | CommandStartEvent | CommandEndEvent | CommandProgressEvent | CheckRunEvent
  | HealthReportEvent | InstancesReportEvent | ValidateReportEvent
  | ScrubReportEvent | ExecReportEvent | SecurityReportEvent
  | ApprovalRequiredEvent | ApprovalResolvedEvent | HistoryEvent

// The `kind` discriminator makes every event payload independently checkable.
export type ChatEvent = ChatEventMeta & ChatEventPayload

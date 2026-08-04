// Shared types mirroring the backend schema (design §6.3).

export interface SessionSnapshot {
  provider?: string
  region?: string
  lab?: string
  variant_name?: string
  vpc_cidr?: string
  attack_box?: string | null
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

// Chat event as sent over the WebSocket (kind + kind-specific fields).
export interface ChatEvent {
  session_id?: string
  kind: string
  content?: string
  tool?: string
  args?: string
  result?: string
  message?: string
  command?: string
  exit_code?: number
  line?: string
  hosts_updated?: number
  usage?: { input_tokens?: number; output_tokens?: number }
  events?: ChatEvent[]
  [key: string]: unknown
}

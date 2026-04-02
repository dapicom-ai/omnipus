// REST API client — all calls go through the backend gateway
// Auth: Authorization: Bearer <token> header when a token is stored in localStorage ('omnipus_auth_token'). The backend validates against OMNIPUS_BEARER_TOKEN.

const BASE_URL = import.meta.env.VITE_API_URL ?? ''

function getAuthHeaders(): HeadersInit {
  const token = localStorage.getItem('omnipus_auth_token')
  return token ? { Authorization: `Bearer ${token}` } : {}
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${BASE_URL}/api/v1${path}`, {
    ...init,
    headers: {
      'Content-Type': 'application/json',
      ...getAuthHeaders(),
      ...init?.headers,
    },
  })
  if (!res.ok) {
    const text = await res.text().catch(() => res.statusText)
    throw new Error(`${res.status}: ${text}`)
  }
  return res.json() as Promise<T>
}

// ── Agents ────────────────────────────────────────────────────────────────────

export interface Agent {
  id: string
  name: string
  description: string
  type: 'system' | 'core' | 'custom'
  model: string
  status: 'active' | 'idle' | 'error' | 'draft'
  icon?: string
  color?: string
  tools?: string[]
  soul?: string
  heartbeat?: string
  instructions?: string
  fallback_models?: string[]
  model_params?: {
    temperature?: number
    max_tokens?: number
    top_p?: number
  }
  timeout_seconds?: number
  max_tool_iterations?: number
  steering_mode?: string
  tool_feedback?: boolean
  heartbeat_enabled?: boolean
  heartbeat_interval?: number
  rate_limits?: {
    use_global_defaults: boolean
    max_llm_calls_per_hour?: number
    max_tool_calls_per_minute?: number
    max_cost_per_day?: number
  }
  stats?: {
    total_sessions: number
    total_tokens: number
    total_cost: number
    last_active?: string
  }
}

export function fetchAgents(): Promise<Agent[]> {
  return request<Agent[]>('/agents')
}

export function fetchAgent(id: string): Promise<Agent> {
  return request<Agent>(`/agents/${encodeURIComponent(id)}`)
}

export function createAgent(data: Partial<Agent>): Promise<Agent> {
  return request<Agent>('/agents', { method: 'POST', body: JSON.stringify(data) })
}

export function updateAgent(id: string, data: Partial<Agent>): Promise<Agent> {
  return request<Agent>(`/agents/${encodeURIComponent(id)}`, { method: 'PUT', body: JSON.stringify(data) })
}

export interface AgentSession {
  id: string
  title: string
  created_at: string
  updated_at: string
}

export function fetchAgentSessions(agentId: string): Promise<AgentSession[]> {
  return request<AgentSession[]>(`/agents/${encodeURIComponent(agentId)}/sessions`)
}

// ── Sessions ──────────────────────────────────────────────────────────────────

export interface Session {
  id: string
  agent_id: string
  title: string
  type: 'chat' | 'task' | 'channel'
  status?: 'active' | 'archived' | 'interrupted'
  task_id?: string
  created_at: string
  updated_at: string
  message_count: number
  total_tokens?: number
  total_cost?: number
}

interface RawSession {
  id: string
  agent_id: string
  title: string
  type?: 'chat' | 'task' | 'channel'
  status?: 'active' | 'archived' | 'interrupted'
  task_id?: string
  created_at: string
  updated_at: string
  stats?: {
    tokens_in: number
    tokens_out: number
    tokens_total: number
    cost: number
    tool_calls: number
    message_count: number
  }
}

function rawToSession(raw: RawSession): Session {
  return {
    id: raw.id,
    agent_id: raw.agent_id,
    title: raw.title,
    // Legacy sessions without a type field default to 'chat'
    type: raw.type ?? 'chat',
    status: raw.status,
    task_id: raw.task_id,
    created_at: raw.created_at,
    updated_at: raw.updated_at,
    message_count: raw.stats?.message_count ?? 0,
    total_tokens: raw.stats?.tokens_total,
    total_cost: raw.stats?.cost,
  }
}

export interface Message {
  id: string
  session_id?: string
  role: 'user' | 'assistant' | 'system'
  content: string
  timestamp: string
  tokens?: number
  cost?: number
  status?: 'streaming' | 'done' | 'error' | 'interrupted'
  tool_calls?: ToolCall[]
}

export interface ToolCall {
  id: string
  tool: string
  params: Record<string, unknown>
  result?: unknown
  status: 'running' | 'success' | 'error' | 'cancelled'
  duration_ms?: number
  error?: string
}

export async function fetchSessions(agentId?: string, type?: Session['type']): Promise<Session[]> {
  const params: Record<string, string> = {}
  if (agentId) params.agent_id = agentId
  if (type) params.type = type
  const qs = Object.keys(params).length > 0 ? '?' + new URLSearchParams(params).toString() : ''
  const raw = await request<RawSession[]>(`/sessions${qs}`)
  return raw.map(rawToSession)
}

export function fetchSessionMessages(sessionId: string): Promise<Message[]> {
  return request<Message[]>(`/sessions/${encodeURIComponent(sessionId)}/messages`)
}

export function createSession(agentId: string): Promise<Session> {
  return request<Session>('/sessions', {
    method: 'POST',
    body: JSON.stringify({ agent_id: agentId }),
  })
}

// ── Config ────────────────────────────────────────────────────────────────────

// Frontend-shaped config. Mapped from raw backend response via rawToFrontendConfig().
export interface Config {
  gateway: {
    bind_address: string
    port: number
    auth_mode: 'none' | 'token'
    token?: string
    hot_reload?: boolean
    log_level?: string
  }
  security: {
    policy_mode: 'allow' | 'deny'
    exec_approval: 'auto' | 'ask' | 'deny'
    prompt_injection_level: 'off' | 'low' | 'medium' | 'high'
    daily_cost_cap?: number
    exec_timeout_seconds?: number
    max_background_seconds?: number
    enable_deny_patterns?: boolean
    rate_limits: {
      max_tokens_per_day?: number
      max_cost_per_day?: number
      max_agent_llm_calls_per_hour?: number
      max_agent_tool_calls_per_minute?: number
    }
  }
  data: {
    session_retention_days: number
  }
}

const VALID_AUTH_MODES = ['none', 'token'] as const
const VALID_POLICY_MODES = ['allow', 'deny'] as const
const VALID_EXEC_APPROVALS = ['auto', 'ask', 'deny'] as const
const VALID_INJECTION_LEVELS = ['off', 'low', 'medium', 'high'] as const

function validEnum<T extends string>(value: unknown, valid: readonly T[], fallback: T): T {
  if ((valid as readonly string[]).includes(value as string)) return value as T
  console.warn('[api] validEnum: unexpected value', value, '— falling back to', fallback)
  return fallback
}

function rawToFrontendConfig(raw: Record<string, unknown>): Config {
  const gateway = (raw.gateway ?? {}) as Record<string, unknown>
  const storage = (raw.storage ?? {}) as Record<string, unknown>
  const retention = (storage.retention ?? {}) as Record<string, unknown>
  const security = (raw.security ?? {}) as Record<string, unknown>
  const rateLimits = (security.rate_limits ?? {}) as Record<string, unknown>
  return {
    gateway: {
      bind_address: (gateway.host as string) ?? '127.0.0.1',
      port: (gateway.port as number) ?? 8080,
      auth_mode: validEnum(gateway.auth_mode, VALID_AUTH_MODES, 'none'),
      token: gateway.token as string | undefined,
      hot_reload: gateway.hot_reload as boolean | undefined,
      log_level: gateway.log_level as string | undefined,
    },
    security: {
      policy_mode: validEnum(security.policy_mode, VALID_POLICY_MODES, 'deny'),
      exec_approval: validEnum(security.exec_approval, VALID_EXEC_APPROVALS, 'ask'),
      prompt_injection_level: validEnum(security.prompt_injection_level, VALID_INJECTION_LEVELS, 'medium'),
      daily_cost_cap: security.daily_cost_cap as number | undefined,
      exec_timeout_seconds: security.exec_timeout_seconds as number | undefined,
      max_background_seconds: security.max_background_seconds as number | undefined,
      enable_deny_patterns: security.enable_deny_patterns as boolean | undefined,
      rate_limits: {
        max_tokens_per_day: rateLimits.max_tokens_per_day as number | undefined,
        max_cost_per_day: rateLimits.max_cost_per_day as number | undefined,
        max_agent_llm_calls_per_hour: rateLimits.max_agent_llm_calls_per_hour as number | undefined,
        max_agent_tool_calls_per_minute: rateLimits.max_agent_tool_calls_per_minute as number | undefined,
      },
    },
    data: {
      session_retention_days: (retention.session_days as number) ?? 90,
    },
  }
}

export async function fetchConfig(): Promise<Config> {
  const raw = await request<Record<string, unknown>>('/config')
  return rawToFrontendConfig(raw)
}

export function updateConfig(data: Partial<Config>): Promise<Config> {
  return request<Config>('/config', { method: 'PUT', body: JSON.stringify(data) })
}

// ── Providers ─────────────────────────────────────────────────────────────────

export interface Provider {
  id: string
  name?: string
  display_name?: string
  status: 'connected' | 'disconnected' | 'error'
  models?: string[]
  error?: string
}

export function fetchProviders(): Promise<Provider[]> {
  return request<Provider[]>('/providers')
}

export function configureProvider(id: string, apiKey: string, endpoint?: string): Promise<Provider> {
  return request<Provider>(`/providers/${id}`, {
    method: 'PUT',
    body: JSON.stringify({ api_key: apiKey, endpoint }),
  })
}

export function testProvider(id: string): Promise<{ success: boolean; error?: string }> {
  return request(`/providers/${id}/test`, { method: 'POST' })
}

export function rotateGatewayToken(): Promise<{ token: string }> {
  return request('/config/gateway/rotate-token', { method: 'POST' })
}

// ── Tasks ─────────────────────────────────────────────────────────────────────

export interface Task {
  id: string
  title: string
  prompt: string
  agent_id?: string
  agent_name?: string
  created_by?: string
  parent_task_id?: string
  priority: number
  status: 'queued' | 'assigned' | 'running' | 'completed' | 'failed'
  result?: string
  artifacts?: string[]
  trigger_type: 'manual' | 'time' | 'event'
  created_at?: string
  started_at?: string
  completed_at?: string
}

export function fetchTasks(status?: Task['status']): Promise<Task[]> {
  const qs = status ? '?' + new URLSearchParams({ status }).toString() : ''
  return request<Task[]>(`/tasks${qs}`)
}

export function fetchSubtasks(taskId: string): Promise<Task[]> {
  return request<Task[]>(`/tasks/${encodeURIComponent(taskId)}/subtasks`)
}

export function createTask(data: {
  title: string
  prompt: string
  agent_id?: string
  priority?: number
  parent_task_id?: string
}): Promise<Task> {
  return request<Task>('/tasks', { method: 'POST', body: JSON.stringify(data) })
}

export function updateTask(id: string, data: Partial<Task>): Promise<Task> {
  return request<Task>(`/tasks/${encodeURIComponent(id)}`, { method: 'PUT', body: JSON.stringify(data) })
}

export function startTask(id: string): Promise<void> {
  return request(`/tasks/${encodeURIComponent(id)}/start`, { method: 'POST' })
}

export function deleteTask(id: string): Promise<void> {
  return request(`/tasks/${encodeURIComponent(id)}`, { method: 'DELETE' })
}

// ── Gateway Status ────────────────────────────────────────────────────────────

export interface GatewayStatus {
  online: boolean
  agent_count: number
  channel_count: number
  daily_cost: number
  version?: string
}

export function fetchGatewayStatus(): Promise<GatewayStatus> {
  return request<GatewayStatus>('/status')
}

// ── Tools & Channels ──────────────────────────────────────────────────────────

export interface Tool {
  name: string
  category: string
  description: string
}

export function fetchTools(): Promise<Tool[]> {
  return request<Tool[]>('/tools')
}

export interface Channel {
  id: string
  name: string
  transport: string
  enabled: boolean
  configured?: boolean
}

export function fetchChannels(): Promise<Channel[]> {
  return request<Channel[]>('/channels')
}

export function enableChannel(id: string): Promise<Channel> {
  return request<Channel>(`/channels/${encodeURIComponent(id)}/enable`, { method: 'PUT' })
}

export function disableChannel(id: string): Promise<Channel> {
  return request<Channel>(`/channels/${encodeURIComponent(id)}/disable`, { method: 'PUT' })
}

export function fetchChannelConfig(id: string): Promise<Record<string, unknown>> {
  return request<Record<string, unknown>>(`/channels/${encodeURIComponent(id)}`)
}

export function configureChannel(id: string, config: Record<string, unknown>): Promise<void> {
  return request<void>(`/channels/${encodeURIComponent(id)}/configure`, {
    method: 'PUT',
    body: JSON.stringify(config),
  })
}

export function testChannel(id: string): Promise<{ success: boolean; message: string }> {
  return request<{ success: boolean; message: string }>(`/channels/${encodeURIComponent(id)}/test`, {
    method: 'POST',
  })
}

// ── Skills ────────────────────────────────────────────────────────────────────

export interface Skill {
  id: string
  name: string
  version: string
  description: string
  author: string
  verified: boolean
  status: 'active' | 'inactive' | 'error'
  agent_assignment?: string
}

export interface McpServer {
  id: string
  name: string
  transport: 'stdio' | 'sse' | 'websocket'
  status: 'connected' | 'disconnected' | 'error'
  tool_count: number
  tools?: string[]
}

export interface McpServerCreate {
  name: string
  command: string
  args?: string[]
  transport: 'stdio' | 'sse' | 'websocket'
}

export function fetchSkills(): Promise<Skill[]> {
  return request<Skill[]>('/skills')
}

export function deleteSkill(name: string): Promise<void> {
  return request<void>(`/skills/${encodeURIComponent(name)}`, { method: 'DELETE' })
}

export function fetchMcpServers(): Promise<McpServer[]> {
  return request<McpServer[]>('/mcp-servers')
}

export function addMcpServer(data: McpServerCreate): Promise<McpServer> {
  return request<McpServer>('/mcp-servers', { method: 'POST', body: JSON.stringify(data) })
}

export function deleteMcpServer(id: string): Promise<void> {
  return request<void>(`/mcp-servers/${encodeURIComponent(id)}`, { method: 'DELETE' })
}

export function fetchMcpServerTools(id: string): Promise<string[]> {
  return request<string[]>(`/mcp-servers/${encodeURIComponent(id)}/tools`)
}

// ── Storage Stats ─────────────────────────────────────────────────────────────

export interface StorageStats {
  workspace_size_bytes: number
  session_count: number
  memory_entry_count: number
  oldest_session_date?: string
}

export function fetchStorageStats(): Promise<StorageStats> {
  return request<StorageStats>('/storage/stats')
}

// ── App State ─────────────────────────────────────────────────────────────────

export interface AppState {
  onboarding_complete: boolean
  last_doctor_run?: string
  last_doctor_score?: number
}

export function fetchAppState(): Promise<AppState> {
  return request<AppState>('/state')
}

export function completeOnboarding(): Promise<void> {
  return request('/state', {
    method: 'PATCH',
    body: JSON.stringify({ onboarding_complete: true }),
  })
}

// ── Doctor ────────────────────────────────────────────────────────────────────

export interface DoctorIssue {
  id: string
  severity: 'high' | 'medium' | 'low'
  title: string
  description: string
  recommendation: string
  action_link?: string
  action_label?: string
}

export interface DoctorResult {
  score: number
  issues: DoctorIssue[]
  checked_at: string
}

export function fetchDoctorResults(): Promise<DoctorResult | null> {
  return request<DoctorResult | null>('/doctor')
}

export function runDoctor(): Promise<DoctorResult> {
  return request<DoctorResult>('/doctor', { method: 'POST' })
}

// ── Activity Feed ─────────────────────────────────────────────────────────────

export interface ActivityEvent {
  id: string
  type: 'task_created' | 'task_updated' | 'session_started' | 'session_ended' | 'agent_error' | 'tool_called' | 'approval_requested' | (string & {})
  summary: string
  timestamp: string
  agent_id?: string
  agent_name?: string
}

export function fetchActivity(): Promise<ActivityEvent[]> {
  return request<ActivityEvent[]>('/activity')
}

// ── Credentials ───────────────────────────────────────────────────────────────

export interface CredentialKey {
  key: string
  created_at?: string
  updated_at?: string
}

export function fetchCredentials(): Promise<CredentialKey[]> {
  return request<CredentialKey[]>('/credentials')
}

export function addCredential(key: string, value: string): Promise<void> {
  return request<void>('/credentials', { method: 'POST', body: JSON.stringify({ key, value }) })
}

export function deleteCredential(key: string): Promise<void> {
  return request<void>(`/credentials/${encodeURIComponent(key)}`, { method: 'DELETE' })
}

// ── Backup / Restore ──────────────────────────────────────────────────────────

export interface BackupEntry {
  filename: string
  size_bytes: number
  created_at: string
}

export function createBackup(): Promise<{ filename: string }> {
  return request('/backup', { method: 'POST' })
}

export function fetchBackups(): Promise<BackupEntry[]> {
  return request<BackupEntry[]>('/backups')
}

export function restoreBackup(filename: string): Promise<void> {
  return request<void>('/restore', { method: 'POST', body: JSON.stringify({ filename }) })
}

export function clearAllSessions(): Promise<void> {
  return request<void>('/sessions/all', { method: 'DELETE' })
}

// ── About ─────────────────────────────────────────────────────────────────────

export interface AboutInfo {
  version: string
  go_version: string
  os: string
  arch: string
  uptime_seconds: number
}

export function fetchAboutInfo(): Promise<AboutInfo> {
  return request<AboutInfo>('/about')
}

// ── Audit Log ─────────────────────────────────────────────────────────────────

export interface AuditEntry {
  id: string
  timestamp: string
  action: string
  actor?: string
  target?: string
  result: 'allow' | 'deny' | 'error'
}

export function fetchAuditLog(): Promise<AuditEntry[]> {
  return request<AuditEntry[]>('/audit')
}

// ── User Context (USER.md) ────────────────────────────────────────────────────

export function fetchUserContext(): Promise<{ content: string }> {
  return request<{ content: string }>('/user-context')
}

export function updateUserContext(content: string): Promise<void> {
  return request<void>('/user-context', {
    method: 'PUT',
    body: JSON.stringify({ content }),
  })
}

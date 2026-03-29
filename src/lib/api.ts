// REST API client — all calls go through the backend gateway
// Auth: Authorization: Bearer <token> header when OMNIPUS_BEARER_TOKEN is set

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
  status: 'active' | 'idle' | 'error'
  icon?: string
  color?: string
  tools?: string[]
  heartbeat_interval?: number
  rate_limits?: {
    use_global_defaults: boolean
    max_tokens_per_day?: number
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
  return request<Agent>(`/agents/${id}`)
}

export function createAgent(data: Partial<Agent>): Promise<Agent> {
  return request<Agent>('/agents', { method: 'POST', body: JSON.stringify(data) })
}

export function updateAgent(id: string, data: Partial<Agent>): Promise<Agent> {
  return request<Agent>(`/agents/${id}`, { method: 'PUT', body: JSON.stringify(data) })
}

// ── Sessions ──────────────────────────────────────────────────────────────────

export interface Session {
  id: string
  agent_id: string
  title: string
  created_at: string
  updated_at: string
  message_count: number
  total_tokens?: number
  total_cost?: number
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

export function fetchSessions(agentId?: string): Promise<Session[]> {
  const qs = agentId ? `?agent_id=${agentId}` : ''
  return request<Session[]>(`/sessions${qs}`)
}

export function fetchSessionMessages(sessionId: string): Promise<Message[]> {
  return request<Message[]>(`/sessions/${sessionId}/messages`)
}

export function createSession(agentId: string): Promise<Session> {
  return request<Session>('/sessions', {
    method: 'POST',
    body: JSON.stringify({ agent_id: agentId }),
  })
}

// ── Config ────────────────────────────────────────────────────────────────────

export interface Config {
  gateway: {
    bind_address: string
    port: number
    auth_mode: 'none' | 'token'
    token?: string
  }
  security: {
    policy_mode: 'allow' | 'deny'
    exec_approval: 'auto' | 'ask' | 'deny'
    prompt_injection_level: 'off' | 'low' | 'medium' | 'high'
    daily_cost_cap?: number
    rate_limits: {
      max_tokens_per_day?: number
      max_cost_per_day?: number
    }
  }
  data: {
    session_retention_days: number
  }
}

export function fetchConfig(): Promise<Config> {
  return request<Config>('/config')
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
  name: string
  description?: string
  status: 'inbox' | 'next' | 'active' | 'waiting' | 'done'
  agent_id?: string
  agent_name?: string
  cost?: number
  created_at?: string
  updated_at?: string
}

export function fetchTasks(): Promise<Task[]> {
  return request<Task[]>('/tasks')
}

export function createTask(data: Pick<Task, 'name' | 'description' | 'agent_id'>): Promise<Task> {
  return request<Task>('/tasks', { method: 'POST', body: JSON.stringify(data) })
}

export function updateTask(id: string, data: Partial<Task>): Promise<Task> {
  return request<Task>(`/tasks/${id}`, { method: 'PUT', body: JSON.stringify(data) })
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
}

export function fetchSkills(): Promise<Skill[]> {
  return request<Skill[]>('/skills')
}

export function fetchMcpServers(): Promise<McpServer[]> {
  return request<McpServer[]>('/mcp-servers')
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
  return request<DoctorResult | null>('/doctor').catch(() => null)
}

export function runDoctor(): Promise<DoctorResult> {
  return request<DoctorResult>('/doctor', { method: 'POST' })
}

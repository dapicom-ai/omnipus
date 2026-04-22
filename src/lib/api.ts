// REST API client — all calls go through the backend gateway.
// Auth: Authorization: Bearer <token> header. Token read from sessionStorage (preferred) or localStorage ('omnipus_auth_token'). Backend validates against per-user RBAC token hashes or legacy OMNIPUS_BEARER_TOKEN env var.
// CSRF: X-CSRF-Token header echoes the __Host-csrf cookie value on every
// state-changing request (double-submit cookie, issue #97). The cookie is
// issued by the backend on /auth/login, /auth/register-admin, and
// /onboarding/complete. State-changing calls made before the cookie exists
// fail fast client-side so the UI surfaces an actionable error instead of
// waiting for the server's 403.

const BASE_URL = import.meta.env.VITE_API_URL ?? ''

// CSRF_COOKIE_NAME must match pkg/gateway/middleware/csrf.go CSRFCookieName.
const CSRF_COOKIE_NAME = '__Host-csrf'
const CSRF_HEADER_NAME = 'X-CSRF-Token'
const STATE_CHANGING_METHODS = new Set(['POST', 'PUT', 'PATCH', 'DELETE'])

// CSRF_EXEMPT_PATHS lists state-changing endpoints whose handler's job is to
// ISSUE the __Host-csrf cookie. They can't require the cookie to be present
// — that's the chicken-and-egg bootstrap problem. Keep this list in sync
// with pkg/gateway/middleware/csrf.go `exemptPaths` for /api/v1/* entries.
// Paths here are compared against the /api/v1/-prefixed URL.
//
// Why each entry is here:
//   - /api/v1/onboarding/complete — called on fresh install (no cookie exists).
//   - /api/v1/auth/login — called on first load of an existing install
//     (refresh, new tab); cookie may be absent until the login succeeds.
//   - /api/v1/auth/register-admin — first-boot admin account creation.
const CSRF_EXEMPT_PATHS = new Set<string>([
  '/api/v1/onboarding/complete',
  '/api/v1/onboarding/probe-provider',
  '/api/v1/auth/login',
  '/api/v1/auth/register-admin',
])

// readCSRFCookie parses document.cookie and returns the __Host-csrf value,
// or null if the cookie is absent. We intentionally do not cache — cookies
// can change after login/logout/onboarding and caching would cause stale
// tokens on the next state-changing call.
function readCSRFCookie(): string | null {
  if (typeof document === 'undefined') return null
  const prefix = `${CSRF_COOKIE_NAME}=`
  // document.cookie is a single string of "a=b; c=d" pairs. We walk the
  // pairs manually rather than split on ";" because cookie values can
  // contain "=" (and base64 tokens certainly can).
  for (const part of document.cookie.split(';')) {
    const trimmed = part.trim()
    if (trimmed.startsWith(prefix)) {
      const raw = trimmed.slice(prefix.length)
      // Apply decodeURIComponent defensively: if the browser percent-encoded
      // the cookie value (e.g. standard base64 "=", "+", "/"), we decode it
      // so the header value matches what the server originally set. If
      // decoding fails (malformed sequence such as a lone "%"), fall back to
      // the raw string and let the server compare verbatim.
      try {
        return decodeURIComponent(raw)
      } catch {
        return raw
      }
    }
  }
  return null
}

function getAuthHeaders(): HeadersInit {
  const token = sessionStorage.getItem('omnipus_auth_token') ?? localStorage.getItem('omnipus_auth_token')
  return token ? { Authorization: `Bearer ${token}` } : {}
}

// buildHeaders composes the standard request headers, layering (in order):
// content-type → bearer auth → CSRF header → caller overrides. The CSRF
// header is added unconditionally when the cookie exists; safe GETs pass
// it through too because doing so is harmless and keeps the code simple.
function buildHeaders(extra?: HeadersInit): HeadersInit {
  const csrf = readCSRFCookie()
  return {
    'Content-Type': 'application/json',
    ...getAuthHeaders(),
    ...(csrf ? { [CSRF_HEADER_NAME]: csrf } : {}),
    ...extra,
  }
}

// isPathCSRFExempt checks whether a state-changing call can skip the
// client-side cookie-presence check. Only onboarding-complete is exempt —
// see CSRF_EXEMPT_PATHS.
function isPathCSRFExempt(apiPath: string): boolean {
  return CSRF_EXEMPT_PATHS.has(`/api/v1${apiPath}`)
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  // Client-side CSRF gate: reject state-changing calls that would be
  // guaranteed to 403 at the server. This gives a clear error immediately
  // instead of a cryptic "403 csrf cookie missing" from the network tab
  // and also prevents a cascade of dependent requests firing during a
  // broken auth state.
  const method = (init?.method ?? 'GET').toUpperCase()
  if (
    STATE_CHANGING_METHODS.has(method) &&
    !isPathCSRFExempt(path) &&
    readCSRFCookie() === null
  ) {
    throw new Error(
      `CSRF cookie missing — cannot ${method} ${path}. ` +
        `Log in or complete onboarding first so the server can issue the __Host-csrf cookie.`,
    )
  }

  const res = await fetch(`${BASE_URL}/api/v1${path}`, {
    ...init,
    headers: buildHeaders(init?.headers),
  })
  if (!res.ok) {
    const text = await res.text().catch((e) => {
      console.warn('[api] Could not read response body:', e)
      return res.statusText
    })
    throw new Error(`${res.status}: ${text}`)
  }
  return res.json() as Promise<T>
}

// ── Agents ────────────────────────────────────────────────────────────────────

export interface Agent {
  id: string
  name: string
  description: string
  type: 'core' | 'custom'
  model: string
  status: 'active' | 'idle' | 'error' | 'draft'
  locked?: boolean
  icon?: string
  color?: string
  tools?: string[]
  tools_cfg?: AgentToolsCfg
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
  // Multi-agent session fields — present on sessions created with the joined
  // session model. For legacy single-agent sessions these are absent; callers
  // should fall back to [agent_id] when agent_ids is undefined.
  agent_ids?: string[]      // all agents that participated in this session
  active_agent_id?: string  // the agent currently handling this session
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
  agent_ids?: string[]
  active_agent_id?: string
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
    agent_ids: raw.agent_ids,
    active_agent_id: raw.active_agent_id,
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
    // Prompt guard strictness is owned by the dedicated /security/prompt-guard
    // endpoint since Wave 3. This field is still populated on read for
    // backward compatibility but must NOT be sent on updateConfig calls.
    prompt_injection_level?: 'off' | 'low' | 'medium' | 'high'
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
  tools?: {
    exec?: {
      enable_proxy?: boolean
    }
  }
  agents?: {
    defaults?: {
      default_agent_id?: string
    }
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

// cast provides a type-safe wrapper around the repetitive (raw.foo ?? fallback) as T pattern.
function cast<T>(obj: unknown, fallback: T): T {
  return (obj ?? fallback) as T
}

function rawToFrontendConfig(raw: Record<string, unknown>): Config {
  const gateway = cast<Record<string, unknown>>(raw.gateway, {})
  const storage = cast<Record<string, unknown>>(raw.storage, {})
  const retention = cast<Record<string, unknown>>(storage.retention, {})
  const security = cast<Record<string, unknown>>(raw.security, {})
  const rateLimits = cast<Record<string, unknown>>(security.rate_limits, {})
  const agents = cast<Record<string, unknown>>(raw.agents, {})
  const agentDefaults = cast<Record<string, unknown>>(agents.defaults, {})
  return {
    gateway: {
      bind_address: cast<string>(gateway.host, '127.0.0.1'),
      port: cast<number>(gateway.port, 8080),
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
      session_retention_days: cast<number>(retention.session_days, 90),
    },
    agents: {
      defaults: {
        default_agent_id: agentDefaults.default_agent_id as string | undefined,
      },
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

export function configureProvider(id: string, apiKey?: string, endpoint?: string, model?: string): Promise<Provider> {
  const body: Record<string, string> = {}
  if (apiKey !== undefined) body.api_key = apiKey
  if (endpoint !== undefined) body.endpoint = endpoint
  if (model !== undefined) body.model = model
  return request<Provider>(`/providers/${id}`, {
    method: 'PUT',
    body: JSON.stringify(body),
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
  session_id?: string
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

// ── Auth / Login ─────────────────────────────────────────────────────────────────

export interface LoginResponse {
  token: string
  role: UserRole
  username: string
}

export async function login(username: string, password: string): Promise<LoginResponse> {
  return request<LoginResponse>('/auth/login', {
    method: 'POST',
    body: JSON.stringify({ username, password }),
  })
}

export async function registerAdmin(username: string, password: string): Promise<LoginResponse> {
  return request<LoginResponse>('/auth/register-admin', {
    method: 'POST',
    body: JSON.stringify({ username, password }),
  })
}

export interface CompleteOnboardingRequest {
  provider: {
    id: string
    api_key: string
    model: string
  }
  admin: {
    username: string
    password: string
  }
}

export async function completeOnboardingTransaction(req: CompleteOnboardingRequest): Promise<LoginResponse> {
  return request<LoginResponse>('/onboarding/complete', {
    method: 'POST',
    body: JSON.stringify(req),
  })
}

// probeProvider is a non-persistent "test + fetch model list" call used during
// onboarding, before the __Host-csrf cookie can be issued. It accepts the
// api_key in the request body, asks the server to hit the provider's /models
// endpoint with that key, and returns both a success flag and the model list.
// Nothing is written to disk or in-memory config.
//
// After onboarding completes, the server returns HTTP 409 from this endpoint.
// Admins who want to add providers post-onboarding use configureProvider
// (PUT /providers/{id}) + fetchProviders (GET /providers) — both work
// because the browser has the __Host-csrf cookie at that point.
export interface ProbeProviderResponse {
  success: boolean
  models?: string[]
  error?: string
}
export async function probeProvider(
  id: string,
  apiKey: string,
  endpoint?: string,
): Promise<ProbeProviderResponse> {
  return request<ProbeProviderResponse>('/onboarding/probe-provider', {
    method: 'POST',
    body: JSON.stringify({ id, api_key: apiKey, endpoint: endpoint ?? '' }),
  })
}

export interface ValidateTokenResponse {
  username: string
  role: UserRole
}

export async function validateToken(): Promise<ValidateTokenResponse> {
  return request<ValidateTokenResponse>('/auth/validate')
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

// ── Devices ───────────────────────────────────────────────────────────────────

export interface DevicePending {
  device_id: string
  fingerprint: string
  pairing_code: string
  device_name: string
  created_at: string
  expires_at: string
}

export interface DevicePaired {
  device_id: string
  fingerprint: string
  device_name: string
  paired_at: string
  last_seen_at: string
  status: 'active' | 'revoked'
}

export interface DevicesResponse {
  pending: DevicePending[]
  paired: DevicePaired[]
}

export function fetchDevices(): Promise<DevicesResponse> {
  return request<DevicesResponse>('/devices')
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

export function renameSession(id: string, title: string): Promise<Session> {
  return request<Session>(`/sessions/${encodeURIComponent(id)}`, {
    method: 'PUT',
    body: JSON.stringify({ title }),
  })
}

export function deleteSession(id: string): Promise<{ success: boolean }> {
  return request<{ success: boolean }>(`/sessions/${encodeURIComponent(id)}`, { method: 'DELETE' })
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

export type AuditEventType = 'tool_call' | 'exec' | 'file_op' | 'llm_call' | 'policy_eval' | 'rate_limit' | 'ssrf' | 'startup' | 'shutdown'
export type AuditDecision = 'allow' | 'deny' | 'error'

export interface AuditEntry {
  timestamp: string
  event: AuditEventType | (string & {})
  decision?: AuditDecision | (string & {})
  agent_id?: string
  session_id?: string
  tool?: string
  command?: string
  parameters?: Record<string, unknown>
  policy_rule?: string
  details?: Record<string, unknown>
}

export function fetchAuditLog(): Promise<AuditEntry[]> {
  return request<AuditEntry[]>('/audit-log')
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

// ── RBAC / Me ─────────────────────────────────────────────────────────────────

export type UserRole = 'admin' | 'user'

export interface MeInfo {
  role: UserRole
}

export async function fetchMe(): Promise<MeInfo> {
  return request<MeInfo>('/me')
}

// ── File Upload ───────────────────────────────────────────────────────────────

export interface UploadedFile {
  name: string
  path: string
  size: number
  content_type: string
}

export async function uploadFiles(sessionId: string, files: File[]): Promise<{ files: UploadedFile[] }> {
  const formData = new FormData()
  formData.append('session_id', sessionId)
  for (const file of files) {
    formData.append('files', file)
  }
  const token = sessionStorage.getItem('omnipus_auth_token') ?? localStorage.getItem('omnipus_auth_token')
  const csrf = readCSRFCookie()
  // Upload is a state-changing POST — fail fast if we have no CSRF cookie
  // (see request() for the same pattern). We still send the header on the
  // off chance the user explicitly set the cookie externally.
  if (csrf === null) {
    throw new Error(
      'CSRF cookie missing — cannot upload files. Log in first so the server can issue the __Host-csrf cookie.',
    )
  }
  // Build headers by hand because FormData must NOT have a Content-Type
  // set — the browser needs to fill in the multipart boundary itself.
  const headers: Record<string, string> = {
    [CSRF_HEADER_NAME]: csrf,
  }
  if (token) {
    headers.Authorization = `Bearer ${token}`
  }
  const res = await fetch(`${BASE_URL}/api/v1/upload`, {
    method: 'POST',
    headers,
    body: formData,
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: res.statusText }))
    throw new Error(err.error || `Upload failed: ${res.status}`)
  }
  return res.json()
}

// ── Auth ──────────────────────────────────────────────────────────────────────

export function changePassword(currentPassword: string, newPassword: string): Promise<{ success: boolean }> {
  return request<{ success: boolean }>('/auth/change-password', {
    method: 'POST',
    body: JSON.stringify({ current_password: currentPassword, new_password: newPassword }),
  })
}

// ── Exec Allowlist ────────────────────────────────────────────────────────────

export interface ExecAllowlist {
  allowed_binaries: string[]
  // restart_required is true on a successful PUT: the in-memory agent loop
  // still uses the previous allowlist until Omnipus restarts (SEC-12). The UI
  // surfaces this via a "Restart required" badge in ExecAllowlistSection.
  restart_required?: boolean
}

export function fetchExecAllowlist(): Promise<ExecAllowlist> {
  return request<ExecAllowlist>('/security/exec-allowlist')
}

export function updateExecAllowlist(patterns: string[]): Promise<ExecAllowlist> {
  return request<ExecAllowlist>('/security/exec-allowlist', {
    method: 'PUT',
    body: JSON.stringify({ allowed_binaries: patterns }),
  })
}

// ── Prompt Guard ──────────────────────────────────────────────────────────────

export type PromptGuardStrictness = 'low' | 'medium' | 'high'

export interface PromptGuardConfig {
  strictness: PromptGuardStrictness
  restart_required?: boolean
}

export function fetchPromptGuard(): Promise<PromptGuardConfig> {
  return request<PromptGuardConfig>('/security/prompt-guard')
}

export function updatePromptGuard(strictness: PromptGuardStrictness): Promise<PromptGuardConfig> {
  return request<PromptGuardConfig>('/security/prompt-guard', {
    method: 'PUT',
    body: JSON.stringify({ strictness }),
  })
}

// ── Exec Proxy ────────────────────────────────────────────────────────────────

export interface ExecProxyStatus {
  running: boolean
  enabled: boolean
  address?: string
}

export function fetchExecProxyStatus(): Promise<ExecProxyStatus> {
  return request<ExecProxyStatus>('/security/exec-proxy-status')
}

// ── Rate Limits ───────────────────────────────────────────────────────────────

export interface RateLimitStatus {
  enabled: boolean
  daily_cost_usd: number
  daily_cost_cap: number
  max_agent_llm_calls_per_hour: number
  max_agent_tool_calls_per_minute: number
}

export function fetchRateLimits(): Promise<RateLimitStatus> {
  return request<RateLimitStatus>('/security/rate-limits')
}

// ── Agent Tools ───────────────────────────────────────────────────────────────

export interface BuiltinTool {
  name: string
  scope: 'system' | 'core' | 'general'
  category: string
  description: string
}

export interface AgentToolsCfg {
  builtin: {
    default_policy?: 'allow' | 'ask' | 'deny'
    policies?: Record<string, 'allow' | 'ask' | 'deny'>
    // Legacy fields (backward compat)
    mode?: 'explicit' | 'inherit'
    visible?: string[]
  }
  mcp?: { servers: { id: string; tools?: string[] }[] }
}

export function fetchBuiltinTools(): Promise<BuiltinTool[]> {
  return request<BuiltinTool[]>('/tools/builtin')
}

export function fetchMcpServersForAgent(): Promise<McpServer[]> {
  return request<McpServer[]>('/mcp-servers')
}

export function fetchAgentTools(agentId: string): Promise<{ config: AgentToolsCfg; effective_tools: BuiltinTool[] }> {
  return request<{ config: AgentToolsCfg; effective_tools: BuiltinTool[] }>(`/agents/${encodeURIComponent(agentId)}/tools`)
}

export function updateAgentTools(agentId: string, cfg: AgentToolsCfg): Promise<{ config: AgentToolsCfg; effective_tools: BuiltinTool[] }> {
  return request<{ config: AgentToolsCfg; effective_tools: BuiltinTool[] }>(`/agents/${encodeURIComponent(agentId)}/tools`, {
    method: 'PUT',
    body: JSON.stringify(cfg),
  })
}

// ── Global Tool Policies ──────────────────────────────────────────────────────

export interface GlobalToolPolicies {
  default_policy: 'allow' | 'ask' | 'deny'
  policies: Record<string, 'allow' | 'ask' | 'deny'>
}

export function fetchGlobalToolPolicies(): Promise<GlobalToolPolicies> {
  return request<GlobalToolPolicies>('/security/tool-policies')
}

export function updateGlobalToolPolicies(cfg: GlobalToolPolicies): Promise<GlobalToolPolicies> {
  return request<GlobalToolPolicies>('/security/tool-policies', {
    method: 'PUT',
    body: JSON.stringify(cfg),
  })
}

// ── Sandbox Status ────────────────────────────────────────────────────────────

export interface SandboxStatus {
  backend: string
  available: boolean
  // kernel_level reports the CAPABILITY — the backend can enforce at the
  // kernel level if Apply() is called. policy_applied reports whether the
  // enforcement is actually live on this process. A kernel-capable backend
  // without policy_applied has status notes explaining the gap.
  kernel_level: boolean
  policy_applied: boolean
  abi_version?: number
  blocked_syscalls?: string[]
  seccomp_enabled: boolean
  landlock_features?: string[]
  notes?: string[]
}

export function fetchSandboxStatus(): Promise<SandboxStatus> {
  return request<SandboxStatus>('/security/sandbox-status')
}

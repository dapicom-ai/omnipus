import { createFileRoute } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import {
  PuzzlePiece,
  HardDrives,
  Hash,
  Wrench,
  Shield,
} from '@phosphor-icons/react'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import { Badge } from '@/components/ui/badge'
import { fetchSkills, fetchMcpServers } from '@/lib/api'

// Built-in tools list — static, derived from BRD Appendix D
const BUILTIN_TOOLS = [
  { name: 'exec', category: 'system', description: 'Execute shell commands' },
  { name: 'file.read', category: 'system', description: 'Read file contents' },
  { name: 'file.write', category: 'system', description: 'Write file contents' },
  { name: 'file.list', category: 'system', description: 'List directory contents' },
  { name: 'web_search', category: 'web', description: 'Search the web' },
  { name: 'browser.navigate', category: 'web', description: 'Navigate to a URL' },
  { name: 'browser.screenshot', category: 'web', description: 'Capture browser screenshot' },
  { name: 'memory.read', category: 'memory', description: 'Read from agent memory' },
  { name: 'memory.write', category: 'memory', description: 'Write to agent memory' },
  { name: 'task.create', category: 'task', description: 'Create a new task' },
  { name: 'task.update', category: 'task', description: 'Update task status' },
]

// Available channels (BRD Appendix B)
const CHANNEL_DEFS = [
  { id: 'whatsapp', name: 'WhatsApp', transport: 'Go (compiled in)', description: 'WhatsApp messaging via whatsmeow' },
  { id: 'discord', name: 'Discord', transport: 'Go (compiled in)', description: 'Discord bot integration' },
  { id: 'telegram', name: 'Telegram', transport: 'Go (compiled in)', description: 'Telegram bot integration' },
  { id: 'slack', name: 'Slack', transport: 'Go (compiled in)', description: 'Slack app integration' },
  { id: 'nostr', name: 'Nostr', transport: 'Go (compiled in)', description: 'Nostr protocol integration' },
  { id: 'browser', name: 'Browser', transport: 'Go (compiled in)', description: 'Chromedp browser automation' },
]

function SkillsScreen() {
  const { data: skills = [], isLoading: skillsLoading } = useQuery({
    queryKey: ['skills'],
    queryFn: fetchSkills,
  })

  const { data: mcpServers = [], isLoading: mcpLoading } = useQuery({
    queryKey: ['mcp-servers'],
    queryFn: fetchMcpServers,
  })

  return (
    <div className="max-w-4xl mx-auto px-4 py-6">
      <div className="mb-6">
        <h1 className="font-headline text-2xl font-bold text-[var(--color-secondary)]">Skills & Tools</h1>
        <p className="text-sm text-[var(--color-muted)] mt-0.5">
          Manage agent capabilities — skills, MCP servers, channels, and built-in tools.
        </p>
      </div>

      <Tabs defaultValue="skills">
        <TabsList className="mb-6">
          <TabsTrigger value="skills" className="gap-1.5">
            <PuzzlePiece size={13} /> Installed Skills
          </TabsTrigger>
          <TabsTrigger value="mcp" className="gap-1.5">
            <HardDrives size={13} /> MCP Servers
          </TabsTrigger>
          <TabsTrigger value="channels" className="gap-1.5">
            <Hash size={13} /> Channels
          </TabsTrigger>
          <TabsTrigger value="builtins" className="gap-1.5">
            <Wrench size={13} /> Built-in Tools
          </TabsTrigger>
        </TabsList>

        {/* Installed Skills */}
        <TabsContent value="skills">
          {skillsLoading ? (
            <SkeletonList />
          ) : skills.length === 0 ? (
            <EmptyState icon={<PuzzlePiece size={40} weight="thin" />} message="No skills installed." />
          ) : (
            <div className="space-y-2">
              {skills.map((skill) => (
                <div
                  key={skill.id}
                  className="flex items-start gap-3 p-4 rounded-lg border border-[var(--color-border)] bg-[var(--color-surface-1)]"
                >
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2 flex-wrap">
                      <span className="font-medium text-sm text-[var(--color-secondary)]">{skill.name}</span>
                      <span className="font-mono text-[10px] text-[var(--color-muted)]">v{skill.version}</span>
                      {skill.verified && (
                        <Badge variant="success" className="gap-1 text-[10px]">
                          <Shield size={9} weight="fill" /> Verified
                        </Badge>
                      )}
                      <Badge
                        variant={skill.status === 'active' ? 'success' : skill.status === 'error' ? 'error' : 'muted'}
                        className="text-[10px]"
                      >
                        {skill.status}
                      </Badge>
                    </div>
                    <p className="text-xs text-[var(--color-muted)] mt-1">{skill.description}</p>
                    <div className="flex items-center gap-3 mt-1.5 text-[10px] text-[var(--color-muted)]">
                      <span>by {skill.author}</span>
                      {skill.agent_assignment && <span>→ {skill.agent_assignment}</span>}
                    </div>
                  </div>
                </div>
              ))}
            </div>
          )}
        </TabsContent>

        {/* MCP Servers */}
        <TabsContent value="mcp">
          {mcpLoading ? (
            <SkeletonList />
          ) : mcpServers.length === 0 ? (
            <EmptyState icon={<HardDrives size={40} weight="thin" />} message="No MCP servers connected." />
          ) : (
            <div className="space-y-2">
              {mcpServers.map((server) => (
                <div
                  key={server.id}
                  className="flex items-center gap-3 p-4 rounded-lg border border-[var(--color-border)] bg-[var(--color-surface-1)]"
                >
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2">
                      <span className="font-medium text-sm text-[var(--color-secondary)]">{server.name}</span>
                      <Badge variant="outline" className="text-[10px] font-mono">{server.transport}</Badge>
                      <Badge
                        variant={server.status === 'connected' ? 'success' : 'error'}
                        className="text-[10px]"
                      >
                        {server.status}
                      </Badge>
                    </div>
                  </div>
                  <span className="text-xs text-[var(--color-muted)] shrink-0">
                    {server.tool_count} tools
                  </span>
                </div>
              ))}
            </div>
          )}
        </TabsContent>

        {/* Channels */}
        <TabsContent value="channels">
          <div className="space-y-2">
            {CHANNEL_DEFS.map((channel) => (
              <div
                key={channel.id}
                className="flex items-center gap-3 p-4 rounded-lg border border-[var(--color-border)] bg-[var(--color-surface-1)]"
              >
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2 flex-wrap">
                    <span className="font-medium text-sm text-[var(--color-secondary)]">{channel.name}</span>
                    <Badge variant="outline" className="text-[10px] font-mono">{channel.transport}</Badge>
                    <Badge variant="muted" className="text-[10px]">Available</Badge>
                  </div>
                  <p className="text-xs text-[var(--color-muted)] mt-1">{channel.description}</p>
                </div>
                <button
                  type="button"
                  className="text-xs text-[var(--color-accent)] hover:text-[var(--color-accent-hover)] transition-colors shrink-0"
                >
                  Enable
                </button>
              </div>
            ))}
          </div>
        </TabsContent>

        {/* Built-in tools */}
        <TabsContent value="builtins">
          <div className="space-y-1.5">
            {BUILTIN_TOOLS.map((tool) => (
              <div
                key={tool.name}
                className="flex items-center gap-3 px-4 py-3 rounded-lg border border-[var(--color-border)] bg-[var(--color-surface-1)]"
              >
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="font-mono text-xs text-[var(--color-secondary)]">{tool.name}</span>
                    <Badge variant="muted" className="text-[10px]">{tool.category}</Badge>
                  </div>
                  <p className="text-[10px] text-[var(--color-muted)] mt-0.5">{tool.description}</p>
                </div>
              </div>
            ))}
          </div>
        </TabsContent>
      </Tabs>
    </div>
  )
}

function SkeletonList() {
  return (
    <div className="space-y-2">
      {[1, 2, 3].map((i) => (
        <div key={i} className="h-16 rounded-lg border border-[var(--color-border)] bg-[var(--color-surface-1)] animate-pulse" />
      ))}
    </div>
  )
}

function EmptyState({ icon, message }: { icon: React.ReactNode; message: string }) {
  return (
    <div className="flex flex-col items-center justify-center py-16 gap-3 text-center">
      <div className="text-[var(--color-border)]">{icon}</div>
      <p className="text-sm text-[var(--color-muted)]">{message}</p>
    </div>
  )
}

export const Route = createFileRoute('/_app/skills')({
  component: SkillsScreen,
})

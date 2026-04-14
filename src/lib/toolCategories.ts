import type { BuiltinTool } from '@/lib/api'
import type { ToolPolicy } from '@/components/shared/PolicyBadge'

export const CATEGORY_LABELS: Record<string, string> = {
  file: 'File & Code',
  code: 'Code Execution',
  web: 'Web & Search',
  browser: 'Browser Automation',
  communication: 'Communication',
  task: 'Task Management',
  automation: 'Automation',
  search: 'Search & Discovery',
  skills: 'Skills',
  hardware: 'Hardware (IoT)',
  system: 'System',
}

export function resolvePolicy(
  toolName: string,
  policies: Record<string, ToolPolicy> | undefined,
  defaultPolicy: ToolPolicy,
): ToolPolicy {
  return policies?.[toolName] ?? defaultPolicy
}

export function groupByCategory(tools: BuiltinTool[]): Record<string, BuiltinTool[]> {
  const groups: Record<string, BuiltinTool[]> = {}
  for (const t of tools) {
    const cat = t.category || 'other'
    if (!groups[cat]) groups[cat] = []
    groups[cat].push(t)
  }
  return groups
}

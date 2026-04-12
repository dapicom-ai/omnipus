import { Warning } from '@phosphor-icons/react'
import { Accordion, AccordionItem, AccordionTrigger, AccordionContent } from '@/components/ui/accordion'
import { Checkbox } from '@/components/ui/checkbox'
import type { BuiltinTool } from '@/lib/api'

interface ToolGroupListProps {
  tools: BuiltinTool[]
  selected: string[]
  agentType: 'system' | 'core' | 'custom'
  onToggle: (toolName: string) => void
}

export function ToolGroupList({ tools, selected, agentType, onToggle }: ToolGroupListProps) {
  // Hide system-scope tools for custom agents
  const visible = tools.filter((t) => !(agentType === 'custom' && t.scope === 'system'))

  // Group by category
  const grouped = visible.reduce<Record<string, BuiltinTool[]>>((acc, tool) => {
    const cat = tool.category || 'General'
    if (!acc[cat]) acc[cat] = []
    acc[cat].push(tool)
    return acc
  }, {})

  const categories = Object.keys(grouped).sort()

  if (categories.length === 0) {
    return (
      <p className="text-xs text-[var(--color-muted)] py-4 text-center">No tools available.</p>
    )
  }

  return (
    <Accordion type="multiple" defaultValue={categories}>
      {categories.map((cat) => {
        const catTools = grouped[cat]
        const selectedCount = catTools.filter((t) => selected.includes(t.name)).length
        return (
          <AccordionItem key={cat} value={cat}>
            <AccordionTrigger className="bg-[var(--color-surface-2)] px-3 rounded-t-md text-xs font-medium">
              <span>{cat}</span>
              <span className="text-[var(--color-muted)] font-normal ml-2 text-[10px]">
                {selectedCount}/{catTools.length}
              </span>
            </AccordionTrigger>
            <AccordionContent>
              <div className="space-y-0.5">
                {catTools.map((tool) => {
                  const isSelected = selected.includes(tool.name)
                  return (
                    <div
                      key={tool.name}
                      className="flex items-center gap-3 px-3 py-2 rounded-md bg-[var(--color-surface-1)] hover:bg-[var(--color-surface-2)] transition-colors cursor-pointer"
                      onClick={() => onToggle(tool.name)}
                    >
                      <Checkbox
                        checked={isSelected}
                        onCheckedChange={() => onToggle(tool.name)}
                        onClick={(e) => e.stopPropagation()}
                      />
                      <div className="flex items-center gap-1.5 min-w-0 flex-1">
                        {tool.scope === 'core' && (
                          <Warning
                            size={12}
                            weight="fill"
                            className="text-amber-500 shrink-0"
                            aria-label="Core-scope tool — affects agent behavior"
                          />
                        )}
                        <span className="font-mono text-xs text-[var(--color-secondary)] shrink-0">
                          {tool.name}
                        </span>
                        <span className="text-xs text-[var(--color-muted)] truncate">
                          {tool.description}
                        </span>
                      </div>
                    </div>
                  )
                })}
              </div>
            </AccordionContent>
          </AccordionItem>
        )
      })}
    </Accordion>
  )
}

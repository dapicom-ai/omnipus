// Omnipus — System Agent Tools
// License: MIT
// Copyright (c) 2026 Omnipus contributors

package systools

import (
	"github.com/dapicom-ai/omnipus/pkg/tools"
)

// AllTools returns all 35 system tools as a flat slice.
// The slice preserves the canonical tool ordering from BRD Appendix D §D.4.1.
func AllTools(d *Deps, navCb NavigateCallback) []tools.Tool {
	return []tools.Tool{
		// Agent management (6)
		NewAgentCreateTool(d),
		NewAgentUpdateTool(d),
		NewAgentDeleteTool(d),
		NewAgentListTool(d),
		NewAgentActivateTool(d),
		NewAgentDeactivateTool(d),

		// Project management (4)
		NewProjectCreateTool(d),
		NewProjectUpdateTool(d),
		NewProjectDeleteTool(d),
		NewProjectListTool(d),

		// Task management (4)
		NewTaskCreateTool(d),
		NewTaskUpdateTool(d),
		NewTaskDeleteTool(d),
		NewTaskListTool(d),

		// Channel management (5)
		NewChannelEnableTool(d),
		NewChannelConfigureTool(d),
		NewChannelDisableTool(d),
		NewChannelListTool(d),
		NewChannelTestTool(d),

		// Skill management (4)
		NewSkillInstallTool(d),
		NewSkillRemoveTool(d),
		NewSkillSearchTool(d),
		NewSkillListTool(d),

		// MCP server management (3)
		NewMCPAddTool(d),
		NewMCPRemoveTool(d),
		NewMCPListTool(d),

		// Provider management (3)
		NewProviderConfigureTool(d),
		NewProviderListTool(d),
		NewProviderTestTool(d),

		// Pin management (3)
		NewPinListTool(d),
		NewPinCreateTool(d),
		NewPinDeleteTool(d),

		// Config (2)
		NewConfigGetTool(d),
		NewConfigSetTool(d),

		// Diagnostics / utility (4)
		NewDoctorRunTool(d),
		NewBackupCreateTool(d),
		NewCostQueryTool(d),
		NewNavigateTool(d, navCb),
	}
}

// BuildRegistry creates a ToolRegistry containing all 35 system tools.
// Use this registry as the backing store for the SystemToolHandler.
func BuildRegistry(d *Deps, navCb NavigateCallback) *tools.ToolRegistry {
	reg := tools.NewToolRegistry()
	for _, t := range AllTools(d, navCb) {
		reg.Register(t)
	}
	return reg
}

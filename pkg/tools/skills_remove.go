package tools

import (
	"context"
	"fmt"

	"github.com/dapicom-ai/omnipus/pkg/skills"
	"github.com/dapicom-ai/omnipus/pkg/utils"
)

// RemoveSkillTool allows the LLM agent to remove an installed skill.
type RemoveSkillTool struct {
	installer *skills.SkillInstaller
}

// NewRemoveSkillTool creates a new RemoveSkillTool.
func NewRemoveSkillTool(installer *skills.SkillInstaller) *RemoveSkillTool {
	return &RemoveSkillTool{installer: installer}
}

func (t *RemoveSkillTool) Name() string {
	return "remove_skill"
}

func (t *RemoveSkillTool) Description() string {
	return "Remove an installed skill by name. The skill's directory is deleted and it becomes unavailable to the agent."
}

func (t *RemoveSkillTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{
				"type":        "string",
				"description": "The name of the installed skill to remove (e.g., 'aws-cost-analyzer')",
			},
		},
		"required": []string{"name"},
	}
}

func (t *RemoveSkillTool) Execute(_ context.Context, args map[string]any) *ToolResult {
	name, _ := args["name"].(string)
	if err := utils.ValidateSkillIdentifier(name); err != nil {
		return ErrorResult(fmt.Sprintf("invalid skill name %q: %v", name, err))
	}

	if err := t.installer.Uninstall(name); err != nil {
		return ErrorResult(fmt.Sprintf("failed to remove skill %q: %v", name, err))
	}

	return SilentResult(fmt.Sprintf("Skill %q removed successfully.", name))
}

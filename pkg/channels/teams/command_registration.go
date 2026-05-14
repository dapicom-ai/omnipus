package teams

import (
	"context"
	"strings"

	"github.com/dapicom-ai/omnipus/pkg/commands"
	"github.com/dapicom-ai/omnipus/pkg/logger"
)

// teamsCommandManifestNote is the guidance logged at startup so operators know
// exactly what to add to their Teams App manifest.json.
// Microsoft Teams bot commands are declared in the app manifest (manifest.json)
// under the `bots[].commandLists` array and cannot be registered programmatically
// via a REST API in the current Bot Framework model.  The pragmatic v0.1 approach
// is to log the expected command set and return nil so startup is never blocked
// (FR-28: fall back to text parsing when platform registration is unavailable).
const teamsCommandManifestNote = `Teams bot commands must be declared in the app manifest.json under
bots[].commandLists[].commands.  Add each command below with the appropriate
scopes ("personal", "team", or "groupchat") and redeploy the app package to
the Teams Admin Center.`

// RegisterCommands implements channels.CommandRegistrarCapable for Teams.
// Because Microsoft Teams does not expose a programmatic API for registering
// bot command menus (commands are declared statically in the app manifest),
// this implementation logs the expected command set so operators know what to
// add to their manifest.json.  It always returns nil so the channel starts
// successfully and falls back to text parsing (FR-28).
func (c *TeamsChannel) RegisterCommands(_ context.Context, defs []commands.Definition) error {
	type entry struct {
		title       string
		description string
	}
	var entries []string
	for _, def := range defs {
		if def.Name == "" || def.Description == "" {
			continue
		}
		entries = append(entries, "/"+def.Name+" — "+def.Description)
	}

	if len(entries) == 0 {
		return nil
	}

	logger.InfoCF("teams", "Teams command registration (operator action required)", map[string]any{
		"note":     teamsCommandManifestNote,
		"commands": strings.Join(entries, "; "),
		"count":    len(entries),
	})
	return nil
}

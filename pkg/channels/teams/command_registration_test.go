package teams

import (
	"context"
	"testing"

	"github.com/dapicom-ai/omnipus/pkg/channels"
	"github.com/dapicom-ai/omnipus/pkg/commands"
)

// TestTeamsChannel_ImplementsCommandRegistrarCapable verifies the interface is
// satisfied at compile time.
func TestTeamsChannel_ImplementsCommandRegistrarCapable(t *testing.T) {
	var _ channels.CommandRegistrarCapable = (*TeamsChannel)(nil)
}

// TestTeamsRegisterCommands_ReturnsNil is T12d: verifies graceful no-op with
// the /cancel definition.
func TestTeamsRegisterCommands_ReturnsNil(t *testing.T) {
	ch := &TeamsChannel{}
	defs := []commands.Definition{
		{Name: "cancel", Description: "Cancel the running agent task"},
	}
	if err := ch.RegisterCommands(context.Background(), defs); err != nil {
		t.Fatalf("RegisterCommands() returned unexpected error: %v", err)
	}
}

// TestTeamsRegisterCommands_SkipsIncomplete verifies that definitions missing
// Name or Description are silently filtered out (Telegram pattern).
func TestTeamsRegisterCommands_SkipsIncomplete(t *testing.T) {
	ch := &TeamsChannel{}
	defs := []commands.Definition{
		{Name: "", Description: "no name"},
		{Name: "noDesc", Description: ""},
		{Name: "cancel", Description: "Cancel the running agent task"},
	}
	if err := ch.RegisterCommands(context.Background(), defs); err != nil {
		t.Fatalf("RegisterCommands() with incomplete defs returned error: %v", err)
	}
}

// TestTeamsRegisterCommands_EmptyList verifies that an empty definition list
// returns nil without logging noise.
func TestTeamsRegisterCommands_EmptyList(t *testing.T) {
	ch := &TeamsChannel{}
	if err := ch.RegisterCommands(context.Background(), nil); err != nil {
		t.Fatalf("RegisterCommands(nil) returned unexpected error: %v", err)
	}
}

// TestTeamsRegisterCommands_WithBuiltinCancel passes the full builtin list
// (T12d — uses real builtin list, same as Slack/Discord pattern).
func TestTeamsRegisterCommands_WithBuiltinCancel(t *testing.T) {
	allDefs := commands.BuiltinDefinitions()

	ch := &TeamsChannel{}
	if err := ch.RegisterCommands(context.Background(), allDefs); err != nil {
		t.Fatalf("RegisterCommands(BuiltinDefinitions()) returned error: %v", err)
	}
}

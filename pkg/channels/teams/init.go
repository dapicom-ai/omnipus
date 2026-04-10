package teams

import (
	"github.com/dapicom-ai/omnipus/pkg/bus"
	"github.com/dapicom-ai/omnipus/pkg/channels"
	"github.com/dapicom-ai/omnipus/pkg/config"
)

func init() {
	channels.RegisterFactory("teams", func(cfg *config.Config, b *bus.MessageBus) (channels.Channel, error) {
		return NewTeamsChannel(cfg.Channels.Teams, b)
	})
}

package whatsapp

import (
	"github.com/dapicom-ai/omnipus/pkg/bus"
	"github.com/dapicom-ai/omnipus/pkg/channels"
	"github.com/dapicom-ai/omnipus/pkg/config"
	"github.com/dapicom-ai/omnipus/pkg/credentials"
)

func init() {
	channels.RegisterFactory(
		"whatsapp",
		func(cfg *config.Config, _ credentials.SecretBundle, b *bus.MessageBus) (channels.Channel, error) {
			return NewWhatsAppChannel(cfg.Channels.WhatsApp, b)
		},
	)
}

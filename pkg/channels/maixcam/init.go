package maixcam

import (
	"github.com/dapicom-ai/omnipus/pkg/bus"
	"github.com/dapicom-ai/omnipus/pkg/channels"
	"github.com/dapicom-ai/omnipus/pkg/config"
	"github.com/dapicom-ai/omnipus/pkg/credentials"
)

func init() {
	channels.RegisterFactory(
		"maixcam",
		func(cfg *config.Config, _ credentials.SecretBundle, b *bus.MessageBus) (channels.Channel, error) {
			return NewMaixCamChannel(cfg.Channels.MaixCam, b)
		},
	)
}

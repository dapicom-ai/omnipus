//go:build cgo

package matrix

import (
	"path/filepath"

	"github.com/dapicom-ai/omnipus/pkg/bus"
	"github.com/dapicom-ai/omnipus/pkg/channels"
	"github.com/dapicom-ai/omnipus/pkg/config"
)

func init() {
	channels.RegisterFactory("matrix", func(cfg *config.Config, b *bus.MessageBus) (channels.Channel, error) {
		matrixCfg := cfg.Channels.Matrix
		cryptoDatabasePath := matrixCfg.CryptoDatabasePath
		if cryptoDatabasePath == "" {
			cryptoDatabasePath = filepath.Join(cfg.WorkspacePath(), "matrix")
		}
		return NewMatrixChannel(matrixCfg, b, cryptoDatabasePath)
	})
}

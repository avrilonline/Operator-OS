package qq

import (
	"github.com/standardws/operator/pkg/bus"
	"github.com/standardws/operator/pkg/channels"
	"github.com/standardws/operator/pkg/config"
)

func init() {
	channels.RegisterFactory("qq", func(cfg *config.Config, b *bus.MessageBus) (channels.Channel, error) {
		return NewQQChannel(cfg.Channels.QQ, b)
	})
}

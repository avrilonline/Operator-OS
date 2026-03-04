package pico

import (
	"github.com/standardws/operator/pkg/bus"
	"github.com/standardws/operator/pkg/channels"
	"github.com/standardws/operator/pkg/config"
)

func init() {
	channels.RegisterFactory("pico", func(cfg *config.Config, b *bus.MessageBus) (channels.Channel, error) {
		return NewPicoChannel(cfg.Channels.Pico, b)
	})
}

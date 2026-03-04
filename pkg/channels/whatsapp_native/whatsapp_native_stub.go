//go:build !whatsapp_native

package whatsapp

import (
	"fmt"

	"github.com/standardws/operator/pkg/bus"
	"github.com/standardws/operator/pkg/channels"
	"github.com/standardws/operator/pkg/config"
)

// NewWhatsAppNativeChannel returns an error when the binary was not built with -tags whatsapp_native.
// Build with: go build -tags whatsapp_native ./cmd/...
func NewWhatsAppNativeChannel(
	cfg config.WhatsAppConfig,
	bus *bus.MessageBus,
	storePath string,
) (channels.Channel, error) {
	return nil, fmt.Errorf("whatsapp native not compiled in; build with -tags whatsapp_native")
}

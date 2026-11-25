package whatsapp

import (
	"context"
	"os"

	"github.com/mdp/qrterminal/v3"
	"go.mau.fi/whatsmeow"
)

func (b *Bot) showQR(ctx context.Context, items <-chan whatsmeow.QRChannelItem) {
	for {
		select {
		case <-ctx.Done():
			return
		case item, ok := <-items:
			if !ok {
				return
			}
			switch item.Event {
			case "code":
				b.logger.Info("Scan this QR code with WhatsApp linked devices")
				qrterminal.GenerateWithConfig(item.Code, qrterminal.Config{
					Level:      qrterminal.M,
					Writer:     os.Stdout,
					HalfBlocks: true,
					QuietZone:  1,
				})
			case "error":
				b.logger.Error("WhatsApp pairing failed", "error", item.Error)
			case "success":
				b.logger.Info("WhatsApp pairing completed")
			}
		}
	}
}

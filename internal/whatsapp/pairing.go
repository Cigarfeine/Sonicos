package whatsapp

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/mdp/qrterminal/v3"
	"go.mau.fi/whatsmeow"
)

func (b *Bot) cancelPairingSession() {
	b.pairMu.Lock()
	cancel := b.qrCancel
	b.qrCancel = nil
	b.pairMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (b *Bot) startPairingSession(parentCtx context.Context) error {
	b.sessionOpMu.Lock()
	defer b.sessionOpMu.Unlock()

	if b.client.Store.ID != nil {
		return ErrAlreadyConnected
	}

	b.cancelPairingSession()
	if b.client.IsConnected() {
		b.client.Disconnect()
	}

	qrCtx, cancel := context.WithCancel(parentCtx)
	readyCh := make(chan struct{})

	b.pairMu.Lock()
	b.qrCancel = cancel
	b.qrReady = readyCh
	b.qrSnap = QRSnapshot{State: "waiting"}
	b.pairMu.Unlock()

	qrChannel, err := b.client.GetQRChannel(qrCtx)
	if err != nil {
		cancel()
		return fmt.Errorf("get QR channel: %w", err)
	}

	b.markConnecting()
	if err := b.client.ConnectContext(qrCtx); err != nil {
		cancel()
		return fmt.Errorf("connect for pairing: %w", err)
	}

	go b.consumeQR(qrCtx, qrChannel, readyCh)
	b.emitEvent("status", nil)
	return nil
}

func (b *Bot) consumeQR(ctx context.Context, items <-chan whatsmeow.QRChannelItem, readyCh chan struct{}) {
	var readyOnce sync.Once
	closeReady := func() {
		readyOnce.Do(func() {
			close(readyCh)
		})
	}
	defer closeReady()

	for {
		select {
		case <-ctx.Done():
			return
		case item, ok := <-items:
			if !ok {
				return
			}
			switch {
			case item.Event == "code":
				b.logger.Info("Scan this QR code with WhatsApp linked devices")
				qrterminal.GenerateWithConfig(item.Code, qrterminal.Config{
					Level:      qrterminal.M,
					Writer:     os.Stdout,
					HalfBlocks: true,
					QuietZone:  1,
				})
				timeout := item.Timeout
				if timeout <= 0 {
					timeout = 20 * time.Second
				}
				snap := QRSnapshot{
					Code:      item.Code,
					State:     "qr",
					ExpiresAt: time.Now().UTC().Add(timeout),
				}
				b.pairMu.Lock()
				if b.qrReady == readyCh {
					b.qrSnap = snap
				}
				b.pairMu.Unlock()
				closeReady()
				b.emitEvent("qr", snap)
				b.emitEvent("status", nil)

			case item.Event == whatsmeow.QRChannelSuccess.Event:
				snap := QRSnapshot{State: "connected"}
				b.pairMu.Lock()
				if b.qrReady == readyCh {
					b.qrSnap = snap
				}
				b.pairMu.Unlock()
				closeReady()
				b.logger.Info("WhatsApp pairing completed")
				b.emitEvent("status", nil)
				return

			case item.Event == whatsmeow.QRChannelTimeout.Event:
				snap := QRSnapshot{
					State: "disconnected",
					Error: "QR code timed out. Click Refresh QR to generate a new one.",
				}
				b.pairMu.Lock()
				if b.qrReady == readyCh {
					b.qrSnap = snap
				}
				b.pairMu.Unlock()
				closeReady()
				b.logger.Info("WhatsApp QR pairing timed out")
				b.emitEvent("qr", snap)
				b.emitEvent("status", nil)
				return

			case item.Event == "error" || strings.HasPrefix(item.Event, "err-"):
				errMsg := item.Event
				if item.Error != nil {
					errMsg = item.Error.Error()
				}
				snap := QRSnapshot{
					State: "error",
					Error: errMsg,
				}
				b.pairMu.Lock()
				if b.qrReady == readyCh {
					b.qrSnap = snap
				}
				b.pairMu.Unlock()
				closeReady()
				b.logger.Error("WhatsApp pairing failed", "event", item.Event, "error", errMsg)
				b.emitEvent("qr", snap)
				b.emitEvent("status", nil)
				return
			}
		}
	}
}

package whatsapp

import (
	"context"
	"errors"
	"fmt"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

var (
	errAlreadyConnected = errors.New("already connected")
	errNoQR             = errors.New("no QR available")
)

type groupInfo struct {
	JID          string
	Name         string
	Participants []struct {
		JID      string
		IsAdmin  bool
	}
}

type groupParticipantInfo struct {
	JID         string   `json:"jid"`
	Name        string   `json:"name"`
	Groups      []string `json:"groups"`
	GroupNames  []string `json:"group_names"`
	IsAdmin     bool     `json:"is_admin"`
}

// ClientState returns the current WhatsApp connection state as a string.
func (b *Bot) ClientState() string {
	if b.client.IsConnected() {
		if b.client.Store.ID != nil {
			return "connected"
		}
		return "connecting"
	}
	return "disconnected"
}

// DeviceID returns the connected device JID, or nil if not connected.
func (b *Bot) DeviceID() *types.JID {
	if b.client.Store.ID != nil {
		return b.client.Store.ID
	}
	return nil
}

// QRCode returns the current QR code data and state.
func (b *Bot) QRCode() (string, string, error) {
	if b.client.Store.ID != nil {
		return "", "connected", errAlreadyConnected
	}

	qrChannel, err := b.client.GetQRChannel(context.Background())
	if err != nil {
		return "", "disconnected", errNoQR
	}

	select {
	case item, ok := <-qrChannel:
		if !ok {
			return "", "disconnected", errNoQR
		}
		switch item.Event {
		case "code":
			return item.Code, "qr", nil
		case "error":
			return "", "error", fmt.Errorf("qr error: %w", item.Error)
		case "success":
			return "", "success", nil
		default:
			return "", "unknown", nil
		}
	default:
		return "", "waiting", nil
	}
}

// RefreshQR requests a new QR code from WhatsApp.
func (b *Bot) RefreshQR() error {
	if b.client.Store.ID != nil {
		return errAlreadyConnected
	}
	qrChannel, err := b.client.GetQRChannel(context.Background())
	if err != nil {
		return err
	}
	if qrChannel == nil {
		return errNoQR
	}

	go func() {
		for range qrChannel {
			// drain
		}
	}()

	return nil
}

// RequestPairing initiates phone number pairing with WhatsApp.
func (b *Bot) RequestPairing(countryCode, phoneNumber string) (string, string, error) {
	if b.client.Store.ID != nil {
		return "", "", errAlreadyConnected
	}

	phone := countryCode + phoneNumber
	code, err := b.client.PairPhone(context.Background(), phone, true, whatsmeow.PairClientUnknown, "Sonicos Dashboard")
	if err != nil {
		return "", "", fmt.Errorf("pairing failed: %w", err)
	}

	return code, "", nil
}

// Reconnect attempts to reconnect the WhatsApp client.
func (b *Bot) Reconnect() error {
	if b.client.IsConnected() {
		return nil
	}
	return b.client.Connect()
}

// Disconnect disconnects the WhatsApp client but keeps the session.
func (b *Bot) Disconnect() error {
	b.client.Disconnect()
	return nil
}

// Unlink logs out the client and removes the session.
func (b *Bot) Unlink() error {
	return b.client.Logout(context.Background())
}

// ListGroups returns all groups the bot is a member of.
func (b *Bot) ListGroups() []groupInfo {
	joined, err := b.client.GetJoinedGroups(context.Background())
	if err != nil {
		return nil
	}
	out := make([]groupInfo, 0, len(joined))
	for _, g := range joined {
		info := groupInfo{JID: g.JID.String(), Name: g.Name}
		out = append(out, info)
	}
	return out
}

// GetGroup returns information about a specific group.
func (b *Bot) GetGroup(jid string) *groupInfo {
	parsed, err := types.ParseJID(jid)
	if err != nil {
		return nil
	}
	info, err := b.client.GetGroupInfo(context.Background(), parsed)
	if err != nil {
		return nil
	}
	return &groupInfo{
		JID: info.JID.String(),
		Name: info.Name,
	}
}

// ListUsers returns unique users across all groups the bot is a member of.
func (b *Bot) ListUsers() []groupParticipantInfo {
	joined, err := b.client.GetJoinedGroups(context.Background())
	if err != nil {
		return nil
	}
	seen := make(map[string]groupParticipantInfo)
	botID := ""
	if b.client.Store.ID != nil {
		botID = b.client.Store.ID.String()
	}
	for _, g := range joined {
		parsed, err := types.ParseJID(g.JID.String())
		if err != nil {
			continue
		}
		info, err := b.client.GetGroupInfo(context.Background(), parsed)
		if err != nil {
			continue
		}
		for _, p := range info.Participants {
			jid := p.JID.String()
			if jid == botID {
				continue
			}
			existing, ok := seen[jid]
			if !ok {
				seen[jid] = groupParticipantInfo{
					JID:        jid,
					Name:       "",
					Groups:     []string{g.JID.String()},
					GroupNames: []string{g.Name},
					IsAdmin:    p.IsAdmin,
				}
			} else {
				existing.Groups = append(existing.Groups, g.JID.String())
				existing.GroupNames = append(existing.GroupNames, g.Name)
				if p.IsAdmin {
					existing.IsAdmin = true
				}
				seen[jid] = existing
			}
		}
	}
	out := make([]groupParticipantInfo, 0, len(seen))
	for _, v := range seen {
		out = append(out, v)
	}
	return out
}
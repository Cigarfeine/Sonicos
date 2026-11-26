package whatsapp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

var (
	// ErrAlreadyConnected indicates WhatsApp is already linked to a device session.
	ErrAlreadyConnected = errors.New("already connected")
	// ErrNoQR indicates no QR payload is currently available.
	ErrNoQR = errors.New("no QR available")
)

// QRSnapshot captures the latest WhatsApp QR/pairing state for the dashboard and SSE stream.
type QRSnapshot struct {
	Code      string
	State     string
	ExpiresAt time.Time
	Error     string
}

// EventListener receives asynchronous WhatsApp lifecycle, QR, and pairing updates.
type EventListener func(event string, data any)

type GroupParticipant struct {
	JID     string
	IsAdmin bool
}

type GroupInfo struct {
	JID          string
	Name         string
	Participants []GroupParticipant
}

type GroupParticipantInfo struct {
	JID        string   `json:"jid"`
	Name       string   `json:"name"`
	Groups     []string `json:"groups"`
	GroupNames []string `json:"group_names"`
	IsAdmin    bool     `json:"is_admin"`
}

// ClientState returns the current WhatsApp connection state as a string.
func (b *Bot) ClientState() string {
	if b.client.IsConnected() && b.client.Store.ID != nil {
		return "connected"
	}
	if b.client.Store.ID == nil {
		b.pairMu.RLock()
		st := b.qrSnap.State
		b.pairMu.RUnlock()
		if st != "" {
			return st
		}
		if b.client.IsConnected() {
			return "connecting"
		}
	} else if b.client.IsConnected() {
		return "connected"
	}
	return "disconnected"
}

// DeviceID returns the connected device JID, or nil if not linked.
func (b *Bot) DeviceID() *types.JID {
	if b.client.Store.ID != nil {
		return b.client.Store.ID
	}
	return nil
}

// ConnectedSince returns when the current WhatsApp session connected, or zero if offline.
func (b *Bot) ConnectedSince() time.Time {
	b.pairMu.RLock()
	defer b.pairMu.RUnlock()
	return b.connectedAt
}

// QRCode returns the latest cached QR snapshot from the active pairing session.
func (b *Bot) QRCode() (QRSnapshot, error) {
	if b.client.Store.ID != nil {
		return QRSnapshot{State: "connected"}, ErrAlreadyConnected
	}

	b.pairMu.RLock()
	snap := b.qrSnap
	b.pairMu.RUnlock()

	if snap.Code == "" && (snap.State == "" || snap.State == "disconnected") {
		return snap, ErrNoQR
	}
	return snap, nil
}

// RefreshQR restarts the unauthenticated pairing session to generate a fresh QR code.
func (b *Bot) RefreshQR() error {
	if b.client.Store.ID != nil {
		return ErrAlreadyConnected
	}
	return b.startPairingSession(b.baseCtx())
}

// RequestPairing initiates phone number pairing with WhatsApp and returns the 8-character code and expiry.
func (b *Bot) RequestPairing(ctx context.Context, countryCode, phoneNumber string) (string, time.Time, error) {
	if b.client.Store.ID != nil {
		return "", time.Time{}, ErrAlreadyConnected
	}

	phone, err := sanitizePhone(countryCode, phoneNumber)
	if err != nil {
		return "", time.Time{}, err
	}

	b.pairMu.RLock()
	st := b.qrSnap.State
	readyCh := b.qrReady
	b.pairMu.RUnlock()

	if !b.client.IsConnected() || st == "disconnected" || st == "error" || st == "" || readyCh == nil {
		if err := b.startPairingSession(b.baseCtx()); err != nil {
			b.emitEvent("pairing", map[string]any{"state": "failed", "error": err.Error()})
			return "", time.Time{}, fmt.Errorf("connect pairing socket: %w", err)
		}
		b.pairMu.RLock()
		readyCh = b.qrReady
		b.pairMu.RUnlock()
	}

	if readyCh != nil {
		waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		select {
		case <-readyCh:
		case <-waitCtx.Done():
		}
		cancel()
	}

	pairCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	code, err := b.client.PairPhone(pairCtx, phone, true, whatsmeow.PairClientChrome, "Chrome (Linux)")
	if err != nil {
		b.emitEvent("pairing", map[string]any{"state": "failed", "error": err.Error()})
		return "", time.Time{}, fmt.Errorf("pairing failed: %w", err)
	}

	expiresAt := time.Now().UTC().Add(160 * time.Second)
	b.pairMu.Lock()
	b.qrSnap.State = "pairing"
	b.pairMu.Unlock()

	b.emitEvent("pairing", map[string]any{"state": "pairing", "expires_at": expiresAt.Format(time.RFC3339)})
	b.emitEvent("status", nil)
	return code, expiresAt, nil
}

// Reconnect attempts to reconnect the WhatsApp client.
func (b *Bot) Reconnect() error {
	if b.client.IsConnected() {
		return nil
	}
	if b.client.Store.ID == nil {
		return b.startPairingSession(b.baseCtx())
	}
	b.markConnecting()
	return b.client.Connect()
}

// Disconnect disconnects the WhatsApp client while keeping the stored session.
func (b *Bot) Disconnect() error {
	b.cancelPairingSession()
	b.client.Disconnect()
	b.markDisconnected()
	b.pairMu.Lock()
	b.connectedAt = time.Time{}
	b.qrSnap = QRSnapshot{State: "disconnected"}
	b.pairMu.Unlock()
	b.emitEvent("status", nil)
	return nil
}

// Unlink logs out the client, removes the session, and starts a fresh pairing session.
func (b *Bot) Unlink() error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := b.client.Logout(ctx); err != nil {
		return err
	}
	b.markDisconnected()
	b.pairMu.Lock()
	b.connectedAt = time.Time{}
	b.qrSnap = QRSnapshot{State: "disconnected"}
	b.pairMu.Unlock()
	b.emitEvent("status", nil)

	if err := b.startPairingSession(b.baseCtx()); err != nil {
		b.logger.Warn("failed to start pairing session after unlink", "error", err)
	}
	return nil
}

// ListGroups returns all groups the bot is a member of.
func (b *Bot) ListGroups() []GroupInfo {
	if !b.client.IsConnected() || b.client.Store.ID == nil {
		return nil
	}
	joined, err := b.client.GetJoinedGroups(context.Background())
	if err != nil {
		return nil
	}
	out := make([]GroupInfo, 0, len(joined))
	for _, g := range joined {
		participants := make([]GroupParticipant, 0, len(g.Participants))
		for _, p := range g.Participants {
			participants = append(participants, GroupParticipant{
				JID:     p.JID.String(),
				IsAdmin: p.IsAdmin || p.IsSuperAdmin,
			})
		}
		out = append(out, GroupInfo{
			JID:          g.JID.String(),
			Name:         g.Name,
			Participants: participants,
		})
	}
	return out
}

// GetGroup returns information about a specific group.
func (b *Bot) GetGroup(jid string) *GroupInfo {
	if !b.client.IsConnected() || b.client.Store.ID == nil {
		return nil
	}
	parsed, err := types.ParseJID(jid)
	if err != nil {
		return nil
	}
	info, err := b.client.GetGroupInfo(context.Background(), parsed)
	if err != nil {
		return nil
	}
	participants := make([]GroupParticipant, 0, len(info.Participants))
	for _, p := range info.Participants {
		participants = append(participants, GroupParticipant{
			JID:     p.JID.String(),
			IsAdmin: p.IsAdmin || p.IsSuperAdmin,
		})
	}
	return &GroupInfo{
		JID:          info.JID.String(),
		Name:         info.Name,
		Participants: participants,
	}
}

// ListUsers returns unique users across all groups the bot is a member of.
func (b *Bot) ListUsers() []GroupParticipantInfo {
	if !b.client.IsConnected() || b.client.Store.ID == nil {
		return nil
	}
	joined, err := b.client.GetJoinedGroups(context.Background())
	if err != nil {
		return nil
	}
	seen := make(map[string]GroupParticipantInfo)
	botID := ""
	if b.client.Store.ID != nil {
		botID = b.client.Store.ID.ToNonAD().String()
	}
	for _, g := range joined {
		for _, p := range g.Participants {
			jid := p.JID.ToNonAD().String()
			if jid == botID {
				continue
			}
			isAdmin := p.IsAdmin || p.IsSuperAdmin
			existing, ok := seen[jid]
			if !ok {
				seen[jid] = GroupParticipantInfo{
					JID:        jid,
					Name:       "",
					Groups:     []string{g.JID.String()},
					GroupNames: []string{g.Name},
					IsAdmin:    isAdmin,
				}
			} else {
				existing.Groups = append(existing.Groups, g.JID.String())
				existing.GroupNames = append(existing.GroupNames, g.Name)
				if isAdmin {
					existing.IsAdmin = true
				}
				seen[jid] = existing
			}
		}
	}
	out := make([]GroupParticipantInfo, 0, len(seen))
	for _, v := range seen {
		out = append(out, v)
	}
	return out
}

func sanitizePhone(countryCode, phoneNumber string) (string, error) {
	cc := cleanPhoneDigits(countryCode)
	num := strings.TrimLeft(cleanPhoneDigits(phoneNumber), "0")
	if cc == "" || num == "" {
		return "", errors.New("country_code and phone_number must contain digits")
	}
	if strings.HasPrefix(num, cc) && len(num) > len(cc)+6 {
		num = num[len(cc):]
	}
	full := cc + num
	if len(full) < 7 || len(full) > 15 {
		return "", errors.New("phone number must be 7 to 15 digits including country code")
	}
	return full, nil
}

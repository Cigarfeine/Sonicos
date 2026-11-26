package whatsapp

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/SafwanGanz/Sonicos/internal/command"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

const (
	groupInfoCacheTTL        = 30 * time.Second
	groupInfoCacheMaxEntries = 500
)

type cachedGroupInfo struct {
	info      command.GroupInfo
	expiresAt time.Time
}

type groupService struct {
	client *whatsmeow.Client
	mu     sync.RWMutex
	cache  map[string]cachedGroupInfo
}

func newGroupService(client *whatsmeow.Client) command.GroupService {
	if gs := newGroupServiceCache(client); gs != nil {
		return gs
	}
	return nil
}

func newGroupServiceCache(client *whatsmeow.Client) *groupService {
	if client == nil {
		return nil
	}
	return &groupService{
		client: client,
		cache:  make(map[string]cachedGroupInfo),
	}
}

func convertGroupInfo(client *whatsmeow.Client, info *types.GroupInfo) command.GroupInfo {
	participants := make([]command.GroupParticipant, 0, len(info.Participants))
	for _, p := range info.Participants {
		participants = append(participants, command.GroupParticipant{
			JID:          p.JID.String(),
			PhoneNumber:  p.PhoneNumber.String(),
			IsAdmin:      p.IsAdmin || p.IsSuperAdmin,
			IsSuperAdmin: p.IsSuperAdmin,
		})
	}
	var botJID string
	if client != nil && client.Store != nil && client.Store.ID != nil {
		botJID = client.Store.ID.ToNonAD().String()
	}
	return command.GroupInfo{
		JID:          info.JID.String(),
		Name:         info.Name,
		Topic:        info.Topic,
		Owner:        info.OwnerJID.String(),
		BotJID:       botJID,
		Locked:       info.IsLocked,
		Announce:     info.IsAnnounce,
		Approval:     info.IsJoinApprovalRequired,
		EphemeralSec: info.DisappearingTimer,
		MemberAddAll: info.MemberAddMode == types.GroupMemberAddModeAllMember,
		Participants: participants,
	}
}

func (g *groupService) Invalidate(groupJID string) {
	if g == nil || groupJID == "" {
		return
	}
	g.mu.Lock()
	delete(g.cache, groupJID)
	g.mu.Unlock()
}

func (g *groupService) Prime(info *types.GroupInfo) {
	if g == nil || info == nil {
		return
	}
	converted := convertGroupInfo(g.client, info)
	g.putCache(converted.JID, converted)
}

func (g *groupService) putCache(groupJID string, info command.GroupInfo) {
	if g == nil || groupJID == "" {
		return
	}
	now := time.Now()
	g.mu.Lock()
	if g.cache == nil {
		g.cache = make(map[string]cachedGroupInfo)
	}
	if len(g.cache) >= groupInfoCacheMaxEntries {
		for k, v := range g.cache {
			if !now.Before(v.expiresAt) {
				delete(g.cache, k)
			}
		}
		if len(g.cache) >= groupInfoCacheMaxEntries {
			clear(g.cache)
		}
	}
	g.cache[groupJID] = cachedGroupInfo{
		info:      info,
		expiresAt: now.Add(groupInfoCacheTTL),
	}
	g.mu.Unlock()
}

func (g *groupService) GetInfo(ctx context.Context, groupJID string) (command.GroupInfo, error) {
	now := time.Now()
	g.mu.RLock()
	if g.cache != nil {
		if entry, ok := g.cache[groupJID]; ok && now.Before(entry.expiresAt) {
			g.mu.RUnlock()
			return entry.info, nil
		}
	}
	g.mu.RUnlock()

	if g.client == nil {
		return command.GroupInfo{}, fmt.Errorf("WhatsApp client unavailable")
	}
	jid, err := types.ParseJID(groupJID)
	if err != nil {
		return command.GroupInfo{}, fmt.Errorf("parse group JID: %w", err)
	}
	info, err := g.client.GetGroupInfo(ctx, jid)
	if err != nil {
		return command.GroupInfo{}, err
	}
	converted := convertGroupInfo(g.client, info)
	g.putCache(groupJID, converted)
	return converted, nil
}

func (g *groupService) UpdateParticipants(ctx context.Context, groupJID string, targets []string, action command.ParticipantAction) error {
	jid, err := types.ParseJID(groupJID)
	if err != nil {
		return fmt.Errorf("parse group JID: %w", err)
	}
	jids, err := parseTargetJIDs(targets)
	if err != nil {
		return err
	}
	var change whatsmeow.ParticipantChange
	switch action {
	case command.ParticipantAdd:
		change = whatsmeow.ParticipantChangeAdd
	case command.ParticipantRemove:
		change = whatsmeow.ParticipantChangeRemove
	case command.ParticipantPromote:
		change = whatsmeow.ParticipantChangePromote
	case command.ParticipantDemote:
		change = whatsmeow.ParticipantChangeDemote
	default:
		return fmt.Errorf("unsupported participant action %q", action)
	}
	results, err := g.client.UpdateGroupParticipants(ctx, jid, jids, change)
	if err != nil {
		return err
	}
	g.Invalidate(groupJID)
	for _, res := range results {
		if res.Error != 0 {
			return fmt.Errorf("failed for %s (code %d)", res.JID.User, res.Error)
		}
	}
	return nil
}

func (g *groupService) SetName(ctx context.Context, groupJID, name string) error {
	jid, err := types.ParseJID(groupJID)
	if err != nil {
		return err
	}
	if err := g.client.SetGroupName(ctx, jid, name); err != nil {
		return err
	}
	g.Invalidate(groupJID)
	return nil
}

func (g *groupService) SetDescription(ctx context.Context, groupJID, description string) error {
	jid, err := types.ParseJID(groupJID)
	if err != nil {
		return err
	}
	if err := g.client.SetGroupDescription(ctx, jid, description); err != nil {
		return err
	}
	g.Invalidate(groupJID)
	return nil
}

func (g *groupService) SetPhoto(ctx context.Context, groupJID string, image []byte) error {
	jid, err := types.ParseJID(groupJID)
	if err != nil {
		return err
	}
	if _, err = g.client.SetGroupPhoto(ctx, jid, image); err != nil {
		return err
	}
	g.Invalidate(groupJID)
	return nil
}

func (g *groupService) SetAnnounce(ctx context.Context, groupJID string, announce bool) error {
	jid, err := types.ParseJID(groupJID)
	if err != nil {
		return err
	}
	if err := g.client.SetGroupAnnounce(ctx, jid, announce); err != nil {
		return err
	}
	g.Invalidate(groupJID)
	return nil
}

func (g *groupService) SetLocked(ctx context.Context, groupJID string, locked bool) error {
	jid, err := types.ParseJID(groupJID)
	if err != nil {
		return err
	}
	if err := g.client.SetGroupLocked(ctx, jid, locked); err != nil {
		return err
	}
	g.Invalidate(groupJID)
	return nil
}

func (g *groupService) SetJoinApproval(ctx context.Context, groupJID string, requireApproval bool) error {
	jid, err := types.ParseJID(groupJID)
	if err != nil {
		return err
	}
	if err := g.client.SetGroupJoinApprovalMode(ctx, jid, requireApproval); err != nil {
		return err
	}
	g.Invalidate(groupJID)
	return nil
}

func (g *groupService) SetMemberAddMode(ctx context.Context, groupJID string, allowAll bool) error {
	jid, err := types.ParseJID(groupJID)
	if err != nil {
		return err
	}
	mode := types.GroupMemberAddModeAdmin
	if allowAll {
		mode = types.GroupMemberAddModeAllMember
	}
	if err := g.client.SetGroupMemberAddMode(ctx, jid, mode); err != nil {
		return err
	}
	g.Invalidate(groupJID)
	return nil
}

func (g *groupService) SetDisappearing(ctx context.Context, groupJID string, seconds uint32) error {
	jid, err := types.ParseJID(groupJID)
	if err != nil {
		return err
	}
	if err := g.client.SetDisappearingTimer(ctx, jid, time.Duration(seconds)*time.Second, time.Now()); err != nil {
		return err
	}
	g.Invalidate(groupJID)
	return nil
}

func (g *groupService) GetInviteLink(ctx context.Context, groupJID string, reset bool) (string, error) {
	jid, err := types.ParseJID(groupJID)
	if err != nil {
		return "", err
	}
	return g.client.GetGroupInviteLink(ctx, jid, reset)
}

func (g *groupService) GetJoinRequests(ctx context.Context, groupJID string) ([]command.GroupRequest, error) {
	jid, err := types.ParseJID(groupJID)
	if err != nil {
		return nil, err
	}
	reqs, err := g.client.GetGroupRequestParticipants(ctx, jid)
	if err != nil {
		return nil, err
	}
	out := make([]command.GroupRequest, 0, len(reqs))
	for _, r := range reqs {
		out = append(out, command.GroupRequest{
			JID:         r.JID.String(),
			RequestedAt: r.RequestedAt.Unix(),
		})
	}
	return out, nil
}

func (g *groupService) UpdateJoinRequests(ctx context.Context, groupJID string, targets []string, approve bool) error {
	jid, err := types.ParseJID(groupJID)
	if err != nil {
		return err
	}
	jids, err := parseTargetJIDs(targets)
	if err != nil {
		return err
	}
	action := whatsmeow.ParticipantChangeReject
	if approve {
		action = whatsmeow.ParticipantChangeApprove
	}
	_, err = g.client.UpdateGroupRequestParticipants(ctx, jid, jids, action)
	return err
}

func (g *groupService) RevokeMessage(ctx context.Context, chatJID, senderJID, messageID string) error {
	chat, err := types.ParseJID(chatJID)
	if err != nil {
		return err
	}
	var sender types.JID
	if strings.TrimSpace(senderJID) != "" {
		sender, err = types.ParseJID(senderJID)
		if err != nil {
			return err
		}
		sender = sender.ToNonAD()
	}
	_, err = g.client.SendMessage(ctx, chat, g.client.BuildRevoke(chat, sender, types.MessageID(messageID)))
	return err
}

func (g *groupService) Leave(ctx context.Context, groupJID string) error {
	jid, err := types.ParseJID(groupJID)
	if err != nil {
		return err
	}
	if err := g.client.LeaveGroup(ctx, jid); err != nil {
		return err
	}
	g.Invalidate(groupJID)
	return nil
}

func parseTargetJIDs(targets []string) ([]types.JID, error) {
	jids := make([]types.JID, 0, len(targets))
	for _, raw := range targets {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if strings.Contains(raw, "@") {
			jid, err := types.ParseJID(raw)
			if err != nil {
				return nil, fmt.Errorf("invalid JID %q: %w", raw, err)
			}
			jids = append(jids, jid.ToNonAD())
			continue
		}
		digits := cleanPhoneDigits(raw)
		if digits == "" {
			return nil, fmt.Errorf("invalid phone number %q", raw)
		}
		jids = append(jids, types.NewJID(digits, types.DefaultUserServer))
	}
	if len(jids) == 0 {
		return nil, fmt.Errorf("no valid target users provided")
	}
	return jids, nil
}

func cleanPhoneDigits(value string) string {
	var b strings.Builder
	for _, r := range value {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

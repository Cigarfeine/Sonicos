package plugins

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/sonicos/whatsapp-bot/internal/command"
)

func readTestFile(t *testing.T, path string) ([]byte, error) {
	t.Helper()
	return os.ReadFile(path)
}

type fakeModerationStore struct {
	bannedFile  map[string]command.BannedSticker
	bannedAudio map[string]command.BannedSticker
	strikes     map[string]int
	seen        map[string]command.StickerInfo
	shields     map[string]command.GroupShields
	rates       map[string]int
	stages      map[string]command.StagedMedia
	reactionLib map[string]map[string]command.SavedReactionSticker
}

func newFakeModerationStore() *fakeModerationStore {
	return &fakeModerationStore{
		bannedFile:  make(map[string]command.BannedSticker),
		bannedAudio: make(map[string]command.BannedSticker),
		strikes:     make(map[string]int),
		seen:        make(map[string]command.StickerInfo),
		shields:     make(map[string]command.GroupShields),
		rates:       make(map[string]int),
		stages:      make(map[string]command.StagedMedia),
		reactionLib: make(map[string]map[string]command.SavedReactionSticker),
	}
}

func (f *fakeModerationStore) BanSticker(_ context.Context, groupJID string, entry command.BannedSticker) error {
	if entry.FileSHA256 != "" {
		f.bannedFile[groupJID+":"+entry.FileSHA256] = entry
	}
	if entry.AudioSHA256 != "" {
		f.bannedAudio[groupJID+":"+entry.AudioSHA256] = entry
	}
	return nil
}

func (f *fakeModerationStore) UnbanSticker(_ context.Context, groupJID, fileSHA256, audioSHA256 string) (bool, error) {
	if stored, ok, _ := f.IsStickerBanned(context.Background(), groupJID, fileSHA256, audioSHA256); ok {
		if stored.FileSHA256 != "" {
			fileSHA256 = stored.FileSHA256
		}
		if stored.AudioSHA256 != "" {
			audioSHA256 = stored.AudioSHA256
		}
	}
	var removed bool
	if fileSHA256 != "" {
		if _, ok := f.bannedFile[groupJID+":"+fileSHA256]; ok {
			delete(f.bannedFile, groupJID+":"+fileSHA256)
			removed = true
		}
	}
	if audioSHA256 != "" {
		if _, ok := f.bannedAudio[groupJID+":"+audioSHA256]; ok {
			delete(f.bannedAudio, groupJID+":"+audioSHA256)
			removed = true
		}
	}
	return removed, nil
}

func (f *fakeModerationStore) UnbanStickerByPrefix(_ context.Context, groupJID, prefix string) (bool, error) {
	if len(prefix) < 6 {
		return false, errors.New("prefix too short")
	}
	for _, e := range f.bannedFile {
		if strings.HasPrefix(e.FileSHA256, prefix) || strings.HasPrefix(e.AudioSHA256, prefix) {
			return f.UnbanSticker(context.Background(), groupJID, e.FileSHA256, e.AudioSHA256)
		}
	}
	for _, e := range f.bannedAudio {
		if strings.HasPrefix(e.FileSHA256, prefix) || strings.HasPrefix(e.AudioSHA256, prefix) {
			return f.UnbanSticker(context.Background(), groupJID, e.FileSHA256, e.AudioSHA256)
		}
	}
	return false, nil
}

func (f *fakeModerationStore) IsStickerBanned(_ context.Context, groupJID, fileSHA256, audioSHA256 string) (command.BannedSticker, bool, error) {
	if e, ok := f.bannedFile[groupJID+":"+fileSHA256]; ok && fileSHA256 != "" {
		return e, true, nil
	}
	if e, ok := f.bannedAudio[groupJID+":"+audioSHA256]; ok && audioSHA256 != "" {
		return e, true, nil
	}
	return command.BannedSticker{}, false, nil
}

func (f *fakeModerationStore) ListBannedStickers(_ context.Context, groupJID string) ([]command.BannedSticker, error) {
	seen := make(map[string]command.BannedSticker)
	for k, e := range f.bannedFile {
		if strings.HasPrefix(k, groupJID+":") {
			seen[e.FileSHA256+"|"+e.AudioSHA256] = e
		}
	}
	for k, e := range f.bannedAudio {
		if strings.HasPrefix(k, groupJID+":") {
			seen[e.FileSHA256+"|"+e.AudioSHA256] = e
		}
	}
	var out []command.BannedSticker
	for _, e := range seen {
		out = append(out, e)
	}
	return out, nil
}

func (f *fakeModerationStore) AddStrike(_ context.Context, groupJID, userID string) (int, error) {
	k := groupJID + ":" + userID
	f.strikes[k]++
	return f.strikes[k], nil
}

func (f *fakeModerationStore) GetStrikes(_ context.Context, groupJID, userID string) (int, error) {
	return f.strikes[groupJID+":"+userID], nil
}

func (f *fakeModerationStore) ResetStrikes(_ context.Context, groupJID, userID string) error {
	delete(f.strikes, groupJID+":"+userID)
	return nil
}

func (f *fakeModerationStore) RememberSticker(_ context.Context, groupJID, messageID string, info command.StickerInfo) error {
	f.seen[groupJID+":"+messageID] = info
	return nil
}

func (f *fakeModerationStore) LookupSticker(_ context.Context, groupJID, messageID string) (command.StickerInfo, bool, error) {
	info, ok := f.seen[groupJID+":"+messageID]
	return info, ok, nil
}

func (f *fakeModerationStore) GetShields(_ context.Context, groupJID string) (command.GroupShields, error) {
	s, ok := f.shields[groupJID]
	if !ok {
		return command.GroupShields{AntiLoudDB: -6.0}, nil
	}
	if s.AntiLoudDB == 0 {
		s.AntiLoudDB = -6.0
	}
	return s, nil
}

func (f *fakeModerationStore) SetShield(_ context.Context, groupJID, field, value string) error {
	s, _ := f.GetShields(context.Background(), groupJID)
	switch field {
	case "anti_loud":
		s.AntiLoud = value == "1"
	case "anti_loud_db":
		if value == "-8.0" {
			s.AntiLoudDB = -8.0
		} else if value == "-5.0" {
			s.AntiLoudDB = -5.0
		}
	case "anti_link":
		s.AntiLink = value == "1"
	case "anti_spam":
		s.AntiSpam = value == "1"
	case "anti_sticker_flood":
		s.AntiStickerFlood = value == "1"
	}
	f.shields[groupJID] = s
	return nil
}

func (f *fakeModerationStore) IncrementRate(_ context.Context, groupJID, bucket, userID string, _ int) (int, error) {
	k := groupJID + ":" + bucket + ":" + userID
	f.rates[k]++
	return f.rates[k], nil
}

func (f *fakeModerationStore) SaveStage(_ context.Context, scopeKey string, media command.StagedMedia, _ int) error {
	f.stages[scopeKey] = media
	return nil
}

func (f *fakeModerationStore) LoadStage(_ context.Context, scopeKey string) (command.StagedMedia, bool, error) {
	m, ok := f.stages[scopeKey]
	return m, ok, nil
}

func (f *fakeModerationStore) ClearStage(_ context.Context, scopeKey string) error {
	delete(f.stages, scopeKey)
	return nil
}

func fakeNormReactionKey(raw string) string {
	cleaned := strings.TrimSpace(raw)
	cleaned = strings.ReplaceAll(cleaned, "\uFE0F", "")
	cleaned = strings.ReplaceAll(cleaned, "\uFE0E", "")
	return strings.ToLower(cleaned)
}

func (f *fakeModerationStore) SaveReactionSticker(_ context.Context, ownerID, emoji string, st command.SavedReactionSticker) error {
	key := fakeNormReactionKey(emoji)
	if ownerID == "" || key == "" || len(st.Data) == 0 {
		return errors.New("invalid reaction sticker")
	}
	if f.reactionLib[ownerID] == nil {
		f.reactionLib[ownerID] = make(map[string]command.SavedReactionSticker)
	}
	if strings.TrimSpace(st.Emoji) == "" {
		st.Emoji = strings.TrimSpace(emoji)
	}
	if st.SavedAt == 0 {
		st.SavedAt = time.Now().UnixNano()
	}
	f.reactionLib[ownerID][key] = st
	return nil
}

func (f *fakeModerationStore) GetReactionSticker(_ context.Context, ownerID, emoji string) (command.SavedReactionSticker, bool, error) {
	key := fakeNormReactionKey(emoji)
	st, ok := f.reactionLib[ownerID][key]
	return st, ok, nil
}

func (f *fakeModerationStore) DeleteReactionSticker(_ context.Context, ownerID, emoji string) (bool, error) {
	key := fakeNormReactionKey(emoji)
	if _, ok := f.reactionLib[ownerID][key]; !ok {
		return false, nil
	}
	delete(f.reactionLib[ownerID], key)
	return true, nil
}

func (f *fakeModerationStore) ListReactionStickers(_ context.Context, ownerID string) ([]command.SavedReactionSticker, error) {
	items := f.reactionLib[ownerID]
	out := make([]command.SavedReactionSticker, 0, len(items))
	for _, st := range items {
		out = append(out, st)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].SavedAt != out[j].SavedAt {
			return out[i].SavedAt < out[j].SavedAt
		}
		return out[i].Emoji < out[j].Emoji
	})
	return out, nil
}

type fakeGroupService struct {
	info    command.GroupInfo
	revoked []string
	kicked  []string
}

func (g *fakeGroupService) GetInfo(context.Context, string) (command.GroupInfo, error) {
	return g.info, nil
}
func (g *fakeGroupService) UpdateParticipants(_ context.Context, _ string, targets []string, action command.ParticipantAction) error {
	if action == command.ParticipantRemove {
		g.kicked = append(g.kicked, targets...)
	}
	return nil
}
func (g *fakeGroupService) RevokeMessage(_ context.Context, _, _, messageID string) error {
	g.revoked = append(g.revoked, messageID)
	return nil
}
func (g *fakeGroupService) SetName(context.Context, string, string) error                { return nil }
func (g *fakeGroupService) SetDescription(context.Context, string, string) error         { return nil }
func (g *fakeGroupService) SetPhoto(context.Context, string, []byte) error               { return nil }
func (g *fakeGroupService) SetAnnounce(context.Context, string, bool) error              { return nil }
func (g *fakeGroupService) SetLocked(context.Context, string, bool) error                { return nil }
func (g *fakeGroupService) SetJoinApproval(context.Context, string, bool) error          { return nil }
func (g *fakeGroupService) SetMemberAddMode(context.Context, string, bool) error         { return nil }
func (g *fakeGroupService) SetDisappearing(context.Context, string, uint32) error        { return nil }
func (g *fakeGroupService) GetInviteLink(context.Context, string, bool) (string, error)  { return "", nil }
func (g *fakeGroupService) GetJoinRequests(context.Context, string) ([]command.GroupRequest, error) {
	return nil, nil
}
func (g *fakeGroupService) UpdateJoinRequests(context.Context, string, []string, bool) error {
	return nil
}
func (g *fakeGroupService) Leave(context.Context, string) error { return nil }

func TestModeration_BanStickerAndThreeStrikeEscalation(t *testing.T) {
	store := newFakeModerationStore()
	groupJID := "120363000000111@g.us"
	adminJID := "15550000001@s.whatsapp.net"
	memberJID := "15550000002@s.whatsapp.net"
	botJID := "15550009999@s.whatsapp.net"

	gs := &fakeGroupService{
		info: command.GroupInfo{
			JID:    groupJID,
			BotJID: botJID,
			Participants: []command.GroupParticipant{
				{JID: adminJID, IsAdmin: true},
				{JID: botJID, IsAdmin: true},
				{JID: memberJID, IsAdmin: false},
			},
		},
	}

	var replies []command.Reply
	reply := func(_ context.Context, r command.Reply) error {
		replies = append(replies, r)
		return nil
	}

	// 1. Member sends an unbanned audio sticker first -> cached in store, no warning.
	badSticker := command.StickerInfo{
		FileSHA256:     "deadbeef11112222",
		AudioSHA256:    "cafebabe33334444",
		IsAudioSticker: true,
		IsAnimated:     true,
	}
	if err := watchBannedStickers(context.Background(), command.Message{
		ID:              "STICKER-MSG-1",
		Chat:            groupJID,
		Sender:          memberJID,
		Group:           true,
		Sticker:         &badSticker,
		GroupService:    gs,
		ModerationStore: store,
	}, reply); err != nil {
		t.Fatal(err)
	}
	if len(replies) != 0 {
		t.Fatalf("expected no reply for unbanned sticker, got %+v", replies)
	}

	// 2. Admin replies !bansticker using ONLY QuotedMessageID (simulating stripped QuotedSticker).
	if err := banStickerCmd(context.Background(), command.Message{
		ID:              "CMD-BAN",
		Chat:            groupJID,
		Sender:          adminJID,
		Group:           true,
		QuotedMessageID: "STICKER-MSG-1",
		QuotedSender:    memberJID,
		GroupService:    gs,
		ModerationStore: store,
	}, []string{"18+", "loud"}, reply); err != nil {
		t.Fatal(err)
	}
	if len(replies) != 1 || !strings.Contains(replies[0].Text, "Audio sticker blacklisted") {
		t.Fatalf("unexpected ban reply: %+v", replies)
	}
	if len(gs.revoked) != 1 || gs.revoked[0] != "STICKER-MSG-1" {
		t.Fatalf("expected quoted sticker revoked by admin bot, got %v", gs.revoked)
	}

	// 3. Member sends a re-skinned sticker reusing the banned AudioSHA256 -> caught 3 times -> kicked!
	reskinned := command.StickerInfo{
		FileSHA256:     "9999999999999999",
		AudioSHA256:    "cafebabe33334444",
		IsAudioSticker: true,
	}
	for i := 1; i <= 3; i++ {
		replies = nil
		if err := watchBannedStickers(context.Background(), command.Message{
			ID:              "OFFENSE-" + string(rune('0'+i)),
			Chat:            groupJID,
			Sender:          memberJID,
			Group:           true,
			Sticker:         &reskinned,
			GroupService:    gs,
			ModerationStore: store,
		}, reply); err != nil {
			t.Fatal(err)
		}
		if len(replies) != 1 {
			t.Fatalf("strike %d: expected 1 warning reply, got %d", i, len(replies))
		}
	}
	if len(gs.revoked) != 4 {
		t.Fatalf("expected 4 total revokes (1 quote + 3 offenses), got %d", len(gs.revoked))
	}
	if len(gs.kicked) != 1 || gs.kicked[0] != memberJID {
		t.Fatalf("expected member kicked on 3rd strike, got %v", gs.kicked)
	}
	if s, _ := store.GetStrikes(context.Background(), groupJID, "15550000002"); s != 0 {
		t.Fatalf("expected strikes reset after kick, got %d", s)
	}

	// 4. Admin sticker is deleted when bot is admin, and admin gets a warning (no kick, no strikes); FromMe stays silent.
	replies = nil
	if err := watchBannedStickers(context.Background(), command.Message{
		ID:              "ADMIN-STICKER",
		Chat:            groupJID,
		Sender:          adminJID,
		Group:           true,
		Sticker:         &badSticker,
		GroupService:    gs,
		ModerationStore: store,
	}, reply); err != nil {
		t.Fatal(err)
	}
	if len(replies) != 1 || !strings.Contains(replies[0].Text, "blacklisted in this group and was deleted") || len(replies[0].Mentions) != 1 || replies[0].Mentions[0] != adminJID {
		t.Fatalf("expected admin warning reply with deletion notice, got %+v", replies)
	}
	if len(gs.revoked) != 5 || gs.revoked[4] != "ADMIN-STICKER" || len(gs.kicked) != 1 {
		t.Fatalf("expected admin sticker revoked without kicking; revoked=%v kicked=%v", gs.revoked, gs.kicked)
	}
	if s, _ := store.GetStrikes(context.Background(), groupJID, "15550000001"); s != 0 {
		t.Fatalf("expected 0 strikes for admin, got %d", s)
	}

	replies = nil
	if err := watchBannedStickers(context.Background(), command.Message{
		ID:              "SELF-STICKER",
		Chat:            groupJID,
		Sender:          memberJID,
		FromMe:          true,
		Group:           true,
		Sticker:         &badSticker,
		GroupService:    gs,
		ModerationStore: store,
	}, reply); err != nil {
		t.Fatal(err)
	}
	if len(replies) != 0 {
		t.Fatalf("expected FromMe exempt, got %+v", replies)
	}
}

func TestModeration_BotNotAdmin_WarnsWithoutDeleteOrKick(t *testing.T) {
	store := newFakeModerationStore()
	groupJID := "120363000000222@g.us"
	memberJID := "15550000003@s.whatsapp.net"
	botJID := "15550009999@s.whatsapp.net"

	gs := &fakeGroupService{
		info: command.GroupInfo{
			JID:    groupJID,
			BotJID: botJID,
			Participants: []command.GroupParticipant{
				{JID: botJID, IsAdmin: false},
				{JID: memberJID, IsAdmin: false},
			},
		},
	}

	_ = store.BanSticker(context.Background(), groupJID, command.BannedSticker{
		AudioSHA256: "bad_audio_hash_1234",
		Reason:      "18+ audio",
	})

	var replies []command.Reply
	reply := func(_ context.Context, r command.Reply) error {
		replies = append(replies, r)
		return nil
	}

	if err := watchBannedStickers(context.Background(), command.Message{
		ID:              "M1",
		Chat:            groupJID,
		Sender:          memberJID,
		Group:           true,
		Sticker:         &command.StickerInfo{AudioSHA256: "bad_audio_hash_1234", IsAudioSticker: true},
		GroupService:    gs,
		ModerationStore: store,
	}, reply); err != nil {
		t.Fatal(err)
	}

	if len(gs.revoked) != 0 || len(gs.kicked) != 0 {
		t.Fatalf("bot not admin must not revoke or kick; revoked=%v kicked=%v", gs.revoked, gs.kicked)
	}
	if s, _ := store.GetStrikes(context.Background(), groupJID, "15550000003"); s != 0 {
		t.Fatalf("expected 0 strikes when bot is not admin, got %d", s)
	}
	if len(replies) != 1 || !strings.Contains(replies[0].Text, "NSFW / 18+ audio sticker") || !strings.Contains(replies[0].Text, "watch/listen at your own risk!") {
		t.Fatalf("expected NSFW watch/listen at your own risk warning, got %+v", replies)
	}

	// List & unban by prefix.
	replies = nil
	if err := listBannedStickersCmd(context.Background(), command.Message{
		Chat:            groupJID,
		Sender:          botJID,
		Group:           true,
		GroupService:    &fakeGroupService{info: command.GroupInfo{Participants: []command.GroupParticipant{{JID: botJID, IsAdmin: true}}}},
		ModerationStore: store,
	}, nil, reply); err != nil {
		t.Fatal(err)
	}
	if len(replies) != 1 || !strings.Contains(replies[0].Text, "bad_audi") {
		t.Fatalf("unexpected banned list: %+v", replies)
	}

	replies = nil
	if err := unbanStickerCmd(context.Background(), command.Message{
		Chat:            groupJID,
		Sender:          botJID,
		Group:           true,
		GroupService:    &fakeGroupService{info: command.GroupInfo{Participants: []command.GroupParticipant{{JID: botJID, IsAdmin: true}}}},
		ModerationStore: store,
	}, []string{"bad_audi"}, reply); err != nil {
		t.Fatal(err)
	}
	if len(replies) != 1 || !strings.Contains(replies[0].Text, "removed") {
		t.Fatalf("unexpected unban reply: %+v", replies)
	}
}

func TestShields_AntiLink_AntiSpam_AntiStickerFlood_And_AntiLoud(t *testing.T) {
	store := newFakeModerationStore()
	groupJID := "120363000000333@g.us"
	adminJID := "15550000010@s.whatsapp.net"
	memberJID := "15550000011@s.whatsapp.net"
	botJID := "15550009999@s.whatsapp.net"

	gs := &fakeGroupService{
		info: command.GroupInfo{
			JID:    groupJID,
			BotJID: botJID,
			Participants: []command.GroupParticipant{
				{JID: adminJID, IsAdmin: true},
				{JID: botJID, IsAdmin: true},
				{JID: memberJID, IsAdmin: false},
			},
		},
	}
	var replies []command.Reply
	reply := func(_ context.Context, r command.Reply) error {
		replies = append(replies, r)
		return nil
	}

	// Enable antilink, antispam, antistickerflood via admin commands.
	adminMsg := command.Message{Chat: groupJID, Sender: adminJID, Group: true, GroupService: gs, ModerationStore: store}
	_ = antiLinkCmd(context.Background(), adminMsg, []string{"on"}, reply)
	_ = antiSpamCmd(context.Background(), adminMsg, []string{"on"}, reply)
	_ = antiStickerFloodCmd(context.Background(), adminMsg, []string{"on"}, reply)
	_ = antiLoudCmd(context.Background(), adminMsg, []string{"-8"}, reply)
	replies = nil
	_ = shieldsStatusCmd(context.Background(), adminMsg, nil, reply)
	if len(replies) != 1 || !strings.Contains(replies[0].Text, "Anti-Link: *ON*") || !strings.Contains(replies[0].Text, "-8.0 dB") {
		t.Fatalf("unexpected shields status: %+v", replies)
	}

	// AntiLink blocks member link, exempts admin link.
	replies = nil
	_ = watchBannedStickers(context.Background(), command.Message{
		ID: "LINK-1", Chat: groupJID, Sender: memberJID, Group: true,
		Text: "join https://chat.whatsapp.com/abc", GroupService: gs, ModerationStore: store,
	}, reply)
	if len(replies) != 1 || !strings.Contains(replies[0].Text, "blocked link") || len(gs.revoked) != 1 {
		t.Fatalf("expected antilink revoke+warn, got replies=%+v revoked=%v", replies, gs.revoked)
	}
	replies = nil
	_ = watchBannedStickers(context.Background(), command.Message{
		ID: "LINK-ADM", Chat: groupJID, Sender: adminJID, Group: true,
		Text: "https://example.com", GroupService: gs, ModerationStore: store,
	}, reply)
	if len(replies) != 0 || len(gs.revoked) != 1 {
		t.Fatalf("admin link should be exempt")
	}

	// AntiStickerFlood triggers on 5th sticker.
	for i := 1; i <= 5; i++ {
		replies = nil
		_ = watchBannedStickers(context.Background(), command.Message{
			ID: "STK-" + string(rune('0'+i)), Chat: groupJID, Sender: memberJID, Group: true,
			Sticker: &command.StickerInfo{FileSHA256: "okhash"}, GroupService: gs, ModerationStore: store,
		}, reply)
	}
	if len(replies) != 1 || !strings.Contains(replies[0].Text, "flooded stickers") {
		t.Fatalf("expected sticker flood warning on 5th sticker, got %+v", replies)
	}

	// AntiSpam triggers on 7th text message for a fresh member.
	spammerJID := "15550000019@s.whatsapp.net"
	gs.info.Participants = append(gs.info.Participants, command.GroupParticipant{JID: spammerJID})
	for i := 1; i <= 7; i++ {
		replies = nil
		_ = watchBannedStickers(context.Background(), command.Message{
			ID: "TXT-" + string(rune('0'+i)), Chat: groupJID, Sender: spammerJID, Group: true,
			Text: "hi", GroupService: gs, ModerationStore: store,
		}, reply)
	}
	if len(replies) != 1 || !strings.Contains(replies[0].Text, "spammed messages") {
		t.Fatalf("expected spam warning on 7th message, got %+v", replies)
	}
}

func TestShields_AntiLoud_LiveFFmpeg(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	store := newFakeModerationStore()
	groupJID := "120363000000444@g.us"
	memberJID := "15550000012@s.whatsapp.net"
	botJID := "15550009999@s.whatsapp.net"
	_ = store.SetShield(context.Background(), groupJID, "anti_loud", "1")

	quietPath := filepath.Join(t.TempDir(), "quiet.ogg")
	loudPath := filepath.Join(t.TempDir(), "loud.ogg")
	_ = exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-y", "-f", "lavfi", "-i", "sine=frequency=440:duration=2", "-af", "volume=-20dB", "-c:a", "libopus", quietPath).Run()
	_ = exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-y", "-f", "lavfi", "-i", "sine=frequency=440:duration=2", "-af", "volume=25dB,alimiter=limit=0.99", "-c:a", "libopus", loudPath).Run()
	quietBytes, err := readTestFile(t, quietPath)
	if err != nil {
		t.Fatal(err)
	}
	loudBytes, err := readTestFile(t, loudPath)
	if err != nil {
		t.Fatal(err)
	}

	gs := &fakeGroupService{
		info: command.GroupInfo{
			JID: groupJID, BotJID: botJID,
			Participants: []command.GroupParticipant{{JID: botJID, IsAdmin: true}, {JID: memberJID}},
		},
	}
	var replies []command.Reply
	reply := func(_ context.Context, r command.Reply) error {
		replies = append(replies, r)
		return nil
	}

	// Quiet audio passes untouched.
	_ = watchBannedStickers(context.Background(), command.Message{
		ID: "Q1", Chat: groupJID, Sender: memberJID, Group: true,
		Sticker: &command.StickerInfo{IsAudioSticker: true},
		StickerAudioSource: stubMediaSource{att: command.Attachment{Kind: command.MediaAudio, Data: quietBytes}},
		GroupService: gs, ModerationStore: store,
	}, reply)
	if len(replies) != 0 || len(gs.revoked) != 0 {
		t.Fatalf("quiet audio should not trigger antiloud")
	}

	// Earrape audio triggers delete + warning when bot is admin.
	_ = watchBannedStickers(context.Background(), command.Message{
		ID: "L1", Chat: groupJID, Sender: memberJID, Group: true,
		Sticker: &command.StickerInfo{IsAudioSticker: true},
		StickerAudioSource: stubMediaSource{att: command.Attachment{Kind: command.MediaAudio, Data: loudBytes}},
		GroupService: gs, ModerationStore: store,
	}, reply)
	if len(replies) != 1 || !strings.Contains(replies[0].Text, "extremely loud") || len(gs.revoked) != 1 {
		t.Fatalf("expected earrape delete+warn, got replies=%+v revoked=%v", replies, gs.revoked)
	}

	// When bot is NOT admin, sends listen-at-your-own-risk caution without deleting.
	gs.info.Participants[0].IsAdmin = false
	replies = nil
	_ = watchBannedStickers(context.Background(), command.Message{
		ID: "L2", Chat: groupJID, Sender: memberJID, Group: true,
		Sticker: &command.StickerInfo{IsAudioSticker: true},
		StickerAudioSource: stubMediaSource{att: command.Attachment{Kind: command.MediaAudio, Data: loudBytes}},
		GroupService: gs, ModerationStore: store,
	}, reply)
	if len(replies) != 1 || !strings.Contains(replies[0].Text, "listen at your own risk!") || len(gs.revoked) != 1 {
		t.Fatalf("expected non-admin caution without delete, got replies=%+v revoked=%v", replies, gs.revoked)
	}
}

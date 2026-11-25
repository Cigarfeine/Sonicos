package moderation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/sonicos/whatsapp-bot/internal/command"
)

const seenStickerTTL = 24 * time.Hour

// RedisStore persists per-group sticker blacklists, user strike counters,
// and a short-lived message-ID -> sticker-hash cache in Redis.
type RedisStore struct {
	client *redis.Client
}

func NewRedisStore(client *redis.Client) *RedisStore {
	if client == nil {
		return nil
	}
	return &RedisStore{client: client}
}

func banFileKey(groupJID string) string  { return "sonicos:mod:ban:file:" + groupJID }
func banAudioKey(groupJID string) string { return "sonicos:mod:ban:audio:" + groupJID }
func strikesKey(groupJID string) string  { return "sonicos:mod:strikes:" + groupJID }
func seenKey(groupJID, messageID string) string {
	return "sonicos:mod:seen:" + groupJID + ":" + messageID
}

func normHash(h string) string {
	return strings.ToLower(strings.TrimSpace(h))
}

func (s *RedisStore) BanSticker(ctx context.Context, groupJID string, entry command.BannedSticker) error {
	entry.FileSHA256 = normHash(entry.FileSHA256)
	entry.AudioSHA256 = normHash(entry.AudioSHA256)
	if entry.FileSHA256 == "" && entry.AudioSHA256 == "" {
		return errors.New("at least one of file or audio SHA-256 hash is required")
	}
	if entry.AddedAt == 0 {
		entry.AddedAt = time.Now().Unix()
	}
	payload, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal banned sticker: %w", err)
	}
	if entry.FileSHA256 != "" {
		if err := s.client.HSet(ctx, banFileKey(groupJID), entry.FileSHA256, payload).Err(); err != nil {
			return fmt.Errorf("store banned file hash: %w", err)
		}
	}
	if entry.AudioSHA256 != "" {
		if err := s.client.HSet(ctx, banAudioKey(groupJID), entry.AudioSHA256, payload).Err(); err != nil {
			return fmt.Errorf("store banned audio hash: %w", err)
		}
	}
	return nil
}

func (s *RedisStore) UnbanSticker(ctx context.Context, groupJID, fileSHA256, audioSHA256 string) (bool, error) {
	fileHash := normHash(fileSHA256)
	audioHash := normHash(audioSHA256)
	if fileHash == "" && audioHash == "" {
		return false, nil
	}

	// Look up stored entry so paired file+audio hashes are removed together cleanly.
	if stored, ok, err := s.IsStickerBanned(ctx, groupJID, fileHash, audioHash); err != nil {
		return false, err
	} else if ok {
		if stored.FileSHA256 != "" {
			fileHash = normHash(stored.FileSHA256)
		}
		if stored.AudioSHA256 != "" {
			audioHash = normHash(stored.AudioSHA256)
		}
	}

	var removed int64
	if fileHash != "" {
		n, err := s.client.HDel(ctx, banFileKey(groupJID), fileHash).Result()
		if err != nil {
			return false, fmt.Errorf("remove banned file hash: %w", err)
		}
		removed += n
	}
	if audioHash != "" {
		n, err := s.client.HDel(ctx, banAudioKey(groupJID), audioHash).Result()
		if err != nil {
			return false, fmt.Errorf("remove banned audio hash: %w", err)
		}
		removed += n
	}
	return removed > 0, nil
}

func (s *RedisStore) UnbanStickerByPrefix(ctx context.Context, groupJID, hashPrefix string) (bool, error) {
	prefix := normHash(hashPrefix)
	if len(prefix) < 6 {
		return false, errors.New("hash prefix must be at least 6 characters")
	}
	entries, err := s.ListBannedStickers(ctx, groupJID)
	if err != nil {
		return false, err
	}
	var matches []command.BannedSticker
	for _, entry := range entries {
		if (entry.FileSHA256 != "" && strings.HasPrefix(entry.FileSHA256, prefix)) ||
			(entry.AudioSHA256 != "" && strings.HasPrefix(entry.AudioSHA256, prefix)) {
			matches = append(matches, entry)
		}
	}
	if len(matches) == 0 {
		return false, nil
	}
	if len(matches) > 1 {
		return false, fmt.Errorf("hash prefix %q matches %d entries; provide more characters", prefix, len(matches))
	}
	return s.UnbanSticker(ctx, groupJID, matches[0].FileSHA256, matches[0].AudioSHA256)
}

func (s *RedisStore) IsStickerBanned(ctx context.Context, groupJID, fileSHA256, audioSHA256 string) (command.BannedSticker, bool, error) {
	if fh := normHash(fileSHA256); fh != "" {
		raw, err := s.client.HGet(ctx, banFileKey(groupJID), fh).Bytes()
		if err != nil && !errors.Is(err, redis.Nil) {
			return command.BannedSticker{}, false, fmt.Errorf("lookup banned file hash: %w", err)
		}
		if err == nil {
			var entry command.BannedSticker
			if uerr := json.Unmarshal(raw, &entry); uerr != nil {
				return command.BannedSticker{FileSHA256: fh}, true, nil
			}
			return entry, true, nil
		}
	}
	if ah := normHash(audioSHA256); ah != "" {
		raw, err := s.client.HGet(ctx, banAudioKey(groupJID), ah).Bytes()
		if err != nil && !errors.Is(err, redis.Nil) {
			return command.BannedSticker{}, false, fmt.Errorf("lookup banned audio hash: %w", err)
		}
		if err == nil {
			var entry command.BannedSticker
			if uerr := json.Unmarshal(raw, &entry); uerr != nil {
				return command.BannedSticker{AudioSHA256: ah}, true, nil
			}
			return entry, true, nil
		}
	}
	return command.BannedSticker{}, false, nil
}

func (s *RedisStore) ListBannedStickers(ctx context.Context, groupJID string) ([]command.BannedSticker, error) {
	seen := make(map[string]command.BannedSticker)
	for _, key := range []string{banFileKey(groupJID), banAudioKey(groupJID)} {
		vals, err := s.client.HGetAll(ctx, key).Result()
		if err != nil {
			return nil, fmt.Errorf("list banned stickers: %w", err)
		}
		for hash, raw := range vals {
			var entry command.BannedSticker
			if err := json.Unmarshal([]byte(raw), &entry); err != nil {
				entry = command.BannedSticker{FileSHA256: hash}
			}
			entry.FileSHA256 = normHash(entry.FileSHA256)
			entry.AudioSHA256 = normHash(entry.AudioSHA256)
			dedupKey := entry.FileSHA256 + "|" + entry.AudioSHA256
			seen[dedupKey] = entry
		}
	}
	out := make([]command.BannedSticker, 0, len(seen))
	for _, entry := range seen {
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].AddedAt != out[j].AddedAt {
			return out[i].AddedAt < out[j].AddedAt
		}
		if out[i].FileSHA256 != out[j].FileSHA256 {
			return out[i].FileSHA256 < out[j].FileSHA256
		}
		return out[i].AudioSHA256 < out[j].AudioSHA256
	})
	return out, nil
}

func (s *RedisStore) AddStrike(ctx context.Context, groupJID, userID string) (int, error) {
	n, err := s.client.HIncrBy(ctx, strikesKey(groupJID), userID, 1).Result()
	if err != nil {
		return 0, fmt.Errorf("increment sticker strike: %w", err)
	}
	return int(n), nil
}

func (s *RedisStore) GetStrikes(ctx context.Context, groupJID, userID string) (int, error) {
	raw, err := s.client.HGet(ctx, strikesKey(groupJID), userID).Result()
	if errors.Is(err, redis.Nil) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("get sticker strikes: %w", err)
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, nil
	}
	return n, nil
}

func (s *RedisStore) ResetStrikes(ctx context.Context, groupJID, userID string) error {
	if err := s.client.HDel(ctx, strikesKey(groupJID), userID).Err(); err != nil {
		return fmt.Errorf("reset sticker strikes: %w", err)
	}
	return nil
}

func (s *RedisStore) RememberSticker(ctx context.Context, groupJID, messageID string, info command.StickerInfo) error {
	if strings.TrimSpace(groupJID) == "" || strings.TrimSpace(messageID) == "" {
		return nil
	}
	info.FileSHA256 = normHash(info.FileSHA256)
	info.AudioSHA256 = normHash(info.AudioSHA256)
	raw, err := json.Marshal(info)
	if err != nil {
		return err
	}
	return s.client.Set(ctx, seenKey(groupJID, messageID), raw, seenStickerTTL).Err()
}

func (s *RedisStore) LookupSticker(ctx context.Context, groupJID, messageID string) (command.StickerInfo, bool, error) {
	if strings.TrimSpace(groupJID) == "" || strings.TrimSpace(messageID) == "" {
		return command.StickerInfo{}, false, nil
	}
	raw, err := s.client.Get(ctx, seenKey(groupJID, messageID)).Bytes()
	if errors.Is(err, redis.Nil) {
		return command.StickerInfo{}, false, nil
	}
	if err != nil {
		return command.StickerInfo{}, false, fmt.Errorf("lookup seen sticker: %w", err)
	}
	var info command.StickerInfo
	if err := json.Unmarshal(raw, &info); err != nil {
		return command.StickerInfo{}, false, nil
	}
	return info, true, nil
}

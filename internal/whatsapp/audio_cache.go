package whatsapp

import (
	"context"
	"encoding/hex"
	"strings"
	"sync"

	"github.com/sonicos/whatsapp-bot/internal/command"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

const (
	stickerAudioCacheLimit = 2000
	stickerAudioRedisTTL   = 86400
)

type stickerAudioCache struct {
	mu      sync.RWMutex
	entries map[string][]byte
	order   []string
}

func newStickerAudioCache() *stickerAudioCache {
	return &stickerAudioCache{
		entries: make(map[string][]byte),
	}
}

func (c *stickerAudioCache) Remember(ctx context.Context, store command.ModerationStore, chatJID, messageID string, msg *waE2E.Message) {
	if c == nil || msg == nil {
		return
	}
	st, _ := unwrapStickerMessage(msg, false, 0)
	if st == nil {
		return
	}
	audio := normalizeAudioForDownload(st.GetAudioMessage())
	if !isDownloadableAudio(audio) {
		return
	}
	raw, err := proto.Marshal(audio)
	if err != nil || len(raw) == 0 {
		return
	}

	var keys []string
	chatJID = strings.TrimSpace(chatJID)
	messageID = strings.TrimSpace(messageID)
	if chatJID != "" && messageID != "" {
		keys = append(keys, "msg:"+chatJID+":"+messageID)
	}
	if fileSHA := st.GetFileSHA256(); len(fileSHA) > 0 {
		keys = append(keys, "sha:"+hex.EncodeToString(fileSHA))
	}
	if len(keys) == 0 {
		return
	}

	for _, key := range keys {
		c.putMemory(key, raw)
		if store != nil {
			_ = store.SaveStage(ctx, "stickeraudio:"+key, command.StagedMedia{
				Kind:     command.MediaAudio,
				MIMEType: "application/x-protobuf",
				Data:     raw,
			}, stickerAudioRedisTTL)
		}
	}
}

func (c *stickerAudioCache) Lookup(ctx context.Context, store command.ModerationStore, chatJID, quotedMessageID, stickerFileSHA256 string) *waE2E.AudioMessage {
	if c == nil {
		return nil
	}
	var keys []string
	chatJID = strings.TrimSpace(chatJID)
	quotedMessageID = strings.TrimSpace(quotedMessageID)
	if chatJID != "" && quotedMessageID != "" {
		keys = append(keys, "msg:"+chatJID+":"+quotedMessageID)
	}
	if sha := strings.ToLower(strings.TrimSpace(stickerFileSHA256)); sha != "" {
		keys = append(keys, "sha:"+sha)
	}

	for _, key := range keys {
		if raw := c.getMemory(key); len(raw) > 0 {
			if audio := decodeCachedAudio(raw); audio != nil {
				return audio
			}
		}
		if store != nil {
			if staged, ok, err := store.LoadStage(ctx, "stickeraudio:"+key); err == nil && ok && len(staged.Data) > 0 {
				if audio := decodeCachedAudio(staged.Data); audio != nil {
					c.putMemory(key, staged.Data)
					return audio
				}
			}
		}
	}
	return nil
}

func (c *stickerAudioCache) putMemory(key string, raw []byte) {
	cp := append([]byte(nil), raw...)
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.entries[key]; !exists {
		c.order = append(c.order, key)
		for len(c.order) > stickerAudioCacheLimit {
			oldest := c.order[0]
			c.order = c.order[1:]
			delete(c.entries, oldest)
		}
	}
	c.entries[key] = cp
}

func (c *stickerAudioCache) getMemory(key string) []byte {
	c.mu.RLock()
	defer c.mu.RUnlock()
	raw := c.entries[key]
	if len(raw) == 0 {
		return nil
	}
	return append([]byte(nil), raw...)
}

func decodeCachedAudio(raw []byte) *waE2E.AudioMessage {
	var audio waE2E.AudioMessage
	if err := proto.Unmarshal(raw, &audio); err != nil {
		return nil
	}
	normalized := normalizeAudioForDownload(&audio)
	if !isDownloadableAudio(normalized) {
		return nil
	}
	return normalized
}

package idempotency

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type Store struct {
	client *redis.Client
	prefix string
}

var completeScript = redis.NewScript(`
local current = redis.call("GET", KEYS[1])
if current == ARGV[1] then
  redis.call("SET", KEYS[1], "done", "EX", ARGV[2])
  return 1
end
return 0
`)

var releaseScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("DEL", KEYS[1])
end
return 0
`)

func New(ctx context.Context, address string) (*Store, error) {
	options, err := redis.ParseURL(address)
	if err != nil {
		return nil, fmt.Errorf("parse REDIS_URL: %w", err)
	}
	client := redis.NewClient(options)
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("connect to Redis: %w", err)
	}
	return &Store{client: client, prefix: "sonicos:processed:"}, nil
}

func (s *Store) Begin(ctx context.Context, messageID string) (string, bool, error) {
	var rawToken [16]byte
	if _, err := rand.Read(rawToken[:]); err != nil {
		return "", false, fmt.Errorf("generate idempotency token: %w", err)
	}
	token := hex.EncodeToString(rawToken[:])
	claimed, err := s.client.SetNX(ctx, s.prefix+messageID, token, 2*time.Minute).Result()
	if err != nil {
		return "", false, fmt.Errorf("claim message idempotency key: %w", err)
	}
	return token, claimed, nil
}

func (s *Store) Complete(ctx context.Context, messageID, token string) error {
	_, err := completeScript.Run(ctx, s.client, []string{s.prefix + messageID}, token, int((24*time.Hour)/time.Second)).Result()
	if err != nil {
		return fmt.Errorf("complete message idempotency key: %w", err)
	}
	return nil
}

func (s *Store) Release(ctx context.Context, messageID, token string) error {
	if err := releaseScript.Run(ctx, s.client, []string{s.prefix + messageID}, token).Err(); err != nil {
		return fmt.Errorf("release message idempotency key: %w", err)
	}
	return nil
}

func (s *Store) Client() *redis.Client {
	return s.client
}

func (s *Store) Close() error {
	return s.client.Close()
}

package whatsapp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "github.com/mattn/go-sqlite3"
	"go.mau.fi/whatsmeow/store/sqlstore"
)

func OpenStore(ctx context.Context, driver, address string) (*sqlstore.Container, error) {
	if driver == "sqlite3" && strings.HasPrefix(address, "file:") {
		path := strings.TrimPrefix(strings.SplitN(address, "?", 2)[0], "file:")
		if path != ":memory:" && path != "" {
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				return nil, fmt.Errorf("create database directory: %w", err)
			}
		}
	}
	container, err := sqlstore.New(ctx, driver, address, nil)
	if err != nil {
		return nil, fmt.Errorf("open whatsmeow store: %w", err)
	}
	return container, nil
}

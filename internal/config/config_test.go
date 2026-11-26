package config

import (
	"net/url"
	"reflect"
	"testing"
)

func clearCloudAndDBEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"RAILWAY_ENVIRONMENT", "RAILWAY_PROJECT_ID", "RAILWAY_SERVICE_ID", "RAILWAY_VOLUME_MOUNT_PATH",
		"DYNO", "HEROKU_APP_ID", "HEROKU_APP_NAME",
		"DATABASE_DRIVER", "DATABASE_URL", "REDIS_URL",
	} {
		t.Setenv(k, "")
	}
}

func TestLoadDashboardAndPortConfig(t *testing.T) {
	clearCloudAndDBEnv(t)
	t.Setenv("DASHBOARD_PASSWORD", "  alpha123  ")
	t.Setenv("PORT", "9090")
	t.Setenv("CORS_ALLOWED_ORIGINS", " https://a.example.com/ , , https://b.example.com ")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if cfg.DashboardPassword != "alpha123" {
		t.Fatalf("DashboardPassword = %q, want %q", cfg.DashboardPassword, "alpha123")
	}
	if cfg.Port != "9090" || !cfg.PortExplicit {
		t.Fatalf("Port=%q PortExplicit=%v, want 9090 / true", cfg.Port, cfg.PortExplicit)
	}
	if cfg.CloudPlatform != "" || cfg.CloudEphemeral {
		t.Fatalf("expected local non-ephemeral mode, got platform=%q ephemeral=%v", cfg.CloudPlatform, cfg.CloudEphemeral)
	}
	if cfg.RedisURL != "redis://localhost:6379/0" {
		t.Fatalf("expected local Redis fallback, got %q", cfg.RedisURL)
	}
	wantOrigins := []string{"https://a.example.com", "https://b.example.com"}
	if !reflect.DeepEqual(cfg.CORSAllowedOrigins, wantOrigins) {
		t.Fatalf("CORSAllowedOrigins = %v, want %v", cfg.CORSAllowedOrigins, wantOrigins)
	}
}

func TestLoadEmptyDashboardPasswordAndDefaultPort(t *testing.T) {
	clearCloudAndDBEnv(t)
	t.Setenv("DASHBOARD_PASSWORD", "")
	t.Setenv("PORT", "")
	t.Setenv("CORS_ALLOWED_ORIGINS", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if cfg.DashboardPassword != "" {
		t.Fatalf("expected empty DashboardPassword, got %q", cfg.DashboardPassword)
	}
	if cfg.Port != "8080" || cfg.PortExplicit {
		t.Fatalf("Port=%q PortExplicit=%v, want 8080 / false", cfg.Port, cfg.PortExplicit)
	}
}

func TestLoadCloudDetectionAndSupabaseAutoConfig(t *testing.T) {
	t.Run("railway without volume", func(t *testing.T) {
		clearCloudAndDBEnv(t)
		t.Setenv("RAILWAY_ENVIRONMENT", "production")
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load() error: %v", err)
		}
		if cfg.CloudPlatform != "railway" || cfg.HasPersistentVolume || !cfg.CloudEphemeral {
			t.Fatalf("unexpected railway ephemeral state: %+v", cfg)
		}
		if cfg.RedisURL != "" || cfg.RedisConfigured {
			t.Fatalf("expected empty RedisURL on cloud without REDIS_URL, got %q", cfg.RedisURL)
		}
	})

	t.Run("railway with volume", func(t *testing.T) {
		clearCloudAndDBEnv(t)
		t.Setenv("RAILWAY_ENVIRONMENT", "production")
		t.Setenv("RAILWAY_VOLUME_MOUNT_PATH", "/app/data")
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load() error: %v", err)
		}
		if cfg.CloudPlatform != "railway" || !cfg.HasPersistentVolume || cfg.CloudEphemeral {
			t.Fatalf("unexpected railway volume state: %+v", cfg)
		}
	})

	t.Run("heroku ephemeral", func(t *testing.T) {
		clearCloudAndDBEnv(t)
		t.Setenv("DYNO", "web.1")
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load() error: %v", err)
		}
		if cfg.CloudPlatform != "heroku" || cfg.HasPersistentVolume || !cfg.CloudEphemeral {
			t.Fatalf("unexpected heroku state: %+v", cfg)
		}
	})

	t.Run("supabase transaction pooler overrides sqlite3 driver", func(t *testing.T) {
		clearCloudAndDBEnv(t)
		t.Setenv("RAILWAY_ENVIRONMENT", "production")
		t.Setenv("DATABASE_DRIVER", "sqlite3")
		t.Setenv("DATABASE_URL", "postgresql://postgres.proj:secret@aws-0-ap-south-1.pooler.supabase.com:6543/postgres")

		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load() error: %v", err)
		}
		if cfg.DatabaseDriver != "pgx" {
			t.Fatalf("DatabaseDriver = %q, want pgx", cfg.DatabaseDriver)
		}
		if cfg.CloudEphemeral {
			t.Fatal("expected CloudEphemeral=false when using Postgres")
		}
		u, err := url.Parse(cfg.DatabaseURL)
		if err != nil {
			t.Fatalf("parse normalized URL: %v", err)
		}
		if u.Query().Get("sslmode") != "require" {
			t.Fatalf("sslmode = %q, want require", u.Query().Get("sslmode"))
		}
		if u.Query().Get("default_query_exec_mode") != "simple_protocol" {
			t.Fatalf("default_query_exec_mode = %q, want simple_protocol", u.Query().Get("default_query_exec_mode"))
		}
	})

	t.Run("supabase session pooler 5432", func(t *testing.T) {
		clearCloudAndDBEnv(t)
		t.Setenv("RAILWAY_ENVIRONMENT", "production")
		t.Setenv("DATABASE_URL", "postgres://postgres.proj:secret@aws-0-ap-south-1.pooler.supabase.com:5432/postgres")

		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load() error: %v", err)
		}
		if cfg.DatabaseDriver != "pgx" || cfg.CloudEphemeral {
			t.Fatalf("unexpected config: driver=%q ephemeral=%v", cfg.DatabaseDriver, cfg.CloudEphemeral)
		}
		u, _ := url.Parse(cfg.DatabaseURL)
		if u.Query().Get("sslmode") != "require" {
			t.Fatalf("sslmode = %q, want require", u.Query().Get("sslmode"))
		}
	})

	t.Run("unencoded password characters normalize cleanly", func(t *testing.T) {
		clearCloudAndDBEnv(t)
		t.Setenv("RAILWAY_ENVIRONMENT", "production")
		t.Setenv("DATABASE_URL", "postgresql://postgres.proj:Pass@2026#@aws-0-ap-northeast-1.pooler.supabase.com:5432/postgres")

		cfgUnencoded, err := Load()
		if err != nil {
			t.Fatalf("Load() unencoded error: %v", err)
		}

		t.Setenv("DATABASE_URL", "postgresql://postgres.proj:Pass%402026%23@aws-0-ap-northeast-1.pooler.supabase.com:5432/postgres")
		cfgEncoded, err := Load()
		if err != nil {
			t.Fatalf("Load() encoded error: %v", err)
		}

		if cfgUnencoded.DatabaseURL != cfgEncoded.DatabaseURL {
			t.Fatalf("normalized mismatch:\n  unencoded=%q\n  encoded=  %q", cfgUnencoded.DatabaseURL, cfgEncoded.DatabaseURL)
		}
	})
}

func TestLoadRejectsInvalidPort(t *testing.T) {
	clearCloudAndDBEnv(t)
	for _, bad := range []string{"0", "70000", "abc"} {
		t.Setenv("PORT", bad)
		if _, err := Load(); err == nil {
			t.Fatalf("Load() expected error for PORT=%q", bad)
		}
	}
}

package config

import (
	"reflect"
	"testing"
)

func TestLoadDashboardAndPortConfig(t *testing.T) {
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
	wantOrigins := []string{"https://a.example.com", "https://b.example.com"}
	if !reflect.DeepEqual(cfg.CORSAllowedOrigins, wantOrigins) {
		t.Fatalf("CORSAllowedOrigins = %v, want %v", cfg.CORSAllowedOrigins, wantOrigins)
	}
}

func TestLoadEmptyDashboardPasswordAndDefaultPort(t *testing.T) {
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

func TestLoadRejectsInvalidPort(t *testing.T) {
	for _, bad := range []string{"0", "70000", "abc"} {
		t.Setenv("PORT", bad)
		if _, err := Load(); err == nil {
			t.Fatalf("Load() expected error for PORT=%q", bad)
		}
	}
}

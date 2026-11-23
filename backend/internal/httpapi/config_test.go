package httpapi

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// env builds a getenv function over a map.
func env(vars map[string]string) func(string) string {
	return func(name string) string { return vars[name] }
}

func TestLoadConfigRefusesToStartWithoutAKey(t *testing.T) {
	tests := []struct {
		name string
		vars map[string]string
	}{
		{"no key", map[string]string{}},
		{"empty key", map[string]string{"HMAC_SECRET_KEY": ""}},
		{"empty key and demo mode off", map[string]string{"HMAC_SECRET_KEY": "", "DEMO_MODE": "false"}},
		{"demo mode with another value", map[string]string{"DEMO_MODE": "TRUE"}},
		{"demo mode 1", map[string]string{"DEMO_MODE": "1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := LoadConfig(env(tt.vars))
			if !errors.Is(err, ErrMissingHMACKey) {
				t.Fatalf("LoadConfig = %+v, %v, want ErrMissingHMACKey", cfg, err)
			}
			if !strings.Contains(err.Error(), "HMAC_SECRET_KEY") {
				t.Errorf("error %q does not name the variable", err)
			}
			if len(cfg.HMACKey) != 0 {
				t.Errorf("a failed LoadConfig returned a key")
			}
		})
	}
}

func TestLoadConfigErrorNeverHoldsTheKey(t *testing.T) {
	// The only error is the missing-key error, and it is a fixed text. Check the
	// text against a key that a caller might have set elsewhere.
	_, err := LoadConfig(env(map[string]string{"STORAGE_DRIVER": "memory", "PORT": "9999"}))
	if err == nil {
		t.Fatal("LoadConfig succeeded without a key")
	}
	for _, secret := range []string{harnessKey, "9999"} {
		if strings.Contains(err.Error(), secret) {
			t.Errorf("error %q holds %q", err, secret)
		}
	}
}

func TestLoadConfigDemoModeGeneratesAKey(t *testing.T) {
	vars := map[string]string{"DEMO_MODE": "true"}

	a, err := LoadConfig(env(vars))
	if err != nil {
		t.Fatalf("LoadConfig = %v, want nil in Demo_Mode", err)
	}
	b, err := LoadConfig(env(vars))
	if err != nil {
		t.Fatalf("second LoadConfig = %v, want nil", err)
	}

	if !a.DemoMode {
		t.Error("DemoMode is false, want true")
	}
	if len(a.HMACKey) != 32 {
		t.Errorf("generated key has %d bytes, want 32", len(a.HMACKey))
	}
	if bytes.Equal(a.HMACKey, make([]byte, 32)) {
		t.Error("generated key is all zero bytes")
	}
	if bytes.Equal(a.HMACKey, b.HMACKey) {
		t.Error("two loads generated the same key")
	}
}

func TestLoadConfigDemoModeKeepsAConfiguredKey(t *testing.T) {
	cfg, err := LoadConfig(env(map[string]string{"DEMO_MODE": "true", "HMAC_SECRET_KEY": "from-the-environment"}))
	if err != nil {
		t.Fatalf("LoadConfig = %v, want nil", err)
	}
	if string(cfg.HMACKey) != "from-the-environment" {
		t.Errorf("HMACKey = %q, want the configured key", cfg.HMACKey)
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	cfg, err := LoadConfig(env(map[string]string{"HMAC_SECRET_KEY": "k"}))
	if err != nil {
		t.Fatalf("LoadConfig = %v, want nil", err)
	}
	if cfg.AllowedOrigin != "http://localhost:5173" {
		t.Errorf("AllowedOrigin = %q, want %q", cfg.AllowedOrigin, "http://localhost:5173")
	}
	if cfg.Port != "8080" {
		t.Errorf("Port = %q, want 8080", cfg.Port)
	}
	if cfg.StorageDriver != "memory" {
		t.Errorf("StorageDriver = %q, want memory", cfg.StorageDriver)
	}
	if cfg.DemoMode || cfg.OnLambda {
		t.Errorf("DemoMode %v OnLambda %v, want both false", cfg.DemoMode, cfg.OnLambda)
	}
	if string(cfg.HMACKey) != "k" {
		t.Errorf("HMACKey = %q, want the configured key", cfg.HMACKey)
	}
}

func TestLoadConfigReadsEveryVariable(t *testing.T) {
	cfg, err := LoadConfig(env(map[string]string{
		"HMAC_SECRET_KEY":          "k",
		"ALLOWED_ORIGIN":           "https://app.example.com",
		"PORT":                     "9000",
		"STORAGE_DRIVER":           "dynamodb",
		"DEMO_MODE":                "true",
		"AWS_LAMBDA_FUNCTION_NAME": "rukpay",
	}))
	if err != nil {
		t.Fatalf("LoadConfig = %v, want nil", err)
	}
	want := Config{
		StorageDriver: "dynamodb",
		AllowedOrigin: "https://app.example.com",
		Port:          "9000",
		DemoMode:      true,
		OnLambda:      true,
		HMACKey:       []byte("k"),
	}
	if cfg.StorageDriver != want.StorageDriver || cfg.AllowedOrigin != want.AllowedOrigin ||
		cfg.Port != want.Port || cfg.DemoMode != want.DemoMode || cfg.OnLambda != want.OnLambda ||
		!bytes.Equal(cfg.HMACKey, want.HMACKey) {
		t.Errorf("LoadConfig = %+v, want %+v", cfg, want)
	}
}

func TestLoadConfigDetectsLambda(t *testing.T) {
	tests := []struct {
		name     string
		function string
		want     bool
	}{
		{"unset", "", false},
		{"set", "rukpay-api", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := LoadConfig(env(map[string]string{"HMAC_SECRET_KEY": "k", "AWS_LAMBDA_FUNCTION_NAME": tt.function}))
			if err != nil {
				t.Fatalf("LoadConfig = %v, want nil", err)
			}
			if cfg.OnLambda != tt.want {
				t.Errorf("OnLambda = %v, want %v", cfg.OnLambda, tt.want)
			}
		})
	}
}

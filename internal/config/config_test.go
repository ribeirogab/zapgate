package config_test

import (
	"strings"
	"testing"

	"github.com/ribeirogab/zapgate/internal/config"
)

const validKey = "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"

func env(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func required() map[string]string {
	return map[string]string{
		"ZAPGATE_PUBLIC_URL":     "https://wpp.example.test/",
		"ZAPGATE_OWNER_PASSWORD": "secret",
		"ZAPGATE_SIGNING_KEY":    validKey,
	}
}

func TestFromEnvAppliesDefaults(t *testing.T) {
	cfg, err := config.FromEnv(env(required()))

	if err != nil {
		t.Fatal(err)
	}
	if cfg.DataDir != "/data" || cfg.Listen != "0.0.0.0:8080" || cfg.AdminSocket != "/run/zapgate/admin.sock" || cfg.RetentionMonths != 6 {
		t.Fatalf("defaults %+v", cfg)
	}
	if cfg.PublicURL != "https://wpp.example.test" {
		t.Fatalf("public URL %q keeps the trailing slash", cfg.PublicURL)
	}
	if len(cfg.SigningKey) != 32 {
		t.Fatalf("signing key has %d bytes", len(cfg.SigningKey))
	}
}

func TestFromEnvRejectsBadValues(t *testing.T) {
	cases := map[string]struct {
		key, value, message string
	}{
		"missing public URL":   {"ZAPGATE_PUBLIC_URL", "", "ZAPGATE_PUBLIC_URL is required"},
		"missing password":     {"ZAPGATE_OWNER_PASSWORD", "", "ZAPGATE_OWNER_PASSWORD is required"},
		"missing key":          {"ZAPGATE_SIGNING_KEY", "", "ZAPGATE_SIGNING_KEY is required"},
		"key not hex":          {"ZAPGATE_SIGNING_KEY", "zz", "ZAPGATE_SIGNING_KEY must be hex"},
		"key too short":        {"ZAPGATE_SIGNING_KEY", "0011", "at least 32 bytes"},
		"retention not number": {"ZAPGATE_RETENTION_MONTHS", "six", "ZAPGATE_RETENTION_MONTHS must be an integer from 1 to 1200"},
		"retention zero":       {"ZAPGATE_RETENTION_MONTHS", "0", "ZAPGATE_RETENTION_MONTHS must be an integer from 1 to 1200"},
		"retention too long":   {"ZAPGATE_RETENTION_MONTHS", "1201", "ZAPGATE_RETENTION_MONTHS must be an integer from 1 to 1200"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			values := required()
			values[tc.key] = tc.value

			_, err := config.FromEnv(env(values))

			if err == nil || !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("error %v, want %q", err, tc.message)
			}
		})
	}
}

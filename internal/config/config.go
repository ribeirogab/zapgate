package config

import (
	"encoding/hex"
	"errors"
	"fmt"
	"net/netip"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultDataDir         = "/data"
	DefaultListen          = "0.0.0.0:8080"
	DefaultAdminSocket     = "/run/zapgate/admin.sock"
	DefaultRetentionMonths = 6
	MinRetentionMonths     = 1
	MaxRetentionMonths     = 1200
	DefaultApprovalTimeout = 5 * time.Minute
	DefaultApprovalPing    = 25 * time.Second
	DefaultMediaFetch      = time.Minute
	minSigningKeyBytes     = 32
)

type Config struct {
	DataDir         string
	Listen          string
	AdminSocket     string
	PublicURL       string
	ClientIPHeader  string
	OwnerPassword   string
	SigningKey      []byte
	RetentionMonths int
	ApprovalTimeout time.Duration
	ApprovalPing    time.Duration

	MediaFetchTimeout time.Duration
	MediaFetchAllowed []netip.Prefix
}

func FromEnv(getenv func(string) string) (Config, error) {
	cfg := Config{
		DataDir:     valueOr(getenv("ZAPGATE_DATA_DIR"), DefaultDataDir),
		Listen:      ListenFromEnv(getenv),
		AdminSocket: AdminSocketFromEnv(getenv),
		PublicURL:   strings.TrimRight(getenv("ZAPGATE_PUBLIC_URL"), "/"),

		ClientIPHeader: strings.TrimSpace(getenv("ZAPGATE_CLIENT_IP_HEADER")),

		ApprovalTimeout: DefaultApprovalTimeout,
		ApprovalPing:    DefaultApprovalPing,

		MediaFetchTimeout: DefaultMediaFetch,
	}
	var errs []error
	if cfg.PublicURL == "" {
		errs = append(errs, errors.New("ZAPGATE_PUBLIC_URL is required"))
	}
	cfg.OwnerPassword = getenv("ZAPGATE_OWNER_PASSWORD")
	if cfg.OwnerPassword == "" {
		errs = append(errs, errors.New("ZAPGATE_OWNER_PASSWORD is required"))
	}
	key, err := signingKey(getenv("ZAPGATE_SIGNING_KEY"))
	errs = append(errs, err)
	cfg.SigningKey = key
	months, err := retentionMonths(getenv("ZAPGATE_RETENTION_MONTHS"))
	errs = append(errs, err)
	cfg.RetentionMonths = months
	return cfg, errors.Join(errs...)
}

func ListenFromEnv(getenv func(string) string) string {
	return valueOr(getenv("ZAPGATE_LISTEN"), DefaultListen)
}

func AdminSocketFromEnv(getenv func(string) string) string {
	return valueOr(getenv("ZAPGATE_ADMIN_SOCKET"), DefaultAdminSocket)
}

func signingKey(raw string) ([]byte, error) {
	if raw == "" {
		return nil, errors.New("ZAPGATE_SIGNING_KEY is required")
	}
	key, err := hex.DecodeString(raw)
	if err != nil {
		return nil, errors.New("ZAPGATE_SIGNING_KEY must be hex")
	}
	if len(key) < minSigningKeyBytes {
		return nil, fmt.Errorf("ZAPGATE_SIGNING_KEY must decode to at least %d bytes", minSigningKeyBytes)
	}
	return key, nil
}

func ValidRetentionMonths(months int) bool {
	return months >= MinRetentionMonths && months <= MaxRetentionMonths
}

func retentionMonths(raw string) (int, error) {
	if raw == "" {
		return DefaultRetentionMonths, nil
	}
	months, err := strconv.Atoi(raw)
	if err != nil || !ValidRetentionMonths(months) {
		return 0, fmt.Errorf("ZAPGATE_RETENTION_MONTHS must be an integer from %d to %d", MinRetentionMonths, MaxRetentionMonths)
	}
	return months, nil
}

func valueOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

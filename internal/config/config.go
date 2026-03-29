package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	TonAPIToken    string
	WatchAddresses []string

	WebhookURL     string
	WebhookSecret  []byte
	WebhookTimeout time.Duration

	RetryMaxAttempts int
	RetryBaseDelay   time.Duration
	RetryMaxDelay    time.Duration

	BoltDBPath string

	GapFillEnabled bool
	GapFillLimit   int

	HealthAddr string
	LogLevel   string
	MemLimit   int64

	Testnet bool
}

func Load() (Config, error) {
	c := Config{
		WebhookTimeout:   parseDuration("WEBHOOK_TIMEOUT", 10*time.Second),
		RetryMaxAttempts: parseInt("RETRY_MAX_ATTEMPTS", 5),
		RetryBaseDelay:   parseDuration("RETRY_BASE_DELAY", time.Second),
		RetryMaxDelay:    parseDuration("RETRY_MAX_DELAY", 60*time.Second),
		BoltDBPath:       envOr("BOLTDB_PATH", "./data/cursor.db"),
		GapFillEnabled:   envOr("GAP_FILL_ENABLED", "false") == "true",
		GapFillLimit:     parseInt("GAP_FILL_LIMIT", 5),
		HealthAddr:       envOr("HEALTH_ADDR", ":8080"),
		LogLevel:         envOr("LOG_LEVEL", "info"),
		MemLimit:         parseInt64("GOMEMLIMIT", 16777216),
		Testnet:          envOr("TESTNET", "false") == "true",
	}

	var missing []string

	c.TonAPIToken = os.Getenv("TONAPI_TOKEN")

	if addrs := os.Getenv("WATCH_ADDRESSES"); addrs != "" {
		c.WatchAddresses = strings.Split(addrs, ",")
		for i := range c.WatchAddresses {
			c.WatchAddresses[i] = strings.TrimSpace(c.WatchAddresses[i])
		}
	} else {
		missing = append(missing, "WATCH_ADDRESSES")
	}

	c.WebhookURL = os.Getenv("WEBHOOK_URL")
	if c.WebhookURL == "" {
		missing = append(missing, "WEBHOOK_URL")
	}

	secret := os.Getenv("WEBHOOK_SECRET")
	if secret == "" {
		missing = append(missing, "WEBHOOK_SECRET")
	} else if len(secret) < 32 {
		missing = append(missing, "WEBHOOK_SECRET (min 32 bytes)")
	} else {
		c.WebhookSecret = []byte(secret)
	}

	if len(missing) > 0 {
		return Config{}, fmt.Errorf("config: missing or invalid env vars: %s", strings.Join(missing, ", "))
	}

	return c, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func parseInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func parseInt64(key string, fallback int64) int64 {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return fallback
	}
	return n
}

func parseDuration(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}

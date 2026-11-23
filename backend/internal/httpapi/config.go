// Package httpapi serves the six public endpoints of RukPay over net/http. One
// handler is built once and used by both entrypoints, the local server and the
// Lambda function, so behavior is the same in both places (Requirement 10).
//
// The package maps every failure to one calm error body and writes a log line
// with the method, the route pattern, the status and the vault id only
// (Requirements 11.1, 11.2, 11.6).
package httpapi

import (
	"crypto/rand"
	"errors"
	"fmt"
)

// Environment variables read by LoadConfig.
const (
	envAllowedOrigin  = "ALLOWED_ORIGIN"
	envPort           = "PORT"
	envStorageDriver  = "STORAGE_DRIVER"
	envHMACKey        = "HMAC_SECRET_KEY"
	envDemoMode       = "DEMO_MODE"
	envLambdaFunction = "AWS_LAMBDA_FUNCTION_NAME"
)

// Defaults used when a variable is unset or empty.
const (
	defaultAllowedOrigin = "http://localhost:5173"
	defaultPort          = "8080"
	defaultStorageDriver = "memory"
)

// demoKeyBytes is the size of the random key generated in Demo_Mode when no key
// is configured.
const demoKeyBytes = 32

// ErrMissingHMACKey is returned by LoadConfig when HMAC_SECRET_KEY is unset or
// empty and Demo_Mode is off (Requirement 5.8). The text names the variable and
// never a value.
var ErrMissingHMACKey = errors.New("httpapi: " + envHMACKey + " is not set")

// Config is the startup configuration shared by both entrypoints.
type Config struct {
	// StorageDriver is "memory" or "dynamodb". It defaults to "memory".
	StorageDriver string
	// AllowedOrigin is the one browser origin that CORS allows. It defaults to
	// http://localhost:5173.
	AllowedOrigin string
	// Port is the port of the local server. It defaults to 8080.
	Port string
	// DemoMode turns on the demo fast-forward route and lets the service start
	// without a signing key.
	DemoMode bool
	// OnLambda is true when the process runs as a Lambda function.
	OnLambda bool
	// HMACKey signs guardian tokens. It is never written to a response, a log
	// line, a record or an event (Requirement 5.9).
	HMACKey []byte
}

// LoadConfig reads the configuration through getenv.
//
// It returns ErrMissingHMACKey when HMAC_SECRET_KEY is empty and DEMO_MODE is
// not "true". In Demo_Mode with no key it fills HMACKey with 32 random bytes
// from crypto/rand, so tokens work for the life of the process
// (Requirement 5.8). No error text contains the key (Requirement 5.9).
func LoadConfig(getenv func(string) string) (Config, error) {
	cfg := Config{
		StorageDriver: orDefault(getenv(envStorageDriver), defaultStorageDriver),
		AllowedOrigin: orDefault(getenv(envAllowedOrigin), defaultAllowedOrigin),
		Port:          orDefault(getenv(envPort), defaultPort),
		DemoMode:      getenv(envDemoMode) == "true",
		OnLambda:      getenv(envLambdaFunction) != "",
	}

	if key := getenv(envHMACKey); key != "" {
		cfg.HMACKey = []byte(key)
		return cfg, nil
	}
	if !cfg.DemoMode {
		return Config{}, ErrMissingHMACKey
	}

	cfg.HMACKey = make([]byte, demoKeyBytes)
	if _, err := rand.Read(cfg.HMACKey); err != nil {
		return Config{}, fmt.Errorf("httpapi: generate demo key: %w", err)
	}
	return cfg, nil
}

func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

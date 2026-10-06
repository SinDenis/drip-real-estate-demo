// Package security provides password and session credential primitives.
package security

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

const (
	argonMemory      = 64 * 1024
	argonIterations  = 3
	argonParallelism = 2
	argonSaltLength  = 16
	argonKeyLength   = 32
)

var compromisedPasswords = map[string]struct{}{
	"123456789012345": {}, "111111111111111": {}, "administrator": {},
	"adminadminadmin": {}, "changemechangeme": {}, "dragonball123456": {},
	"footballfootball": {}, "iloveyouiloveyou": {}, "letmeinletmeinletmein": {},
	"monkeymonkeymonkey": {}, "password123456": {}, "passwordpassword": {},
	"princessprincess": {}, "qwerty123456789": {}, "qwertyqwertyqwerty": {},
	"qwertyuiopasdfgh": {}, "starwarsstarwars": {}, "trustno1trustno1": {},
	"welcome123456789": {}, "zaq12wsxzaq12wsx": {},
}

// Credentials provides Argon2id password and opaque token primitives.
type Credentials struct{}

// NewCredentials creates a credential manager using production parameters.
func NewCredentials() Credentials {
	return Credentials{}
}

// ValidatePassword enforces length and the local compromised-password blocklist.
func (Credentials) ValidatePassword(password string) error {
	length := utf8.RuneCountInString(password)
	if !utf8.ValidString(password) || length < 15 || length > 128 {
		return errors.New("password must contain 15 to 128 Unicode characters")
	}
	normalized := strings.ToLower(strings.TrimSpace(password))
	if _, blocked := compromisedPasswords[normalized]; blocked {
		return errors.New("password is blocked")
	}
	return nil
}

// HashPassword creates a salted parameterized Argon2id hash.
func (Credentials) HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("read password salt randomness: %w", err)
	}
	hash := argon2.IDKey(
		[]byte(password), salt, argonIterations, argonMemory, argonParallelism, argonKeyLength,
	)
	return fmt.Sprintf(
		"$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		argonMemory,
		argonIterations,
		argonParallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	), nil
}

// VerifyPassword compares a password with a parameterized Argon2id hash.
func (Credentials) VerifyPassword(password string, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" {
		return false, errors.New("invalid Argon2id password hash")
	}
	parameters, err := parseParameters(parts[3])
	if err != nil {
		return false, err
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, fmt.Errorf("decode password hash salt: %w", err)
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, fmt.Errorf("decode password hash: %w", err)
	}
	actual := argon2.IDKey(
		[]byte(password), salt, parameters.iterations, parameters.memory,
		parameters.parallelism, uint32(len(expected)),
	)
	return subtle.ConstantTimeCompare(actual, expected) == 1, nil
}

// NewSessionToken returns an opaque token and the hash safe for persistence.
func (Credentials) NewSessionToken() (string, string, error) {
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return "", "", fmt.Errorf("read session randomness: %w", err)
	}
	raw := base64.RawURLEncoding.EncodeToString(random)
	return raw, hashToken(raw), nil
}

// NewTemporaryPassword returns a cryptographically random 24-character password.
func (Credentials) NewTemporaryPassword() (string, error) {
	random := make([]byte, 18)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("read temporary password randomness: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(random), nil
}

// HashSessionToken returns the persistence-safe digest of an opaque cookie value.
func (Credentials) HashSessionToken(raw string) string {
	return hashToken(raw)
}

type argonParameters struct {
	memory      uint32
	iterations  uint32
	parallelism uint8
}

func parseParameters(value string) (argonParameters, error) {
	parts := strings.Split(value, ",")
	if len(parts) != 3 {
		return argonParameters{}, errors.New("invalid Argon2id parameters")
	}
	memory, err := parseUint(strings.TrimPrefix(parts[0], "m="), 32)
	if err != nil {
		return argonParameters{}, fmt.Errorf("parse Argon2id memory: %w", err)
	}
	iterations, err := parseUint(strings.TrimPrefix(parts[1], "t="), 32)
	if err != nil {
		return argonParameters{}, fmt.Errorf("parse Argon2id iterations: %w", err)
	}
	parallelism, err := parseUint(strings.TrimPrefix(parts[2], "p="), 8)
	if err != nil {
		return argonParameters{}, fmt.Errorf("parse Argon2id parallelism: %w", err)
	}
	return argonParameters{
		memory: uint32(memory), iterations: uint32(iterations), parallelism: uint8(parallelism),
	}, nil
}

func parseUint(value string, bits int) (uint64, error) {
	parsed, err := strconv.ParseUint(value, 10, bits)
	if err != nil || parsed == 0 {
		return 0, errors.New("parameter must be a positive integer")
	}
	return parsed, nil
}

func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

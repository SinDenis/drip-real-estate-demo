package security_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/SinDenis/drip-real-estate-demo/backend/internal/adapters/security"
)

func TestCredentialsEnforcePasswordPolicyAndVerifyArgon2id(t *testing.T) {
	t.Parallel()
	credentials := security.NewCredentials()
	for _, password := range []string{"short", strings.Repeat("x", 129), "passwordpassword"} {
		if err := credentials.ValidatePassword(password); err == nil {
			t.Errorf("ValidatePassword(%q) succeeded, want policy error", password)
		}
	}
	const password = "Reliable analytics access 2026"
	if err := credentials.ValidatePassword(password); err != nil {
		t.Fatalf("ValidatePassword(valid): %v", err)
	}
	hash, err := credentials.HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	verified, err := credentials.VerifyPassword(password, hash)
	if err != nil || !verified {
		t.Fatalf("VerifyPassword(valid) = %v, %v", verified, err)
	}
	verified, err = credentials.VerifyPassword("Reliable analytics access 2027", hash)
	if err != nil || verified {
		t.Fatalf("VerifyPassword(wrong) = %v, %v", verified, err)
	}
}

func TestCredentialsRejectMalformedArgon2idHashes(t *testing.T) {
	t.Parallel()
	credentials := security.NewCredentials()
	for _, hash := range []string{
		"not-a-hash",
		"$argon2id$v=19$m=bad,t=3,p=2$c2FsdA$aGFzaA",
		"$argon2id$v=19$m=65536,t=bad,p=2$c2FsdA$aGFzaA",
		"$argon2id$v=19$m=65536,t=3,p=bad$c2FsdA$aGFzaA",
		"$argon2id$v=19$m=65536,t=3,p=2$!$aGFzaA",
		"$argon2id$v=19$m=65536,t=3,p=2$c2FsdA$!",
	} {
		if verified, err := credentials.VerifyPassword("password", hash); err == nil || verified {
			t.Errorf("VerifyPassword(%q) = %v, %v, want malformed hash error", hash, verified, err)
		}
	}
}

func TestCredentialsCreateOpaqueSessionAndTemporaryTokens(t *testing.T) {
	t.Parallel()
	credentials := security.NewCredentials()
	raw, digest, err := credentials.NewSessionToken()
	if err != nil {
		t.Fatalf("NewSessionToken: %v", err)
	}
	if raw == "" || digest == raw || credentials.HashSessionToken(raw) != digest {
		t.Fatalf("session token pair is invalid: raw=%q digest=%q", raw, digest)
	}
	temporary, err := credentials.NewTemporaryPassword()
	if err != nil {
		t.Fatalf("NewTemporaryPassword: %v", err)
	}
	if utf8.RuneCountInString(temporary) != 24 {
		t.Fatalf("temporary password length = %d, want 24", utf8.RuneCountInString(temporary))
	}
}

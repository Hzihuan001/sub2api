package cursor

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestTokenResponseUnmarshalCamelAndSnake(t *testing.T) {
	var camel TokenResponse
	if err := json.Unmarshal([]byte(`{"accessToken":"a","refreshToken":"r","authId":"x","expiresIn":60}`), &camel); err != nil {
		t.Fatalf("unmarshal camel: %v", err)
	}
	if camel.AccessToken != "a" || camel.RefreshToken != "r" || camel.AuthID != "x" || camel.ExpiresIn != 60 {
		t.Fatalf("camel decode mismatch: %+v", camel)
	}

	var snake TokenResponse
	if err := json.Unmarshal([]byte(`{"access_token":"a2","refresh_token":"r2","auth_id":"x2","expires_in":90}`), &snake); err != nil {
		t.Fatalf("unmarshal snake: %v", err)
	}
	if snake.AccessToken != "a2" || snake.RefreshToken != "r2" || snake.AuthID != "x2" || snake.ExpiresIn != 90 {
		t.Fatalf("snake decode mismatch: %+v", snake)
	}
}

func TestNewDeepLinkChallenge(t *testing.T) {
	verifier, challenge, id, err := NewDeepLinkChallenge()
	if err != nil {
		t.Fatalf("NewDeepLinkChallenge: %v", err)
	}
	if verifier == "" || challenge == "" || id == "" {
		t.Fatalf("empty challenge material: %q %q %q", verifier, challenge, id)
	}
	sum := sha256.Sum256([]byte(verifier))
	want := base64.RawURLEncoding.EncodeToString(sum[:])
	if challenge != want {
		t.Fatalf("challenge mismatch: got %q want %q", challenge, want)
	}

	loginURL := BuildLoginDeepControlURL(challenge, id)
	if !strings.HasPrefix(loginURL, DeepLinkLoginURL+"?") {
		t.Fatalf("unexpected login url: %q", loginURL)
	}
	for _, needle := range []string{"challenge=", "uuid=", "mode=login"} {
		if !strings.Contains(loginURL, needle) {
			t.Fatalf("login url missing %q: %q", needle, loginURL)
		}
	}
}

func TestJWTExpiry(t *testing.T) {
	makeJWT := func(payload string) string {
		return "h." + base64.RawURLEncoding.EncodeToString([]byte(payload)) + ".s"
	}

	exp := time.Now().Add(30 * time.Minute).Unix()
	token := makeJWT(`{"sub":"auth0|user_1","exp":` + jsonInt(exp) + `}`)
	got, ok := JWTExpiry(token)
	if !ok || got.Unix() != exp {
		t.Fatalf("JWTExpiry(%q) = %v,%v", token, got, ok)
	}

	// userId::JWT form is normalized first.
	got, ok = JWTExpiry("user_1::" + token)
	if !ok || got.Unix() != exp {
		t.Fatalf("JWTExpiry cookie form = %v,%v", got, ok)
	}

	if _, ok := JWTExpiry("not-a-jwt"); ok {
		t.Fatal("expected !ok for non-JWT input")
	}
	if _, ok := JWTExpiry(makeJWT(`{"sub":"x"}`)); ok {
		t.Fatal("expected !ok when exp claim missing")
	}
}

func jsonInt(v int64) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func TestIsUserAPIKey(t *testing.T) {
	if !IsUserAPIKey("crsr_abc") || !IsUserAPIKey("  crsr_abc  ") {
		t.Fatal("expected crsr_ prefix to be detected")
	}
	if IsUserAPIKey("sk-abc") || IsUserAPIKey("") {
		t.Fatal("expected non-crsr_ values to be rejected")
	}
}

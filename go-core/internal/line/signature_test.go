package line

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"testing"
)

func makeSignature(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func TestVerifySignature_KnownGoodVector(t *testing.T) {
	secret := "test-channel-secret"
	body := []byte(`{"events":[{"type":"message"}]}`)
	sig := makeSignature(secret, body)

	if !VerifySignature(secret, body, sig) {
		t.Fatal("expected signature to be valid")
	}
}

func TestVerifySignature_TamperedBody(t *testing.T) {
	secret := "test-channel-secret"
	body := []byte(`{"events":[{"type":"message"}]}`)
	sig := makeSignature(secret, body)

	tampered := []byte(`{"events":[{"type":"message","evil":true}]}`)
	if VerifySignature(secret, tampered, sig) {
		t.Fatal("expected tampered body to fail verification")
	}
}

func TestVerifySignature_WrongSecret(t *testing.T) {
	body := []byte(`{"events":[]}`)
	sig := makeSignature("real-secret", body)

	if VerifySignature("wrong-secret", body, sig) {
		t.Fatal("expected wrong secret to fail verification")
	}
}

func TestVerifySignature_EmptySignature(t *testing.T) {
	if VerifySignature("secret", []byte("body"), "") {
		t.Fatal("expected empty signature to return false")
	}
}

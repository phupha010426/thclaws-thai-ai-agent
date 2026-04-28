package liff

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/thaiaiagent/go-core/internal/users"
)

const sessionTTL = 10 * time.Minute

type Session struct {
	UserID    string `json:"userId"`
	AgentID   string `json:"agentId"`
	Namespace string `json:"namespace"`
	ExpiresAt int64  `json:"expiresAt"`
}

func Sign(identity users.Identity, secret string, now time.Time) (string, error) {
	if strings.TrimSpace(secret) == "" {
		return "", errors.New("liff session: secret required")
	}
	s := Session{
		UserID:    identity.UserID,
		AgentID:   identity.AgentID,
		Namespace: identity.Namespace,
		ExpiresAt: now.Add(sessionTTL).Unix(),
	}
	body, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	payload := base64.RawURLEncoding.EncodeToString(body)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return payload + "." + sig, nil
}

func Verify(token, secret string, now time.Time) (Session, error) {
	if strings.TrimSpace(secret) == "" {
		return Session{}, errors.New("liff session: secret required")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return Session{}, errors.New("liff session: invalid token")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(parts[0]))
	expected := mac.Sum(nil)
	got, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Session{}, errors.New("liff session: invalid signature encoding")
	}
	if !hmac.Equal(got, expected) {
		return Session{}, errors.New("liff session: signature mismatch")
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return Session{}, errors.New("liff session: invalid payload encoding")
	}
	var s Session
	if err := json.Unmarshal(body, &s); err != nil {
		return Session{}, fmt.Errorf("liff session: payload: %w", err)
	}
	if s.UserID == "" || s.AgentID == "" || s.Namespace == "" {
		return Session{}, errors.New("liff session: missing identity")
	}
	if now.Unix() > s.ExpiresAt {
		return Session{}, errors.New("liff session: expired")
	}
	return s, nil
}

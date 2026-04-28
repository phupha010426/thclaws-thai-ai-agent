// Package line implements LINE Messaging API integration for the Go service.
// Hard constraint: this package must NEVER call push, multicast, or broadcast
// endpoints. Every outbound message to LINE must use a replyToken obtained from
// an inbound webhook event (verified by VerifySignature below).
package line

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
)

// VerifySignature checks the X-Line-Signature header against the raw request
// body using HMAC-SHA256.  constant-time compare prevents timing attacks that
// could otherwise reveal the channel secret byte-by-byte.
func VerifySignature(channelSecret string, body []byte, headerSig string) bool {
	if headerSig == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(channelSecret))
	mac.Write(body)
	expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(headerSig))
}

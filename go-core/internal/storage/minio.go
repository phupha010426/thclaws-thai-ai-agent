package storage

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

type ObjectStore struct {
	endpoint  string
	accessKey string
	secretKey string
	bucket    string
	scheme    string
	http      *http.Client
	logger    *slog.Logger
}

func NewMinIO(endpoint string, accessKey, secretKey, bucket string, useSSL bool, logger *slog.Logger) (*ObjectStore, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" || accessKey == "" || secretKey == "" || bucket == "" {
		return nil, fmt.Errorf("storage: endpoint/access/secret/bucket required")
	}
	scheme := "http"
	if useSSL {
		scheme = "https"
	}
	endpoint = strings.TrimPrefix(strings.TrimPrefix(endpoint, "https://"), "http://")
	return &ObjectStore{
		endpoint:  endpoint,
		accessKey: accessKey,
		secretKey: secretKey,
		bucket:    bucket,
		scheme:    scheme,
		http:      &http.Client{Timeout: 20 * time.Second},
		logger:    logger,
	}, nil
}

func (s *ObjectStore) EnsureBucket(ctx context.Context) error {
	resp, _, err := s.do(ctx, http.MethodHead, "/"+s.bucket, nil, "", "")
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusOK {
		return nil
	}
	if resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("storage: bucket head status %d", resp.StatusCode)
	}
	resp, body, err := s.do(ctx, http.MethodPut, "/"+s.bucket, nil, "", "")
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("storage: create bucket status %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	s.logger.Info("minio bucket created", "bucket", s.bucket)
	return nil
}

func (s *ObjectStore) Put(ctx context.Context, objectName string, data []byte, contentType string) (bucket string, err error) {
	if len(data) == 0 {
		return "", fmt.Errorf("storage: empty object")
	}
	path := "/" + s.bucket + "/" + encodePath(objectName)
	resp, body, err := s.do(ctx, http.MethodPut, path, data, contentType, sha256Hex(data))
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("storage: put object status %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	return s.bucket, nil
}

func (s *ObjectStore) Get(ctx context.Context, bucket, objectName string) ([]byte, string, error) {
	if bucket == "" {
		bucket = s.bucket
	}
	path := "/" + bucket + "/" + encodePath(objectName)
	resp, body, err := s.do(ctx, http.MethodGet, path, nil, "", "")
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("storage: get object status %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	return body, contentType, nil
}

func (s *ObjectStore) do(ctx context.Context, method, path string, payload []byte, contentType, payloadHash string) (*http.Response, []byte, error) {
	if payloadHash == "" {
		payloadHash = sha256Hex(payload)
	}
	u := s.scheme + "://" + s.endpoint + path
	req, err := http.NewRequestWithContext(ctx, method, u, bytes.NewReader(payload))
	if err != nil {
		return nil, nil, fmt.Errorf("storage: request: %w", err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Host", s.endpoint)
	req.Host = s.endpoint
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)
	req.Header.Set("X-Amz-Date", time.Now().UTC().Format("20060102T150405Z"))
	s.sign(req, payloadHash)

	resp, err := s.http.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("storage: http: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp, raw, nil
}

func (s *ObjectStore) sign(req *http.Request, payloadHash string) {
	amzDate := req.Header.Get("X-Amz-Date")
	date := amzDate[:8]
	scope := date + "/us-east-1/s3/aws4_request"

	signedHeaders := signedHeaderNames(req.Header)
	canonicalHeaders := canonicalHeaders(req.Header)
	canonical := strings.Join([]string{
		req.Method,
		req.URL.EscapedPath(),
		req.URL.RawQuery,
		canonicalHeaders,
		strings.Join(signedHeaders, ";"),
		payloadHash,
	}, "\n")

	canonicalHash := sha256Hex([]byte(canonical))
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		scope,
		canonicalHash,
	}, "\n")
	signingKey := hmacSHA256([]byte("AWS4"+s.secretKey), date)
	signingKey = hmacSHA256(signingKey, "us-east-1")
	signingKey = hmacSHA256(signingKey, "s3")
	signingKey = hmacSHA256(signingKey, "aws4_request")
	signature := hex.EncodeToString(hmacSHA256(signingKey, stringToSign))

	req.Header.Set("Authorization", fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		s.accessKey, scope, strings.Join(signedHeaders, ";"), signature,
	))
}

func canonicalHeaders(h http.Header) string {
	names := signedHeaderNames(h)
	var b strings.Builder
	for _, name := range names {
		values := h.Values(name)
		for i := range values {
			values[i] = strings.Join(strings.Fields(values[i]), " ")
		}
		b.WriteString(strings.ToLower(name))
		b.WriteByte(':')
		b.WriteString(strings.Join(values, ","))
		b.WriteByte('\n')
	}
	return b.String()
}

func signedHeaderNames(h http.Header) []string {
	names := make([]string, 0, len(h))
	for name := range h {
		names = append(names, strings.ToLower(name))
	}
	sort.Strings(names)
	return names
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func hmacSHA256(key []byte, data string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(data))
	return mac.Sum(nil)
}

func encodePath(v string) string {
	parts := strings.Split(v, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

package media

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/thaiaiagent/go-core/internal/memory"
)

type ImageStore interface {
	Get(ctx context.Context, bucket, objectName string) ([]byte, string, error)
}

type Handler struct {
	memory *memory.MemoryService
	store  ImageStore
	secret string
}

func NewHandler(memorySvc *memory.MemoryService, store ImageStore, secret string) *Handler {
	return &Handler{memory: memorySvc, store: store, secret: strings.TrimSpace(secret)}
}

func (h *Handler) Register(r chi.Router) {
	r.Get("/media/images/{id}", h.ServeImage)
	r.Get("/v2/media/images/{id}", h.ServeImage)
}

func (h *Handler) ServeImage(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	exp := r.URL.Query().Get("exp")
	sig := r.URL.Query().Get("sig")
	if err := Verify(id, exp, sig, h.secret, time.Now()); err != nil {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		http.Error(w, "bad image id", http.StatusBadRequest)
		return
	}
	img, err := h.memory.GetStoredImageByID(r.Context(), objectID)
	if err != nil {
		http.Error(w, "image lookup failed", http.StatusInternalServerError)
		return
	}
	if img == nil {
		http.NotFound(w, r)
		return
	}
	data, contentType, err := h.store.Get(r.Context(), img.Bucket, img.ObjectName)
	if err != nil {
		http.Error(w, "image unavailable", http.StatusBadGateway)
		return
	}
	if img.ContentType != "" {
		contentType = img.ContentType
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "private, max-age=60")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(data)
}

func BuildImageURL(baseURL, id, secret string, ttl time.Duration) (string, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return "", fmt.Errorf("media: base URL required")
	}
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	exp := strconv.FormatInt(time.Now().Add(ttl).Unix(), 10)
	sig, err := Sign(id, exp, secret)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s/v2/media/images/%s?exp=%s&sig=%s",
		baseURL, url.PathEscape(id), url.QueryEscape(exp), url.QueryEscape(sig)), nil
}

func Sign(id, exp, secret string) (string, error) {
	secret = strings.TrimSpace(secret)
	if id == "" || exp == "" {
		return "", fmt.Errorf("media: id and exp required")
	}
	if secret == "" {
		return "", fmt.Errorf("media: signing secret required")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(id + "." + exp))
	return hex.EncodeToString(mac.Sum(nil)), nil
}

func Verify(id, exp, sig, secret string, now time.Time) error {
	want, err := Sign(id, exp, secret)
	if err != nil {
		return err
	}
	if !hmac.Equal([]byte(want), []byte(strings.TrimSpace(sig))) {
		return errors.New("media: bad signature")
	}
	ts, err := strconv.ParseInt(exp, 10, 64)
	if err != nil {
		return fmt.Errorf("media: bad exp: %w", err)
	}
	if now.Unix() > ts {
		return errors.New("media: signature expired")
	}
	return nil
}

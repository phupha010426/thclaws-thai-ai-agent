package brain

import "context"

// GatewayClient is satisfied by the real SMLGateway client. The interface is
// defined here (not in the agents package) so brain tests can inject a mock
// without importing the concrete HTTP client.
type GatewayClient interface {
	// Embed returns a float64 slice of the requested dimension produced by
	// model (or one of fallbackModels if the primary fails).
	Embed(ctx context.Context, req EmbedRequest) ([]float64, error)

	// Chat returns the assistant's text response for the given messages.
	Chat(ctx context.Context, req ChatRequest) (string, error)
}

// EmbedRequest mirrors the fields consumed by SMLGateway.embed.
type EmbedRequest struct {
	Model          string
	FallbackModels []string
	Text           string
}

// ChatMessage is a single turn in a conversation.
type ChatMessage struct {
	Role    string // "system" | "user" | "assistant"
	Content string
}

// ChatRequest mirrors the fields used by ExtractionService when calling the
// LLM for entity extraction. Cacheable is always false for extraction calls
// because the response depends on ephemeral user text.
type ChatRequest struct {
	Namespace      string
	Model          string
	FallbackModels []string
	MaxTokens      int
	Messages       []ChatMessage
}

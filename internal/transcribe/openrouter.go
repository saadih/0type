package transcribe

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	// OpenRouterEndpoint is OpenRouter's speech-to-text API. It takes the same
	// bearer key as chat completions.
	OpenRouterEndpoint = "https://openrouter.ai/api/v1/audio/transcriptions"
	// DefaultOpenRouterModel is used when no transcription model is configured:
	// Whisper large-v3 turbo, fast and multilingual (Swedish included).
	DefaultOpenRouterModel = "openai/whisper-large-v3-turbo"
)

// OpenRouter transcribes audio with a hosted speech-to-text model via
// OpenRouter. It's the cloud alternative to local Parakeet for machines where
// local inference is slow.
type OpenRouter struct {
	APIKey string
	Model  string
	Client *http.Client
}

// NewOpenRouter returns an OpenRouter transcriber. The API key must come from
// the user's settings; it is never hardcoded. An empty model means
// DefaultOpenRouterModel.
func NewOpenRouter(apiKey, model string) *OpenRouter {
	if model == "" {
		model = DefaultOpenRouterModel
	}
	return &OpenRouter{
		APIKey: apiKey,
		Model:  model,
		Client: &http.Client{Timeout: 90 * time.Second}, // OpenRouter's upstream limit is 60 s
	}
}

// Transcribe uploads a WAV file (base64 in a JSON body, OpenRouter's native
// shape) and returns the recognized text.
func (o *OpenRouter) Transcribe(wav []byte) (string, error) {
	payload := map[string]any{
		"model": o.Model,
		"input_audio": map[string]string{
			"data":   base64.StdEncoding.EncodeToString(wav),
			"format": "wav",
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequest(http.MethodPost, OpenRouterEndpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+o.APIKey)
	req.Header.Set("HTTP-Referer", "https://github.com/saadih/0type")
	req.Header.Set("X-Title", "0type")

	resp, err := o.Client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return "", fmt.Errorf("openrouter: %s: %s", resp.Status, bytes.TrimSpace(b))
	}

	var out struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.Text), nil
}

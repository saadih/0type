package transcribe

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"
)

const (
	groqEndpoint = "https://api.groq.com/openai/v1/audio/transcriptions"
	groqModel    = "whisper-large-v3-turbo"
)

// Groq transcribes audio with Groq's hosted Whisper (an OpenAI-compatible API).
// It's the cloud stand-in used to prove the pipeline before local Parakeet slots
// in behind the same Transcriber interface.
type Groq struct {
	APIKey string
	Model  string
	Client *http.Client
}

// NewGroq returns a Groq transcriber. The API key must come from the caller
// (which reads it from the environment) — it is never hardcoded.
func NewGroq(apiKey string) *Groq {
	return &Groq{
		APIKey: apiKey,
		Model:  groqModel,
		Client: &http.Client{Timeout: 60 * time.Second},
	}
}

// Transcribe uploads a WAV file and returns the recognized text.
func (g *Groq) Transcribe(wav []byte) (string, error) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	fw, err := w.CreateFormFile("file", "audio.wav")
	if err != nil {
		return "", err
	}
	if _, err := fw.Write(wav); err != nil {
		return "", err
	}
	if err := w.WriteField("model", g.Model); err != nil {
		return "", err
	}
	if err := w.WriteField("response_format", "json"); err != nil {
		return "", err
	}
	if err := w.Close(); err != nil {
		return "", err
	}

	req, err := http.NewRequest(http.MethodPost, groqEndpoint, &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+g.APIKey)
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := g.Client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return "", fmt.Errorf("groq: %s: %s", resp.Status, bytes.TrimSpace(b))
	}

	var out struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.Text), nil
}

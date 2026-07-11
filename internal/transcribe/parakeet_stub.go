//go:build !parakeet

package transcribe

import "fmt"

// Parakeet is unavailable unless built with -tags parakeet (CGO + sherpa-onnx).
type Parakeet struct{}

// NewParakeet reports that Parakeet isn't compiled in.
func NewParakeet(dir string) (*Parakeet, error) {
	return nil, fmt.Errorf("parakeet not compiled in (build with -tags parakeet)")
}

// Transcribe is unsupported in the stub build.
func (p *Parakeet) Transcribe(wav []byte) (string, error) {
	return "", fmt.Errorf("parakeet not compiled in")
}

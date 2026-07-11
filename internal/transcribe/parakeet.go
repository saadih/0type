//go:build parakeet

package transcribe

import (
	"encoding/binary"
	"fmt"
	"path/filepath"
	"strings"

	sherpa "github.com/k2-fsa/sherpa-onnx-go/sherpa_onnx"
)

// Parakeet transcribes locally with NVIDIA Parakeet TDT v3 via sherpa-onnx (CGO).
// Built only with -tags parakeet; the recognizer loads the model once and reuses it.
type Parakeet struct {
	rec *sherpa.OfflineRecognizer
}

// NewParakeet loads the Parakeet model from dir (a sherpa-onnx int8 export).
func NewParakeet(dir string) (*Parakeet, error) {
	c := sherpa.OfflineRecognizerConfig{}
	c.FeatConfig.SampleRate = 16000
	c.FeatConfig.FeatureDim = 80
	c.ModelConfig.Transducer.Encoder = filepath.Join(dir, "encoder.int8.onnx")
	c.ModelConfig.Transducer.Decoder = filepath.Join(dir, "decoder.int8.onnx")
	c.ModelConfig.Transducer.Joiner = filepath.Join(dir, "joiner.int8.onnx")
	c.ModelConfig.Tokens = filepath.Join(dir, "tokens.txt")
	c.ModelConfig.ModelType = "nemo_transducer"
	c.ModelConfig.NumThreads = 4
	c.ModelConfig.Provider = "cpu"
	c.DecodingMethod = "greedy_search"

	rec := sherpa.NewOfflineRecognizer(&c)
	if rec == nil {
		return nil, fmt.Errorf("failed to load Parakeet model from %s", dir)
	}
	return &Parakeet{rec: rec}, nil
}

// Transcribe runs a 16 kHz mono 16-bit WAV through the recognizer.
func (p *Parakeet) Transcribe(wav []byte) (string, error) {
	samples := wavToFloat32(wav)
	if len(samples) == 0 {
		return "", nil
	}
	stream := sherpa.NewOfflineStream(p.rec)
	defer sherpa.DeleteOfflineStream(stream)
	stream.AcceptWaveform(16000, samples)
	p.rec.Decode(stream)
	return strings.TrimSpace(stream.GetResult().Text), nil
}

// wavToFloat32 converts 16-bit PCM WAV bytes (44-byte header) to normalized floats.
func wavToFloat32(wav []byte) []float32 {
	if len(wav) <= 44 {
		return nil
	}
	pcm := wav[44:]
	n := len(pcm) / 2
	out := make([]float32, n)
	for i := 0; i < n; i++ {
		out[i] = float32(int16(binary.LittleEndian.Uint16(pcm[i*2:]))) / 32768.0
	}
	return out
}

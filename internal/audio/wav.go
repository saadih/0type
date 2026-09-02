package audio

import (
	"bytes"
	"encoding/binary"
)

// Capture format: 16 kHz mono 16-bit PCM, what Parakeet and the cloud APIs expect.
const (
	SampleRate    = 16000
	channels      = 1
	bitsPerSample = 16
	waveFormatPCM = 1

	// BytesPerSecond is the size of one second of captured PCM.
	BytesPerSecond = SampleRate * channels * (bitsPerSample / 8)
	// HeaderBytes is the size of the RIFF/WAVE header EncodeWAV writes.
	HeaderBytes = 44
)

// EncodeWAV wraps 16 kHz mono 16-bit PCM in a minimal RIFF/WAVE container.
func EncodeWAV(pcm []byte) []byte {
	var b bytes.Buffer
	b.Grow(HeaderBytes + len(pcm))
	dataLen := uint32(len(pcm))
	blockAlign := uint16(channels * (bitsPerSample / 8))
	byteRate := uint32(SampleRate) * uint32(blockAlign)

	b.WriteString("RIFF")
	binary.Write(&b, binary.LittleEndian, uint32(36+dataLen))
	b.WriteString("WAVE")
	b.WriteString("fmt ")
	binary.Write(&b, binary.LittleEndian, uint32(16))
	binary.Write(&b, binary.LittleEndian, uint16(waveFormatPCM))
	binary.Write(&b, binary.LittleEndian, uint16(channels))
	binary.Write(&b, binary.LittleEndian, uint32(SampleRate))
	binary.Write(&b, binary.LittleEndian, byteRate)
	binary.Write(&b, binary.LittleEndian, blockAlign)
	binary.Write(&b, binary.LittleEndian, uint16(bitsPerSample))
	b.WriteString("data")
	binary.Write(&b, binary.LittleEndian, dataLen)
	b.Write(pcm)
	return b.Bytes()
}

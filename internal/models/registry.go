package models

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// Qwen is the cleanup model: Qwen3-4B-Instruct-2507, Q4_K_M GGUF (~2.5 GB), run
// by llama-server. The Instruct build has no thinking mode, so cleanup never
// burns its token budget on a <think> trace.
func Qwen() Asset {
	return Asset{
		ID:       "qwen",
		Name:     "Qwen3-4B-Instruct-2507 (cleanup)",
		URL:      "https://huggingface.co/unsloth/Qwen3-4B-Instruct-2507-GGUF/resolve/main/Qwen3-4B-Instruct-2507-Q4_K_M.gguf",
		Filename: "Qwen3-4B-Instruct-2507-Q4_K_M.gguf",
		Bytes:    2500 * 1024 * 1024,
	}
}

// Parakeet is the transcription model: NVIDIA Parakeet TDT 0.6B v3, int8, for
// sherpa-onnx (~600 MB tar.bz2; 25 European languages incl. Swedish).
func Parakeet() Asset {
	return Asset{
		ID:       "parakeet",
		Name:     "Parakeet TDT 0.6B v3 (transcription)",
		URL:      "https://github.com/k2-fsa/sherpa-onnx/releases/download/asr-models/sherpa-onnx-nemo-parakeet-tdt-0.6b-v3-int8.tar.bz2",
		Filename: "sherpa-onnx-nemo-parakeet-tdt-0.6b-v3-int8.tar.bz2",
	}
}

// LlamaServer resolves the latest llama.cpp Windows Vulkan release (the ~31 MB
// server that runs the Qwen GGUF) from the GitHub API, so it is never pinned to
// a stale build number.
func LlamaServer() (Asset, error) {
	req, err := http.NewRequest(http.MethodGet, "https://api.github.com/repos/ggml-org/llama.cpp/releases/latest", nil)
	if err != nil {
		return Asset{}, err
	}
	req.Header.Set("User-Agent", "0type")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Asset{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Asset{}, fmt.Errorf("llama.cpp releases: %s", resp.Status)
	}
	var rel struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
			Size int64  `json:"size"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return Asset{}, err
	}
	for _, a := range rel.Assets {
		if strings.HasSuffix(a.Name, "bin-win-vulkan-x64.zip") {
			return Asset{
				ID:       "llama-server",
				Name:     "llama.cpp server (" + rel.TagName + ")",
				URL:      a.URL,
				Filename: a.Name,
				Bytes:    a.Size,
			}, nil
		}
	}
	return Asset{}, fmt.Errorf("no win-vulkan asset in llama.cpp %s", rel.TagName)
}

//go:build !windows

package models

import "fmt"

// Server is a running llama-server process (Windows-only for now).
type Server struct{}

// StartLlama is unsupported off Windows.
func StartLlama() (*Server, string, error) {
	return nil, "", fmt.Errorf("bundled llama-server is Windows-only")
}

// ExtractLlama is unsupported off Windows.
func ExtractLlama() (string, error) {
	return "", fmt.Errorf("bundled llama-server is Windows-only")
}

// Stop is a no-op off Windows.
func (s *Server) Stop() {}

//go:build windows

package models

import (
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// LlamaPort is where 0type runs its bundled llama-server for cleanup.
const LlamaPort = "8719"

// Server is a running llama-server process.
type Server struct {
	cmd *exec.Cmd
}

func binDir() string { return filepath.Join(Dir(), "llama-bin") }

func findServerExe() string {
	var found string
	filepath.Walk(binDir(), func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		name := strings.ToLower(info.Name())
		if strings.HasPrefix(name, "llama-server") && strings.HasSuffix(name, ".exe") {
			found = p
		}
		return nil
	})
	return found
}

func findDownloadedZip() string {
	m, _ := filepath.Glob(filepath.Join(Dir(), "llama-*-bin-win-vulkan-x64.zip"))
	if len(m) > 0 {
		return m[0]
	}
	return ""
}

// ExtractLlama unzips the downloaded llama.cpp release into binDir (once) and
// returns the path to llama-server.exe.
func ExtractLlama() (string, error) {
	if exe := findServerExe(); exe != "" {
		return exe, nil
	}
	zipPath := findDownloadedZip()
	if zipPath == "" {
		return "", fmt.Errorf("llama-server not downloaded")
	}
	if err := unzip(zipPath, binDir()); err != nil {
		return "", err
	}
	exe := findServerExe()
	if exe == "" {
		return "", fmt.Errorf("llama-server.exe not found in the downloaded archive")
	}
	return exe, nil
}

func healthy(url string) bool {
	c := &http.Client{Timeout: time.Second}
	resp, err := c.Get(url + "/health")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// StartLlama extracts (if needed) and spawns llama-server against the Qwen GGUF,
// returning the base URL once /health responds. If a server is already running
// on the port (e.g. orphaned from a previous run), it is reused.
func StartLlama() (*Server, string, error) {
	url := "http://127.0.0.1:" + LlamaPort
	if healthy(url) {
		return &Server{}, url, nil
	}

	exe, err := ExtractLlama()
	if err != nil {
		return nil, "", err
	}
	if !Qwen().Installed() {
		return nil, "", fmt.Errorf("qwen model not downloaded")
	}

	cmd := exec.Command(exe,
		"-m", Qwen().Path(),
		"--host", "127.0.0.1", "--port", LlamaPort,
		"-c", "4096", "-ngl", "99", "--jinja",
	)
	cmd.Dir = filepath.Dir(exe) // so it finds its sibling DLLs
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := cmd.Start(); err != nil {
		return nil, "", err
	}
	s := &Server{cmd: cmd}

	client := &http.Client{Timeout: 2 * time.Second}
	for i := 0; i < 120; i++ { // up to ~60s for the model to load
		time.Sleep(500 * time.Millisecond)
		if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
			return nil, "", fmt.Errorf("llama-server exited during startup")
		}
		if resp, err := client.Get(url + "/health"); err == nil {
			ready := resp.StatusCode == http.StatusOK
			resp.Body.Close()
			if ready {
				return s, url, nil
			}
		}
	}
	s.Stop()
	return nil, "", fmt.Errorf("llama-server did not become ready")
}

// Stop terminates the server process (no-op for a reused external server).
func (s *Server) Stop() {
	if s != nil && s.cmd != nil && s.cmd.Process != nil {
		s.cmd.Process.Kill()
	}
}

// unzip extracts src into dst with a zip-slip guard.
func unzip(src, dst string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	clean := filepath.Clean(dst)
	for _, f := range r.File {
		fp := filepath.Join(dst, f.Name)
		if fp != clean && !strings.HasPrefix(fp, clean+string(os.PathSeparator)) {
			return fmt.Errorf("unsafe zip entry: %s", f.Name)
		}
		if f.FileInfo().IsDir() {
			os.MkdirAll(fp, 0o755)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(fp), 0o755); err != nil {
			return err
		}
		out, err := os.Create(fp)
		if err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			out.Close()
			return err
		}
		_, err = io.Copy(out, rc)
		out.Close()
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

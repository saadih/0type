// Package models manages 0type's downloadable models and runtime binaries. None
// of these are committed to the repo — they are large and fetched on demand into
// the user's data dir when the user clicks "Download" in the settings window.
package models

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

// Dir is 0type's data directory for downloaded models and binaries
// (%LOCALAPPDATA%\0type on Windows).
func Dir() string {
	base, err := os.UserCacheDir()
	if err != nil {
		base = "."
	}
	return filepath.Join(base, "0type")
}

// Asset is a downloadable file: a model, or a runtime binary/archive.
type Asset struct {
	ID       string // stable identifier, e.g. "qwen" or "parakeet"
	Name     string // human-readable, for the UI
	URL      string // official release / HuggingFace URL
	Filename string // local filename under Dir()
	Bytes    int64  // approximate size (0 if unknown) — for progress + display
}

// Path is where the asset lives locally once downloaded.
func (a Asset) Path() string { return filepath.Join(Dir(), a.Filename) }

// Installed reports whether the asset is already downloaded. The .part -> rename
// in Download guarantees a file at Path() is complete, so existence is enough (a
// hardcoded size-check would wrongly reject a good download whose real size
// differs by a byte from our estimate).
func (a Asset) Installed() bool {
	fi, err := os.Stat(a.Path())
	return err == nil && fi.Size() > 0
}

// Download fetches the asset into Dir(), calling progress(done, total) as it
// streams. It writes to a ".part" file and renames on success, so an interrupted
// download never looks complete.
func Download(a Asset, progress func(done, total int64)) error {
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		return err
	}
	resp, err := http.Get(a.URL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: %s", a.ID, resp.Status)
	}

	total := resp.ContentLength
	if total <= 0 {
		total = a.Bytes
	}

	part := a.Path() + ".part"
	f, err := os.Create(part)
	if err != nil {
		return err
	}
	pw := &progressWriter{w: f, total: total, cb: progress}
	if _, err := io.Copy(pw, resp.Body); err != nil {
		f.Close()
		os.Remove(part)
		return err
	}
	if progress != nil {
		progress(pw.done, total) // ensure a final 100% tick
	}
	if err := f.Close(); err != nil {
		os.Remove(part)
		return err
	}
	return os.Rename(part, a.Path())
}

// progressWriter tees byte counts to a throttled callback as it writes.
type progressWriter struct {
	w     io.Writer
	done  int64
	total int64
	last  int64
	cb    func(done, total int64)
}

func (p *progressWriter) Write(b []byte) (int, error) {
	n, err := p.w.Write(b)
	p.done += int64(n)
	if p.cb != nil && (p.done-p.last >= 512*1024 || err != nil) {
		p.last = p.done
		p.cb(p.done, p.total)
	}
	return n, err
}

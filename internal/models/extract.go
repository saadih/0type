package models

import (
	"archive/tar"
	"compress/bzip2"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ParakeetDir is the extracted Parakeet model directory.
func ParakeetDir() string {
	return filepath.Join(Dir(), "sherpa-onnx-nemo-parakeet-tdt-0.6b-v3-int8")
}

// ExtractParakeet extracts the downloaded Parakeet tar.bz2 into Dir() (once) and
// returns the model directory.
func ExtractParakeet() (string, error) {
	if fi, err := os.Stat(ParakeetDir()); err == nil && fi.IsDir() {
		return ParakeetDir(), nil
	}
	if !Parakeet().Installed() {
		return "", fmt.Errorf("parakeet model not downloaded")
	}
	if err := untarBz2(Parakeet().Path(), Dir()); err != nil {
		return "", err
	}
	return ParakeetDir(), nil
}

// untarBz2 extracts a .tar.bz2 archive into dst with a path-traversal guard.
func untarBz2(src, dst string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	tr := tar.NewReader(bzip2.NewReader(f))
	clean := filepath.Clean(dst)
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		fp := filepath.Join(dst, h.Name)
		if fp != clean && !strings.HasPrefix(fp, clean+string(os.PathSeparator)) {
			return fmt.Errorf("unsafe tar entry: %s", h.Name)
		}
		switch h.Typeflag {
		case tar.TypeDir:
			os.MkdirAll(fp, 0o755)
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(fp), 0o755); err != nil {
				return err
			}
			out, err := os.Create(fp)
			if err != nil {
				return err
			}
			_, err = io.Copy(out, tr)
			out.Close()
			if err != nil {
				return err
			}
		}
	}
	return nil
}

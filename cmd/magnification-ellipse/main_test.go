package main

import (
	"bytes"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMagnificationEllipseCLI(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "mag.png")

	var stdout, stderr bytes.Buffer
	stdinJSON := `{"version": 1, "rx": {"od": {"sph": -3.0, "cyl": -1.5, "axis": 90}, "os": {"sph": -2.5}}}`
	if err := run([]string{"-data", "-", "-size", "100", "-out", outPath}, strings.NewReader(stdinJSON), &stdout, &stderr); err != nil {
		t.Fatalf("run: %v", err)
	}

	f, err := os.Open(outPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	img, err := png.Decode(f)
	f.Close()
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if img.Bounds().Dx() != 300 || img.Bounds().Dy() != 300 {
		t.Errorf("dims = %v, want 300x300", img.Bounds())
	}

	stdout.Reset()
	if err := run([]string{"-version"}, nil, &stdout, &stderr); err != nil {
		t.Fatalf("run -version: %v", err)
	}
	if !strings.HasPrefix(stdout.String(), "magnification-ellipse ") {
		t.Errorf("unexpected -version output: %q", stdout.String())
	}

	if err := run(nil, nil, &stdout, &stderr); err == nil {
		t.Errorf("expected error when -data is missing")
	}
}

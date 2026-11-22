package main

import (
	"bytes"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"jsouthworth.net/go/optical-analyzer/dsm"
)

func TestPrismaticDifferenceCLI(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "pdiff.png")

	var stdout, stderr bytes.Buffer
	stdinJSON := `{"version": 1, "rx": {"od": {"sph": -4.5}, "os": {"sph": -2.0}}, "lenses": {"material": "1.60-plastic"}}`
	if err := run([]string{"-data", "-", "-out", outPath}, strings.NewReader(stdinJSON), &stdout, &stderr); err != nil {
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
	if img.Bounds().Dx() != dsm.LensImageDim || img.Bounds().Dy() != dsm.LensImageDim {
		t.Errorf("dims = %v, want %dx%d", img.Bounds(), dsm.LensImageDim, dsm.LensImageDim)
	}

	stdout.Reset()
	if err := run([]string{"-version"}, nil, &stdout, &stderr); err != nil {
		t.Fatalf("run -version: %v", err)
	}
	if !strings.HasPrefix(stdout.String(), "prismatic-difference ") {
		t.Errorf("unexpected -version output: %q", stdout.String())
	}

	if err := run(nil, nil, &stdout, &stderr); err == nil {
		t.Errorf("expected error when -data is missing")
	}
}

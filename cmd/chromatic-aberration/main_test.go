package main

import (
	"bytes"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"jsouthworth.net/go/optical-analyzer/contour"
	"jsouthworth.net/go/optical-analyzer/dsm"
)

func TestChromaticAberrationCLI(t *testing.T) {
	dir := t.TempDir()

	odC := contour.Superellipse(52, 40, contour.ShapeSquircle)
	trcPath := filepath.Join(dir, "frame.trc")
	if err := contour.WriteToPath(trcPath, "", nil, contour.Trace{
		FrameA: 52, FrameB: 40, FrameDBL: 18, OD: odC, OS: odC.Mirrored(),
	}); err != nil {
		t.Fatalf("WriteToPath: %v", err)
	}

	cfgPath := filepath.Join(dir, "rx.json")
	cfgBody := `{
		"version": 1,
		"rx": { "od": { "sph": -4.0, "cyl": -1.0, "axis": 90 }, "os": { "sph": -3.5 } },
		"lenses": { "material": "1.67-plastic" },
		"frame": { "trace": "frame.trc" },
		"fit": { "od": { "pd": 31 }, "os": { "pd": 30.5 } }
	}`
	if err := os.WriteFile(cfgPath, []byte(cfgBody), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	odOut := filepath.Join(dir, "od.png")
	osOut := filepath.Join(dir, "os.png")
	var stdout, stderr bytes.Buffer
	if err := run([]string{"-data", cfgPath, "-od", odOut, "-os", osOut, "-dim-outside"}, nil, &stdout, &stderr); err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, p := range []string{odOut, osOut} {
		f, err := os.Open(p)
		if err != nil {
			t.Fatalf("open %s: %v", p, err)
		}
		img, err := png.Decode(f)
		f.Close()
		if err != nil {
			t.Fatalf("decode %s: %v", p, err)
		}
		if img.Bounds().Dx() != dsm.LensImageDim || img.Bounds().Dy() != dsm.LensImageDim {
			t.Errorf("%s dims = %v, want %dx%d", p, img.Bounds(), dsm.LensImageDim, dsm.LensImageDim)
		}
	}

	// Stdin data works
	odStdin := filepath.Join(dir, "od_stdin.png")
	osStdin := filepath.Join(dir, "os_stdin.png")
	stdinJSON := `{"rx": {"od": {"sph": -2.0}, "os": {"sph": -2.0}}}`
	if err := run([]string{"-data", "-", "-od", odStdin, "-os", osStdin}, strings.NewReader(stdinJSON), &stdout, &stderr); err != nil {
		t.Fatalf("run stdin: %v", err)
	}

	// -version prints name and version
	stdout.Reset()
	if err := run([]string{"-version"}, nil, &stdout, &stderr); err != nil {
		t.Fatalf("run -version: %v", err)
	}
	if !strings.HasPrefix(stdout.String(), "chromatic-aberration ") {
		t.Errorf("unexpected -version output: %q", stdout.String())
	}

	// Missing -data and conflicting stdin return errors
	if err := run(nil, nil, &stdout, &stderr); err == nil {
		t.Errorf("expected error when -data is missing")
	}
	if err := run([]string{"-data", "-", "-trace", "-"}, strings.NewReader("{}"), &stdout, &stderr); err == nil {
		t.Errorf("expected error when both -data - and -trace - are set")
	}
}

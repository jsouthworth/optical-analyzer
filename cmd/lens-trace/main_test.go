package main

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"jsouthworth.net/go/optical-analyzer/tracer"
)

func makeTestFrameImage() *image.NRGBA {
	white := color.NRGBA{255, 255, 255, 255}
	dark := color.NRGBA{20, 20, 20, 255}
	img := image.NewNRGBA(image.Rect(0, 0, 400, 200))
	for y := 0; y < 200; y++ {
		for x := 0; x < 400; x++ {
			img.SetNRGBA(x, y, white)
			lx := float64(x) - 200.0
			ly := float64(y) - 100.0
			odX := lx + 70.0
			osX := lx - 70.0
			inOD := (odX/52.0)*(odX/52.0)+(ly/40.0)*(ly/40.0) <= 1.0
			inOS := (osX/52.0)*(osX/52.0)+(ly/40.0)*(ly/40.0) <= 1.0
			outOD := (odX/61.0)*(odX/61.0)+(ly/49.0)*(ly/49.0) <= 1.0
			outOS := (osX/61.0)*(osX/61.0)+(ly/49.0)*(ly/49.0) <= 1.0
			bridge := math.Abs(lx) <= 27 && ly >= -14 && ly <= -4
			if !inOD && !inOS && (outOD || outOS || bridge) {
				img.SetNRGBA(x, y, dark)
			}
		}
	}
	return img
}

func TestLensTraceCLI(t *testing.T) {
	dir := t.TempDir()
	src := makeTestFrameImage()

	pngPath := filepath.Join(dir, "frame.png")
	fPNG, _ := os.Create(pngPath)
	_ = png.Encode(fPNG, src)
	fPNG.Close()

	jpgPath := filepath.Join(dir, "frame.jpg")
	fJPG, _ := os.Create(jpgPath)
	_ = jpeg.Encode(fJPG, src, &jpeg.Options{Quality: 90})
	fJPG.Close()

	var pngBytes bytes.Buffer
	_ = png.Encode(&pngBytes, src)

	overlayPath := filepath.Join(dir, "overlay.png")
	outFilePath := filepath.Join(dir, "out.trc")

	tests := []struct {
		name    string
		args    []string
		stdin   []byte
		wantErr bool
		verify  func(t *testing.T, stdout string)
	}{
		{
			name: "default output to stdout (-) produces OMA format",
			args: []string{"-a", "52", "-groove-depth", "0", pngPath},
			verify: func(t *testing.T, stdout string) {
				for _, want := range []string{"HBOX=52.00", "TRCFMT=1;360;E;R;F", "TRCFMT=1;360;E;L;F"} {
					if !strings.Contains(stdout, want) {
						t.Errorf("stdout OMA missing %q", want)
					}
				}
			},
		},
		{
			name: "explicit -out - with -format json outputs JSON to stdout",
			args: []string{"-image", jpgPath, "-a", "52", "-dbl", "19", "-format", "json", "-out", "-"},
			verify: func(t *testing.T, stdout string) {
				var payload struct {
					Version     int       `json:"version"`
					FrameA      float64   `json:"frame_a"`
					FrameDBL    float64   `json:"frame_dbl"`
					GrooveDepth float64   `json:"groove_depth"`
					ODRadii     []float64 `json:"od_radii"`
				}
				if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
					t.Fatalf("json.Unmarshal: %v", err)
				}
				if payload.Version != 1 || payload.FrameA != 52 || payload.FrameDBL != 19 || payload.GrooveDepth != tracer.DefaultGrooveDepthMM || len(payload.ODRadii) != 360 {
					t.Errorf("unexpected JSON payload: ver=%d A=%v DBL=%v groove=%v len=%d",
						payload.Version, payload.FrameA, payload.FrameDBL, payload.GrooveDepth, len(payload.ODRadii))
				}
			},
		},
		{
			name:  "reads image from stdin (-) and writes SVG to stdout",
			args:  []string{"-image", "-", "-a", "52", "-format", "svg", "-out", "-"},
			stdin: pngBytes.Bytes(),
			verify: func(t *testing.T, stdout string) {
				if !strings.Contains(stdout, "<svg") || !strings.Contains(stdout, `id="od"`) {
					t.Errorf("expected SVG output on stdout")
				}
			},
		},
		{
			name: "writes OMA trace to file and generates debug overlay PNG",
			args: []string{"-image", pngPath, "-a", "52", "-out", outFilePath, "-debug-overlay", overlayPath, "-odpd", "31", "-odoch", "22"},
			verify: func(t *testing.T, stdout string) {
				if stdout != "" {
					t.Errorf("expected empty stdout when writing to file, got %q", stdout)
				}
				if _, err := os.Stat(outFilePath); err != nil {
					t.Errorf("trace file missing: %v", err)
				}
				if _, err := os.Stat(overlayPath); err != nil {
					t.Errorf("overlay file missing: %v", err)
				}
			},
		},
		{
			name: "-version prints version and exits cleanly",
			args: []string{"-version"},
			verify: func(t *testing.T, stdout string) {
				if !strings.HasPrefix(stdout, "lens-trace ") {
					t.Errorf("unexpected -version output: %q", stdout)
				}
			},
		},
		{name: "missing image returns error", args: []string{"-a", "52"}, wantErr: true},
		{name: "missing -a returns error", args: []string{pngPath}, wantErr: true},
		{name: "unsupported format returns error", args: []string{"-a", "52", "-format", "dxf", pngPath}, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := run(tc.args, bytes.NewReader(tc.stdin), &stdout, &stderr)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("run(%v): %v", tc.args, err)
			}
			tc.verify(t, stdout.String())
		})
	}
}

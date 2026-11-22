package contour

import (
	"bytes"
	"math"
	"path/filepath"
	"strings"
	"testing"
)

func almostEqual(a, b, tol float64) bool {
	return math.Abs(a-b) <= tol
}

func TestSuperellipseCoord(t *testing.T) {
	tests := []struct {
		name string
		t    float64
		n    float64
		want float64
	}{
		{name: "zero returns zero", t: 0, n: ShapeOval, want: 0},
		{name: "positive unit unchanged for oval", t: 1.0, n: ShapeOval, want: 1.0},
		{name: "negative unit unchanged for squircle", t: -1.0, n: ShapeSquircle, want: -1.0},
		{name: "oval is identity positive", t: 0.6, n: ShapeOval, want: 0.6},
		{name: "oval is identity negative", t: -0.6, n: ShapeOval, want: -0.6},
		{name: "squircle positive sqrt", t: 0.25, n: ShapeSquircle, want: 0.5},
		{name: "squircle negative sqrt preserves sign", t: -0.25, n: ShapeSquircle, want: -0.5},
		{name: "sharp rectangular fourth root", t: 0.0625, n: ShapeSharpRectangular, want: 0.5},
		{name: "sharp rectangular negative fourth root", t: -0.0625, n: ShapeSharpRectangular, want: -0.5},
		{name: "soft rect exponent 3", t: 0.125, n: ShapeSoftRect, want: 0.25},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SuperellipseCoord(tc.t, tc.n)
			if !almostEqual(got, tc.want, 1e-9) {
				t.Errorf("SuperellipseCoord(%v, %v) = %v, want %v", tc.t, tc.n, got, tc.want)
			}
		})
	}
}

func TestContourGeometry(t *testing.T) {
	tests := []struct {
		name  string
		A, B  float64
		n     float64
		check func(t *testing.T, c Contour)
	}{
		{
			name: "circle matches exact radius and area",
			A:    50, B: 50, n: ShapeOval,
			check: func(t *testing.T, c Contour) {
				for i, r := range c.Radii {
					if !almostEqual(r, 25.0, 1e-9) {
						t.Fatalf("Radii[%d] = %v, want 25.0", i, r)
					}
				}
				wantArea := math.Pi * 25.0 * 25.0
				if !almostEqual(c.AreaMM2(), wantArea, 1e-6) {
					t.Errorf("AreaMM2() = %v, want %v", c.AreaMM2(), wantArea)
				}
			},
		},
		{
			name: "non-circular ellipse matches exact analytical area",
			A:    54, B: 36, n: ShapeOval,
			check: func(t *testing.T, c Contour) {
				if !almostEqual(c.Radii[0], 27.0, 1e-9) {
					t.Errorf("Radii[0] = %v, want 27.0", c.Radii[0])
				}
				if !almostEqual(c.Radii[90], 18.0, 1e-9) {
					t.Errorf("Radii[90] = %v, want 18.0", c.Radii[90])
				}
				wantArea := math.Pi * 27.0 * 18.0
				if !almostEqual(c.AreaMM2(), wantArea, 1e-3) {
					t.Errorf("AreaMM2() = %v, want %v", c.AreaMM2(), wantArea)
				}
			},
		},
		{
			name: "sharp box approaches rectangular area",
			A:    50, B: 40, n: ShapeSharpRectangular,
			check: func(t *testing.T, c Contour) {
				area := c.AreaMM2()
				if area <= math.Pi*25*20 || area >= 50*40 {
					t.Errorf("sharp box area = %v, want strictly between ellipse and rectangle", area)
				}
			},
		},
		{
			name: "extreme exponent stays finite without overflow",
			A:    50, B: 40, n: 500,
			check: func(t *testing.T, c Contour) {
				for i, r := range c.Radii {
					if math.IsNaN(r) || math.IsInf(r, 0) || r <= 0 {
						t.Fatalf("Radii[%d] = %v, want finite positive", i, r)
					}
				}
			},
		},
		{
			name: "zero dimensions produce zero contour",
			A:    0, B: 40, n: ShapeOval,
			check: func(t *testing.T, c Contour) {
				if c.AreaMM2() != 0 {
					t.Errorf("AreaMM2() = %v, want 0", c.AreaMM2())
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := Superellipse(tc.A, tc.B, tc.n)
			tc.check(t, c)
		})
	}

	t.Run("Mirrored reflects horizontally across Y axis and is involutive", func(t *testing.T) {
		od := Superellipse(52, 40, ShapeOval)
		od.Radii[135] = 35.0
		od.Radii[180] = 28.0

		os := od.Mirrored()
		if os.Radii[45] != 35.0 || os.Radii[0] != 28.0 {
			t.Errorf("unexpected mirrored radii: R(45)=%v R(0)=%v", os.Radii[45], os.Radii[0])
		}
		if os.Radii[90] != od.Radii[90] || os.Radii[270] != od.Radii[270] {
			t.Errorf("vertical poles changed on mirror")
		}
		if os.Mirrored() != od {
			t.Errorf("Mirrored().Mirrored() != original")
		}
	})
}

func TestOffset(t *testing.T) {
	tests := []struct {
		name     string
		A, B, n  float64
		offsetMM float64
		wantR0   float64
		wantR90  float64
	}{
		{name: "zero offset is identity", A: 50, B: 40, n: ShapeOval, offsetMM: 0, wantR0: 25.0, wantR90: 20.0},
		{name: "circle expands uniformly by groove depth", A: 48.8, B: 48.8, n: ShapeOval, offsetMM: 0.6, wantR0: 25.0, wantR90: 25.0},
		{name: "ellipse expands both semi-axes by groove depth", A: 50.8, B: 38.8, n: ShapeOval, offsetMM: 0.6, wantR0: 26.0, wantR90: 20.0},
		{name: "squircle expands both semi-axes by groove depth", A: 50.8, B: 38.8, n: ShapeSquircle, offsetMM: 0.6, wantR0: 26.0, wantR90: 20.0},
		{name: "negative offset contracts contour inward", A: 52.0, B: 40.0, n: ShapeOval, offsetMM: -0.5, wantR0: 25.5, wantR90: 19.5},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			base := Superellipse(tc.A, tc.B, tc.n)
			got := Offset(base, tc.offsetMM)
			if !almostEqual(got.Radii[0], tc.wantR0, 0.02) {
				t.Errorf("Radii[0] = %.4f, want %.4f", got.Radii[0], tc.wantR0)
			}
			if !almostEqual(got.Radii[90], tc.wantR90, 0.02) {
				t.Errorf("Radii[90] = %.4f, want %.4f", got.Radii[90], tc.wantR90)
			}
		})
	}
}

func TestResolveFormat(t *testing.T) {
	tests := []struct {
		name    string
		flag    string
		path    string
		want    Format
		wantErr bool
	}{
		{name: "dash defaults to OMA", flag: "", path: "-", want: FormatOMA},
		{name: "empty defaults to OMA", flag: "", path: "", want: FormatOMA},
		{name: "no extension defaults to OMA", flag: "", path: "frame", want: FormatOMA},
		{name: "trc extension", flag: "", path: "frame.trc", want: FormatOMA},
		{name: "oma extension", flag: "", path: "frame.oma", want: FormatOMA},
		{name: "json extension", flag: "", path: "frame.json", want: FormatJSON},
		{name: "svg extension", flag: "", path: "frame.svg", want: FormatSVG},
		{name: "explicit flag overrides dash", flag: "json", path: "-", want: FormatJSON},
		{name: "explicit trc alias", flag: "TRC", path: "-", want: FormatOMA},
		{name: "explicit svg flag", flag: "svg", path: "out.txt", want: FormatSVG},
		{name: "unknown flag rejected", flag: "dxf", path: "-", wantErr: true},
		{name: "unknown extension rejected", flag: "", path: "frame.dxf", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveFormat(tc.flag, tc.path)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ResolveFormat(%q, %q) expected error, got %q", tc.flag, tc.path, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveFormat(%q, %q) unexpected error: %v", tc.flag, tc.path, err)
			}
			if got != tc.want {
				t.Errorf("ResolveFormat(%q, %q) = %q, want %q", tc.flag, tc.path, got, tc.want)
			}
		})
	}
}

func makeAsymmetricSampleTrace() Trace {
	od := Superellipse(52, 40, ShapeOval)
	for i := 120; i <= 150; i++ {
		od.Radii[i] += 2.5 * math.Sin(float64(i-120)/30.0*math.Pi)
	}
	return Trace{
		Name:          "Sample Cat-Eye",
		Source:        "sample.png",
		Job:           "JOB-42",
		Model:         "CAT-100",
		FrameA:        52,
		FrameB:        40,
		FrameDBL:      18,
		GrooveDepthMM: 0.6,
		OD:            od,
		OS:            od.Mirrored(),
	}
}

func TestTraceFormatRoundTrip(t *testing.T) {
	sample := makeAsymmetricSampleTrace()

	tests := []struct {
		name     string
		ext      string
		tolMM    float64
		checkRaw func(t *testing.T, raw string)
	}{
		{
			name:  "JSON round-trip preserves exact radii and metadata",
			ext:   ".json",
			tolMM: 1e-9,
			checkRaw: func(t *testing.T, raw string) {
				for _, want := range []string{`"version": 1`, `"od_radii"`, `"groove_depth"`, `"job": "JOB-42"`, `"model": "CAT-100"`} {
					if !strings.Contains(raw, want) {
						t.Errorf("JSON trace missing %s:\n%s", want, raw)
					}
				}
			},
		},
		{
			name:  "OMA .trc round-trip preserves radii within 0.01 mm quantization",
			ext:   ".trc",
			tolMM: 0.015,
			checkRaw: func(t *testing.T, raw string) {
				for _, want := range []string{"HBOX=52.00", "VBOX=40.00", "DBL=18.00", "TRCFMT=1;360;E;R;F", "TRCFMT=1;360;E;L;F"} {
					if !strings.Contains(raw, want) {
						t.Errorf("OMA trace missing %q", want)
					}
				}
			},
		},
		{
			name:  "OMA .oma extension round-trips cleanly",
			ext:   ".oma",
			tolMM: 0.015,
		},
		{
			name:  "SVG round-trip preserves contours and boxing dimensions",
			ext:   ".svg",
			tolMM: 0.15,
			checkRaw: func(t *testing.T, raw string) {
				if !strings.Contains(raw, `id="od"`) || !strings.Contains(raw, `id="os"`) {
					t.Errorf("SVG missing od/os polygon elements")
				}
			},
		},
	}

	dir := t.TempDir()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(dir, "frame"+tc.ext)
			if err := WriteToPath(path, "", nil, sample); err != nil {
				t.Fatalf("WriteToPath(%s): %v", path, err)
			}

			var rawBuf bytes.Buffer
			format, err := ResolveFormat("", path)
			if err != nil {
				t.Fatalf("ResolveFormat: %v", err)
			}
			if err := Write(&rawBuf, format, sample); err != nil {
				t.Fatalf("Write: %v", err)
			}
			if tc.checkRaw != nil {
				tc.checkRaw(t, rawBuf.String())
			}

			loaded, err := ReadFromPath(path, "", nil, 0, 0, 0)
			if err != nil {
				t.Fatalf("ReadFromPath(%s): %v", path, err)
			}

			if !almostEqual(loaded.FrameA, sample.FrameA, 0.05) ||
				!almostEqual(loaded.FrameB, sample.FrameB, 0.05) ||
				!almostEqual(loaded.FrameDBL, sample.FrameDBL, 0.05) {
				t.Errorf("Frame = (%.2f, %.2f, %.2f), want (%.2f, %.2f, %.2f)",
					loaded.FrameA, loaded.FrameB, loaded.FrameDBL,
					sample.FrameA, sample.FrameB, sample.FrameDBL)
			}

			for i := 0; i < Steps; i++ {
				if !almostEqual(loaded.OD.Radii[i], sample.OD.Radii[i], tc.tolMM) {
					t.Fatalf("OD[%d] = %.4f, want %.4f (tol %.4f)", i, loaded.OD.Radii[i], sample.OD.Radii[i], tc.tolMM)
				}
				if !almostEqual(loaded.OS.Radii[i], sample.OS.Radii[i], tc.tolMM) {
					t.Fatalf("OS[%d] = %.4f, want %.4f (tol %.4f)", i, loaded.OS.Radii[i], sample.OS.Radii[i], tc.tolMM)
				}
			}
		})
	}

	t.Run("dash convention writes default OMA to stdout and reads from stdin", func(t *testing.T) {
		var pipe bytes.Buffer
		if err := WriteToPath("-", "", &pipe, sample); err != nil {
			t.Fatalf("WriteToPath(-): %v", err)
		}
		if !strings.Contains(pipe.String(), "TRCFMT=1;360;E;R;F") {
			t.Fatalf("expected default OMA output on stdout, got:\n%s", pipe.String())
		}

		loaded, err := ReadFromPath("-", "", &pipe, 0, 0, 0)
		if err != nil {
			t.Fatalf("ReadFromPath(-): %v", err)
		}
		if loaded.Source != "stdin" {
			t.Errorf("loaded.Source = %q, want %q", loaded.Source, "stdin")
		}
		if !almostEqual(loaded.FrameA, sample.FrameA, 0.05) || !almostEqual(loaded.FrameB, sample.FrameB, 0.05) {
			t.Errorf("stdin Frame = %.2fx%.2f, want %.2fx%.2f", loaded.FrameA, loaded.FrameB, sample.FrameA, sample.FrameB)
		}
	})
}

func TestReadEdgeCases(t *testing.T) {
	tests := []struct {
		name      string
		format    Format
		data      string
		overrideA float64
		overrideB float64
		overrideD float64
		wantErr   bool
		verify    func(t *testing.T, tr *Trace)
	}{
		{
			name:   "JSON with 36 samples resamples to 360 and auto-mirrors OS",
			format: FormatJSON,
			data: `{
				"name": "36-step circle",
				"od_radii": [25,25,25,25,25,25,25,25,25,25,25,25,25,25,25,25,25,25,25,25,25,25,25,25,25,25,25,25,25,25,25,25,25,25,25,25]
			}`,
			verify: func(t *testing.T, tr *Trace) {
				if !almostEqual(tr.FrameA, 50.0, 0.05) || !almostEqual(tr.FrameB, 50.0, 0.05) {
					t.Errorf("inferred boxing = %.2fx%.2f, want ~50x50", tr.FrameA, tr.FrameB)
				}
				if !almostEqual(tr.OS.Radii[0], 25.0, 1e-6) {
					t.Errorf("auto-mirrored OS radius = %v, want 25", tr.OS.Radii[0])
				}
			},
		},
		{
			name:      "JSON with explicit overrides rescales contour",
			format:    FormatJSON,
			data:      `{"frame_a": 50, "frame_b": 50, "od_radii": [25,25,25,25,25,25,25,25]}`,
			overrideA: 60,
			overrideB: 40,
			overrideD: 19,
			verify: func(t *testing.T, tr *Trace) {
				if !almostEqual(tr.FrameA, 60, 1e-6) || !almostEqual(tr.FrameB, 40, 1e-6) || !almostEqual(tr.FrameDBL, 19, 1e-6) {
					t.Errorf("overrides not applied: A=%v B=%v DBL=%v", tr.FrameA, tr.FrameB, tr.FrameDBL)
				}
				if !almostEqual(tr.OD.Radii[0], 30.0, 0.2) || !almostEqual(tr.OD.Radii[90], 20.0, 0.2) {
					t.Errorf("rescaled radii: R(0)=%.2f R(90)=%.2f, want ~30 and ~20", tr.OD.Radii[0], tr.OD.Radii[90])
				}
			},
		},
		{
			name:   "OMA single-eye (L only) with 16 samples mirrors to OD",
			format: FormatOMA,
			data: `
				# Sample single-eye trace
				JOB=42
				HBOX=50.00
				VBOX=50.00
				DBL=17.50
				TRCFMT=1;16;E;L;F
				R=2500;2500;2500;2500;2500;2500;2500;2500
				R=2500;2500;2500;2500;2500;2500;2500;2500
			`,
			verify: func(t *testing.T, tr *Trace) {
				if !almostEqual(tr.FrameDBL, 17.5, 1e-6) {
					t.Errorf("DBL = %v, want 17.5", tr.FrameDBL)
				}
				if !almostEqual(tr.OD.Radii[90], 25.0, 1e-6) || !almostEqual(tr.OS.Radii[90], 25.0, 1e-6) {
					t.Errorf("expected 25mm radii, got OD=%v OS=%v", tr.OD.Radii[90], tr.OS.Radii[90])
				}
			},
		},
		{
			name:      "SVG single rectangular polygon scales to A and infers B",
			format:    FormatSVG,
			overrideA: 50,
			data: `<svg xmlns="http://www.w3.org/2000/svg">
				<polygon points="10,10 110,10 110,90 10,90"/>
			</svg>`,
			verify: func(t *testing.T, tr *Trace) {
				if !almostEqual(tr.FrameA, 50.0, 1e-6) || !almostEqual(tr.FrameB, 40.0, 1e-6) {
					t.Errorf("Frame = %.2fx%.2f, want 50x40", tr.FrameA, tr.FrameB)
				}
				if !almostEqual(tr.OD.AreaMM2(), 2000.0, 15.0) {
					t.Errorf("rectangle area = %.1f, want ~2000", tr.OD.AreaMM2())
				}
			},
		},
		{
			name:      "SVG cubic and quadratic Bezier path parses cleanly",
			format:    FormatSVG,
			overrideA: 52,
			data: `<svg xmlns="http://www.w3.org/2000/svg">
				<path d="M 20,0 H 80 C 95,0 100,10 100,25 V 55 Q 100,80 80,80 L 20,80 Q 0,80 0,55 v -30 c 0,-15 5,-25 20,-25 Z"/>
			</svg>`,
			verify: func(t *testing.T, tr *Trace) {
				wantB := 52.0 * (80.0 / 100.0)
				if !almostEqual(tr.FrameB, wantB, 0.5) {
					t.Errorf("FrameB = %.2f, want ~%.2f", tr.FrameB, wantB)
				}
				if !almostEqual(tr.OD.Radii[0], 26.0, 0.25) {
					t.Errorf("OD R(0) = %.2f, want ~26.0", tr.OD.Radii[0])
				}
			},
		},
		{
			name:    "SVG without -a or data-frame-a returns error",
			format:  FormatSVG,
			data:    `<svg><polygon points="0,0 100,0 100,80 0,80"/></svg>`,
			wantErr: true,
		},
		{
			name:    "SVG with no outlines returns error",
			format:  FormatSVG,
			data:    `<svg xmlns="http://www.w3.org/2000/svg"></svg>`,
			wantErr: true,
		},
		{
			name:      "SVG with unsupported arc command returns error",
			format:    FormatSVG,
			overrideA: 50,
			data:      `<svg><path d="M 0,0 A 10,10 0 0,1 20,20 Z"/></svg>`,
			wantErr:   true,
		},
		{
			name:    "JSON with too few radii returns error",
			format:  FormatJSON,
			data:    `{"od_radii": [20, 20, 20]}`,
			wantErr: true,
		},
		{
			name:    "JSON with non-positive radius returns error",
			format:  FormatJSON,
			data:    `{"od_radii": [20, 20, -5, 20, 20, 20, 20, 20]}`,
			wantErr: true,
		},
		{
			name:    "JSON with unsupported version returns error",
			format:  FormatJSON,
			data:    `{"version": 2, "od_radii": [20, 20, 20, 20, 20, 20, 20, 20]}`,
			wantErr: true,
		},
		{
			name:    "OMA with no radii returns error",
			format:  FormatOMA,
			data:    "JOB=1\nHBOX=52.00\n",
			wantErr: true,
		},
		{
			name:    "unsupported format returns error",
			format:  "dxf",
			data:    "",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tr, err := Read(strings.NewReader(tc.data), tc.format, tc.overrideA, tc.overrideB, tc.overrideD)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %+v", tr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			tc.verify(t, tr)
		})
	}
}

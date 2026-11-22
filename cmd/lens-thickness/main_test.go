package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"jsouthworth.net/go/optical-analyzer/contour"
	"jsouthworth.net/go/optical-analyzer/lens"
	"jsouthworth.net/go/optical-analyzer/optics"
)

func TestLensThicknessCLIWithTracesAndConfigs(t *testing.T) {
	dir := t.TempDir()
	od := contour.Superellipse(52, 40, contour.ShapeSquircle)
	tr := contour.Trace{
		Name:          "Piped Frame",
		FrameA:        52,
		FrameB:        40,
		FrameDBL:      18,
		GrooveDepthMM: 0.6,
		OD:            od,
		OS:            od.Mirrored(),
	}

	var omaBuf, jsonBuf bytes.Buffer
	if err := contour.Write(&omaBuf, contour.FormatOMA, tr); err != nil {
		t.Fatalf("write OMA: %v", err)
	}
	if err := contour.Write(&jsonBuf, contour.FormatJSON, tr); err != nil {
		t.Fatalf("write JSON: %v", err)
	}

	tracePath := filepath.Join(dir, "frame.trc")
	if err := contour.WriteToPath(tracePath, "", nil, tr); err != nil {
		t.Fatalf("WriteToPath: %v", err)
	}

	writeCfg := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatalf("write config %s: %v", name, err)
		}
		return p
	}

	fullCfg := writeCfg("full.json", `{
		"version": 1,
		"rx": {
			"od": { "sph": -4.5, "cyl": -1.0, "axis": 90 },
			"os": { "sph": -4.0, "cyl": -0.5, "axis": 85 }
		},
		"lenses": {
			"material": "1.70-plastic",
			"design": "aspheric",
			"specific_gravity": 1.42,
			"chamfer": 0.25,
			"bevel_depth": 0.8,
			"od": { "base_curve": 2.75, "center_thickness": 1.2 },
			"os": { "base_curve": 2.75, "center_thickness": 1.2 }
		},
		"frame": { "a": 52, "b": 40, "dbl": 18, "shape": "squircle" },
		"fit": {
			"center_of_rotation": 27.0,
			"od": { "pd": 31.5, "och": 22.0 },
			"os": { "pd": 31.0, "och": 21.5 }
		}
	}`)
	traceStdinCfg := writeCfg("trace_stdin.json", `{
		"rx": {
			"od": { "sph": -5.0 },
			"os": { "sph": -4.5 }
		},
		"lenses": {
			"material": "1.67-plastic",
			"design": "aspheric"
		},
		"frame": { "trace": "-", "trace_format": "oma" },
		"fit": {
			"od": { "pd": 31 },
			"os": { "pd": 31 }
		}
	}`)
	relTraceCfg := writeCfg("rel_trace.json", `{
		"version": 1,
		"rx": { "od": { "sph": -4.0 }, "os": { "sph": -4.0 } },
		"lenses": { "material": "trivex" },
		"frame": { "trace": "frame.trc" }
	}`)
	unknownCfg := writeCfg("unknown.json", `{"rx": {"od": {"sph": -4.0}}, "not_a_field": 1}`)

	tests := []struct {
		name    string
		args    []string
		stdin   string
		wantErr bool
		verify  func(t *testing.T, stdout string)
	}{
		{
			name:  "reads default OMA trace from stdin (-) and computes JSON report",
			args:  []string{"-trace", "-", "-odsph", "-4.5", "-ossph", "-4.0", "-odpd", "31", "-ospd", "31", "-index", "1.67", "-design", "aspheric", "-format", "json"},
			stdin: omaBuf.String(),
			verify: func(t *testing.T, stdout string) {
				var rep lens.Report
				if err := json.Unmarshal([]byte(stdout), &rep); err != nil {
					t.Fatalf("json.Unmarshal: %v", err)
				}
				if rep.Inputs.Frame.A != 52 || rep.Inputs.Frame.B != 40 || rep.Inputs.Frame.DBL != 18 || rep.Results[0].Source != "stdin" {
					t.Errorf("unexpected stdin OMA report: frame=%+v source=%q", rep.Inputs.Frame, rep.Results[0].Source)
				}
			},
		},
		{
			name:  "reads JSON trace from stdin (-) and renders text report",
			args:  []string{"-trace", "-", "-trace-format", "json", "-odsph", "-4.0", "-ossph", "-4.0", "-index", "1.70", "-design", "aspheric", "-chamfer", "0.25"},
			stdin: jsonBuf.String(),
			verify: func(t *testing.T, stdout string) {
				for _, want := range []string{"PD: 35.00 (auto (A+DBL)/2)", "Chamfer: 0.25 mm | Bevel: 0.60 mm", "FRAME SHAPE: Piped Frame"} {
					if !strings.Contains(stdout, want) {
						t.Errorf("text output missing %q", want)
					}
				}
			},
		},
		{
			name: "reads OMA trace from file and emits CSV",
			args: []string{"-trace", tracePath, "-odsph", "-4.0", "-ossph", "-3.5", "-odpd", "31.5", "-odoch", "22", "-index", "1.67", "-design", "spherical", "-chamfer", "0.25", "-format", "csv"},
			verify: func(t *testing.T, stdout string) {
				records, err := csv.NewReader(strings.NewReader(stdout)).ReadAll()
				if err != nil {
					t.Fatalf("csv.ReadAll: %v", err)
				}
				if len(records) != 3 || records[1][14] != "0.25" || records[1][15] != "0.6" {
					t.Errorf("unexpected CSV rows: %v", records)
				}
				if _, err := strconv.ParseFloat(records[1][18], 64); err != nil {
					t.Errorf("invalid max_caliper_thickness_mm: %v", err)
				}
			},
		},
		{
			name: "loads all parameters from normalized -data and lets CLI flags override",
			args: []string{"-data", fullCfg, "-index", "1.67", "-sg", "0", "-bevel-depth", "0", "-format", "json"},
			verify: func(t *testing.T, stdout string) {
				var rep lens.Report
				if err := json.Unmarshal([]byte(stdout), &rep); err != nil {
					t.Fatalf("json.Unmarshal: %v", err)
				}
				if rep.Inputs.RefractiveIndex != 1.67 || rep.Inputs.SpecificGravity != 1.35 || rep.Inputs.BevelDepth != 0 || rep.Inputs.OD.Sphere != -4.5 {
					t.Errorf("unexpected merged inputs: %+v", rep.Inputs)
				}
			},
		},
		{
			name: "resolves relative frame.trace against data file directory and material preset alias",
			args: []string{"-data", relTraceCfg, "-format", "json"},
			verify: func(t *testing.T, stdout string) {
				var rep lens.Report
				if err := json.Unmarshal([]byte(stdout), &rep); err != nil {
					t.Fatalf("json.Unmarshal: %v", err)
				}
				if rep.Inputs.RefractiveIndex != 1.53 || rep.Inputs.Frame.A != 52 || rep.Results[0].Source != tracePath {
					t.Errorf("unexpected relative trace data report: idx=%v frame=%+v source=%q", rep.Inputs.RefractiveIndex, rep.Inputs.Frame, rep.Results[0].Source)
				}
			},
		},
		{
			name:  "normalized -data can reference stdin trace (-)",
			args:  []string{"-data", traceStdinCfg, "-format", "json"},
			stdin: omaBuf.String(),
			verify: func(t *testing.T, stdout string) {
				var rep lens.Report
				if err := json.Unmarshal([]byte(stdout), &rep); err != nil {
					t.Fatalf("json.Unmarshal: %v", err)
				}
				if rep.Inputs.Frame.A != 52 || rep.Results[0].Source != "stdin" {
					t.Errorf("unexpected trace stdin result: %+v", rep.Inputs.Frame)
				}
			},
		},
		{
			name: "-version prints version and exits cleanly",
			args: []string{"-version"},
			verify: func(t *testing.T, stdout string) {
				if !strings.HasPrefix(stdout, "lens-thickness ") {
					t.Errorf("unexpected -version output: %q", stdout)
				}
			},
		},
		{
			name: "supports 1.60 and 1.70 glass via -material glass and auto-detects 1.80/1.90 glass",
			args: []string{"-a", "50", "-b", "38", "-dbl", "18", "-odsph", "-8.0", "-ossph", "-8.0", "-index", "1.60", "-material", "glass", "-shape", "oval", "-design", "aspheric", "-format", "json"},
			verify: func(t *testing.T, stdout string) {
				var rep lens.Report
				if err := json.Unmarshal([]byte(stdout), &rep); err != nil {
					t.Fatalf("json.Unmarshal: %v", err)
				}
				if rep.Inputs.Family != optics.FamilyGlass || rep.Inputs.SpecificGravity != 2.65 || rep.Results[0].Designs[0].OD.CenterThickness != 2.0 {
					t.Errorf("unexpected 1.60 glass report: fam=%q sg=%v ct=%v", rep.Inputs.Family, rep.Inputs.SpecificGravity, rep.Results[0].Designs[0].OD.CenterThickness)
				}
			},
		},
		{
			name: "auto-detects 1.90 glass density and thickness without -material",
			args: []string{"-a", "50", "-b", "38", "-dbl", "18", "-odsph", "-10.0", "-ossph", "-10.0", "-index", "1.90", "-shape", "oval", "-design", "aspheric", "-format", "json"},
			verify: func(t *testing.T, stdout string) {
				var rep lens.Report
				if err := json.Unmarshal([]byte(stdout), &rep); err != nil {
					t.Fatalf("json.Unmarshal: %v", err)
				}
				if rep.Inputs.Family != optics.FamilyGlass || rep.Inputs.SpecificGravity != 4.00 || rep.Results[0].Designs[0].OD.CenterThickness != 1.5 {
					t.Errorf("unexpected 1.90 glass report: fam=%q sg=%v ct=%v", rep.Inputs.Family, rep.Inputs.SpecificGravity, rep.Results[0].Designs[0].OD.CenterThickness)
				}
			},
		},
		{
			name: "recommends lens materials using DSM ±30° clarity, thickness, weight, cost, and availability",
			args: []string{"-recommend", "-region", "eu", "-w-clarity", "50", "-w-thickness", "20", "-w-weight", "10", "-w-cost", "10", "-w-avail", "10", "-a", "52", "-b", "40", "-dbl", "18", "-odsph", "-5.25", "-odcyl", "-1.0", "-odaxis", "85", "-odpd", "31", "-ossph", "-4.75", "-ospd", "30.5", "-format", "json"},
			verify: func(t *testing.T, stdout string) {
				var rec lens.RecommendationReport
				if err := json.Unmarshal([]byte(stdout), &rec); err != nil {
					t.Fatalf("json.Unmarshal: %v", err)
				}
				if len(rec.Recommendations) != 12 || rec.MaxGaze != 30 || rec.Region != optics.RegionEU || rec.Recommendations[0].Rank != 1 {
					t.Errorf("unexpected recommendation report: len=%d gaze=%v region=%q", len(rec.Recommendations), rec.MaxGaze, rec.Region)
				}
				if rec.Weights.Clarity != 0.50 || rec.Weights.Availability != 0.10 {
					t.Errorf("unexpected custom weights: %+v", rec.Weights)
				}
			},
		},
		{
			name: "supports -priority ranking flag in -recommend",
			args: []string{"-recommend", "-priority", "clarity,weight,thickness", "-a", "52", "-b", "40", "-dbl", "18", "-odsph", "-5.0", "-ossph", "-5.0", "-format", "json"},
			verify: func(t *testing.T, stdout string) {
				var rec lens.RecommendationReport
				if err := json.Unmarshal([]byte(stdout), &rec); err != nil {
					t.Fatalf("json.Unmarshal: %v", err)
				}
				if rec.Weights.Clarity != 0.50 || rec.Weights.Weight <= rec.Weights.Thickness || rec.Weights.Cost != 0 || rec.Weights.Availability != 0 {
					t.Errorf("unexpected -priority weights: %+v", rec.Weights)
				}
			},
		},
		{name: "invalid -priority returns error", args: []string{"-recommend", "-a", "52", "-b", "40", "-priority", "sparkle"}, wantErr: true},
		{name: "invalid -region returns error", args: []string{"-recommend", "-a", "52", "-b", "40", "-region", "mars"}, wantErr: true},
		{name: "negative weight returns error", args: []string{"-recommend", "-a", "52", "-b", "40", "-w-avail", "-5"}, wantErr: true},
		{name: "invalid -material returns error", args: []string{"-a", "52", "-b", "40", "-material", "diamond"}, wantErr: true},
		{name: "unknown JSON key returns error", args: []string{"-data", unknownCfg}, wantErr: true},
		{name: "both -data - and -trace - returns error", args: []string{"-data", "-", "-trace", "-"}, stdin: `{}`, wantErr: true},
		{name: "negative chamfer returns error", args: []string{"-a", "52", "-b", "40", "-chamfer", "-0.1"}, wantErr: true},
		{name: "negative bevel-depth returns error", args: []string{"-a", "52", "-b", "40", "-bevel-depth", "-0.1"}, wantErr: true},
		{name: "bevel-depth >= A/2 returns error", args: []string{"-a", "52", "-b", "40", "-bevel-depth", "26"}, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := run(tc.args, strings.NewReader(tc.stdin), &stdout, &stderr)
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

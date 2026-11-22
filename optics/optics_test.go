package optics

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func almostEqual(a, b, tol float64) bool {
	return math.Abs(a-b) <= tol
}

func TestFitEffectiveOCHAndCOR(t *testing.T) {
	tests := []struct {
		name   string
		fit    Fit
		frameB float64
		wantOD float64
		wantOS float64
	}{
		{"all_zero_falls_back_to_half_B", Fit{}, 40, 20, 20},
		{"per_eye_explicit", Fit{OD: EyeFit{OCH: 21.0}, OS: EyeFit{OCH: 19.5}}, 40, 21.0, 19.5},
		{"only_OD_set_OS_falls_back_to_half_B", Fit{OD: EyeFit{OCH: 22.0}}, 40, 22.0, 20.0},
		{"only_OS_set_OD_falls_back_to_half_B", Fit{OS: EyeFit{OCH: 23.0}}, 36, 18.0, 23.0},
		{"negative_values_treated_as_unset", Fit{OD: EyeFit{OCH: -1}, OS: EyeFit{OCH: -2}}, 32, 16.0, 16.0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.fit.EffectiveODOCH(tc.frameB); !almostEqual(got, tc.wantOD, 1e-9) {
				t.Errorf("EffectiveODOCH(%v) = %v, want %v", tc.frameB, got, tc.wantOD)
			}
			if got := tc.fit.EffectiveOSOCH(tc.frameB); !almostEqual(got, tc.wantOS, 1e-9) {
				t.Errorf("EffectiveOSOCH(%v) = %v, want %v", tc.frameB, got, tc.wantOS)
			}
		})
	}

	fShared := Fit{CenterOfRotation: 25.5}
	if fShared.EffectiveODCenterOfRotation() != 25.5 || fShared.EffectiveOSCenterOfRotation() != 25.5 {
		t.Errorf("shared COR mismatch: %+v", fShared)
	}
	fOverride := Fit{CenterOfRotation: 26.0, OS: EyeFit{CenterOfRotation: 28.0}}
	if fOverride.EffectiveODCenterOfRotation() != 26.0 || fOverride.EffectiveOSCenterOfRotation() != 28.0 {
		t.Errorf("per-eye COR override mismatch: %+v", fOverride)
	}
}

func TestMaterialCatalogAndEstimators(t *testing.T) {
	if got := len(StandardMaterials()); got != 12 {
		t.Fatalf("StandardMaterials() count = %d, want 12", got)
	}

	for _, alias := range []struct {
		key     string
		wantKey string
	}{
		{"1.60-plastic", "1.60-plastic"},
		{"cr39", "1.50-plastic"},
		{"trivex", "1.53-plastic"},
		{"polycarbonate", "1.59-plastic"},
		{"crown", "1.523-glass"},
		{"mr-8", "1.60-plastic"},
		{"eyry", "1.70-plastic"},
		{"lantal", "1.90-glass"},
	} {
		spec, ok := LookupMaterialByKey(alias.key)
		if !ok || spec.Key != alias.wantKey {
			t.Errorf("LookupMaterialByKey(%q) = (%+v, %v), want key %q", alias.key, spec, ok, alias.wantKey)
		}
	}
	if _, ok := LookupMaterialByKey("1.60"); ok {
		t.Errorf("ambiguous key \"1.60\" should not match")
	}

	autoSG := []struct {
		name  string
		index float64
		want  float64
	}{
		{"CR-39 nominal 1.50", 1.50, 1.32},
		{"CR-39 exact 1.498", 1.498, 1.32},
		{"Crown glass 1.523", 1.523, 2.54},
		{"Trivex 1.53", 1.53, 1.11},
		{"Polycarbonate 1.586", 1.586, 1.20},
		{"Polycarbonate 1.59", 1.59, 1.20},
		{"High index 1.60", 1.60, 1.30},
		{"High index 1.67", 1.67, 1.35},
		{"Hoya EYRY 1.695", 1.695, 1.41},
		{"Hoya EYRY 1.70", 1.70, 1.41},
		{"High index 1.74", 1.74, 1.47},
		{"High index 1.80 glass auto", 1.80, 3.60},
		{"High index 1.90 glass auto", 1.90, 4.00},
		{"Unknown fallback", 1.45, 1.32},
	}
	for _, tc := range autoSG {
		if got := EstimateSpecificGravity(tc.index, FamilyAuto); got != tc.want {
			t.Errorf("%s: EstimateSpecificGravity(%v, auto) = %v, want %v", tc.name, tc.index, got, tc.want)
		}
	}

	matTests := []struct {
		name     string
		index    float64
		family   MaterialFamily
		wantSG   float64
		wantCT   float64
		wantET   float64
		wantAbbe int
	}{
		{"CR39", 1.50, FamilyPlastic, 1.32, 2.0, 1.5, 58},
		{"CrownGlass", 1.523, FamilyGlass, 2.54, 2.2, 1.8, 59},
		{"Trivex", 1.53, FamilyPlastic, 1.11, 1.3, 1.2, 45},
		{"Poly", 1.59, FamilyPlastic, 1.20, 1.3, 1.2, 30},
		{"Plastic160", 1.60, FamilyPlastic, 1.30, 1.4, 1.3, 42},
		{"Glass160", 1.60, FamilyGlass, 2.65, 2.0, 1.6, 42},
		{"Plastic167", 1.67, FamilyPlastic, 1.35, 1.5, 1.3, 32},
		{"Hoya170Plastic", 1.70, FamilyPlastic, 1.41, 1.0, 1.2, 36},
		{"Glass170", 1.70, FamilyGlass, 3.20, 1.8, 1.5, 39},
		{"Plastic174", 1.74, FamilyPlastic, 1.47, 1.6, 1.4, 33},
		{"Glass180", 1.80, FamilyGlass, 3.60, 1.6, 1.5, 35},
		{"Glass190", 1.90, FamilyGlass, 4.00, 1.5, 1.5, 31},
	}
	for _, tc := range matTests {
		if got := EstimateSpecificGravity(tc.index, tc.family); got != tc.wantSG {
			t.Errorf("%s: SG = %v, want %v", tc.name, got, tc.wantSG)
		}
		if got := MinCenterThickness(tc.index, tc.family); got != tc.wantCT {
			t.Errorf("%s: MinCT = %v, want %v", tc.name, got, tc.wantCT)
		}
		if got := MinEdgeThickness(tc.index, tc.family); got != tc.wantET {
			t.Errorf("%s: MinET = %v, want %v", tc.name, got, tc.wantET)
		}
		if got := EstimateAbbe(tc.index, tc.family); got != tc.wantAbbe {
			t.Errorf("%s: Abbe = %v, want %v", tc.name, got, tc.wantAbbe)
		}
		if spec, ok := LookupMaterial(tc.index, tc.family); !ok || spec.Family != tc.family {
			t.Errorf("%s: LookupMaterial(%v, %q) = (%+v, %v)", tc.name, tc.index, tc.family, spec, ok)
		}
	}
}

func TestEstimateBaseCurveAndConicParams(t *testing.T) {
	tests := []struct {
		name   string
		rx     Refraction
		design LensDesign
		want   float64
	}{
		{"plano_spherical", Refraction{Sph: 0}, DesignSpherical, 6.00},
		{"plano_aspheric", Refraction{Sph: 0}, DesignAspheric, 4.75},
		{"plano_biaspheric", Refraction{Sph: 0}, DesignBiAspheric, 4.00},
		{"minus4_spherical", Refraction{Sph: -4.00}, DesignSpherical, 4.00},
		{"minus4_aspheric", Refraction{Sph: -4.00}, DesignAspheric, 3.00},
		{"minus4_biaspheric", Refraction{Sph: -4.00}, DesignBiAspheric, 2.50},
		{"high_minus_clamp", Refraction{Sph: -12.00, Cyl: -2.00, Axis: 90}, DesignBiAspheric, 0.50},
		{"plus4_spherical", Refraction{Sph: 4.00}, DesignSpherical, 10.00},
		{"plus4_aspheric", Refraction{Sph: 4.00}, DesignAspheric, 7.75},
		{"plus4_biaspheric", Refraction{Sph: 4.00}, DesignBiAspheric, 6.50},
		{"high_plus_clamp", Refraction{Sph: 8.00}, DesignSpherical, 12.00},
		{"sph_cyl_se_biasph", Refraction{Sph: -2.50, Cyl: -2.75, Axis: 95}, DesignBiAspheric, 2.50},
	}
	for _, tc := range tests {
		if got := CalcBaseCurve(tc.rx, tc.design); got != tc.want {
			t.Errorf("%s: CalcBaseCurve = %.2f, want %.2f", tc.name, got, tc.want)
		}
	}

	pf, pb := ConicParams(DesignAspheric)
	if pf != 0.25 || pb != 1.0 {
		t.Errorf("ConicParams(Aspheric) = (%v, %v), want (0.25, 1.0)", pf, pb)
	}
	pf, pb = ConicParams(DesignBiAspheric)
	if pf != 0.0 || pb != 0.25 {
		t.Errorf("ConicParams(BiAspheric) = (%v, %v), want (0.0, 0.25)", pf, pb)
	}

	const R = 100.0
	const r = 20.0
	sag, slope := CalcConicSagAndSlope(5.0, r, 1.50, 1.0)
	if !almostEqual(sag, R-math.Sqrt(R*R-r*r), 1e-9) || !almostEqual(slope, math.Asin(r/R), 1e-9) {
		t.Errorf("spherical sag/slope = (%v, %v)", sag, slope)
	}
}

func TestEstimateCenterThickness(t *testing.T) {
	base := EyeGeometry{IsOD: true, FrameA: 52, FrameB: 40, FrameDBL: 18, MonoPD: 32, OCHeight: 20, Design: DesignBiAspheric}

	pMinus := base
	pMinus.Sphere, pMinus.Index, pMinus.Family = -4.00, 1.70, FamilyPlastic
	if ct := EstimateCenterThickness(pMinus); ct != 1.0 {
		t.Errorf("Hoya 1.70 plastic minus CT = %.2f, want 1.00", ct)
	}

	pMinusGlass := pMinus
	pMinusGlass.Family = FamilyGlass
	if ct := EstimateCenterThickness(pMinusGlass); ct != 1.8 {
		t.Errorf("1.70 glass minus CT = %.2f, want 1.80", ct)
	}

	pPlus := base
	pPlus.Sphere, pPlus.Index, pPlus.Family = 4.00, 1.50, FamilyPlastic
	ctPlus := EstimateCenterThickness(pPlus)
	if ctPlus <= 3.0 {
		t.Errorf("expected +4.00 CR-39 CT > 3.0 mm, got %.2f", ctPlus)
	}

	pPlus174 := pPlus
	pPlus174.Index = 1.74
	if ct174 := EstimateCenterThickness(pPlus174); ct174 >= ctPlus {
		t.Errorf("expected 1.74 plus CT (%.2f) < CR-39 plus CT (%.2f)", ct174, ctPlus)
	}
}

func TestNormalizeValidateAndRoundTrip(t *testing.T) {
	got := NormalizeData(Data{}, NormalizeOptions{})
	want := DefaultData()
	if got.Version != want.Version || got.RX != want.RX || got.Frame != want.Frame || got.Fit != want.Fit ||
		got.Lenses.Material != want.Lenses.Material || got.Lenses.Design != want.Lenses.Design ||
		got.Lenses.OD != want.Lenses.OD || got.Lenses.OS != want.Lenses.OS ||
		got.Lenses.EffectiveBevelDepth() != want.Lenses.EffectiveBevelDepth() ||
		got.Lenses.EffectiveChamfer() != want.Lenses.EffectiveChamfer() {
		t.Fatalf("NormalizeData(empty) =\n%+v\nwant\n%+v", got, want)
	}

	// Shared catalog preset with per-eye overrides
	presetData, err := DecodeData(strings.NewReader(`{
		"version": 1,
		"rx": { "od": { "sph": -4.0 }, "os": { "sph": 4.0 } },
		"lenses": {
			"material": "trivex",
			"design": "aspheric",
			"od": { "base_curve": 3.25 }
		},
		"fit": {
			"center_of_rotation": 26.5,
			"od": { "pd": 30.5, "och": 21.0 },
			"os": { "pd": 30.0 }
		}
	}`))
	if err != nil {
		t.Fatalf("DecodeData(preset): %v", err)
	}
	normPreset := NormalizeData(presetData, NormalizeOptions{})
	if normPreset.Lenses.Material != "1.53-plastic" || normPreset.Lenses.Design != DesignAspheric ||
		normPreset.Lenses.OD.Index != 1.53 || normPreset.Lenses.OD.Abbe != 45 || normPreset.Lenses.OD.BaseCurve != 3.25 ||
		normPreset.Lenses.OS.Index != 1.53 || normPreset.Lenses.OS.SpecificGravity != 1.11 ||
		normPreset.Fit.OD.CenterOfRotation != 26.5 || normPreset.Fit.OS.CenterOfRotation != 26.5 {
		t.Fatalf("unexpected normalized preset: %+v", normPreset)
	}

	// Per-eye material inheritance and explicit 0 bevel/DBL
	zeroBevel := 0.0
	explicit := Data{
		Version: CurrentSchemaVersion,
		RX: RX{
			OD: Refraction{Sph: -2.0},
			OS: Refraction{Sph: -2.0},
		},
		Lenses: LensPair{
			Material:   "1.53-plastic",
			Design:     DesignAspheric,
			BevelDepth: &zeroBevel,
			OD:         LensParameters{CenterThickness: 1.8, BaseCurve: 4.0},
			OS:         LensParameters{CenterThickness: 1.8, BaseCurve: 4.0},
		},
		Frame: Frame{A: 50, B: 38},
		Fit: Fit{
			OD: EyeFit{PD: 30.0, OCH: 18.5, CenterOfRotation: 26.0},
			OS: EyeFit{PD: 30.0, OCH: 21.5, CenterOfRotation: 28.0},
		},
	}
	normExp := NormalizeData(explicit, NormalizeOptions{})
	if normExp.Frame.DBL != DefaultFrameDBL || normExp.Lenses.EffectiveBevelDepth() != 0 || normExp.Lenses.Design != DesignAspheric ||
		normExp.Fit.OD.CenterOfRotation != 26.0 || normExp.Fit.OS.CenterOfRotation != 28.0 ||
		normExp.Fit.OD.OCH != 18.5 || normExp.Fit.OS.OCH != 21.5 {
		t.Fatalf("explicit overrides altered: %+v", normExp)
	}

	tmp := filepath.Join(t.TempDir(), "roundtrip.json")
	if err := SaveData(tmp, normExp); err != nil {
		t.Fatalf("SaveData: %v", err)
	}
	rawJSON, err := os.ReadFile(tmp)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	for _, wantKey := range []string{`"version": 1`, `"rx"`, `"lenses"`, `"design": "aspheric"`, `"bevel_depth": 0`, `"center_thickness": 1.8`, `"center_of_rotation": 26`} {
		if !strings.Contains(string(rawJSON), wantKey) {
			t.Errorf("saved JSON missing %s:\n%s", wantKey, rawJSON)
		}
	}
	reloaded, err := LoadData(tmp)
	if err != nil {
		t.Fatalf("LoadData: %v", err)
	}
	if reloaded.RX != normExp.RX || reloaded.Frame != normExp.Frame || reloaded.Fit != normExp.Fit ||
		reloaded.Lenses.OD != normExp.Lenses.OD || reloaded.Lenses.OS != normExp.Lenses.OS ||
		reloaded.Lenses.EffectiveBevelDepth() != 0 {
		t.Fatalf("roundtrip mismatch:\ngot  %+v\nwant %+v", reloaded, normExp)
	}

	// Validation rejects invalid configs
	invalidCases := []struct {
		name string
		json string
	}{
		{"unsupported_version", `{"version": 2, "rx": {}}`},
		{"invalid_design", `{"rx": {}, "lenses": {"design": "hyperbolic"}}`},
		{"unknown_material_preset", `{"rx": {}, "lenses": {"material": "diamond"}}`},
		{"auto_with_index", `{"rx": {}, "lenses": {"material": "auto", "index": 1.60}}`},
		{"invalid_family", `{"rx": {}, "lenses": {"family": "wood"}}`},
		{"invalid_index", `{"rx": {}, "lenses": {"index": 0.9}}`},
		{"negative_bevel", `{"rx": {}, "lenses": {"bevel_depth": -0.5}}`},
		{"invalid_shape", `{"rx": {}, "frame": {"shape": "hexagon"}}`},
		{"invalid_trace_format", `{"rx": {}, "frame": {"trace_format": "bmp"}}`},
		{"axis_out_of_range", `{"rx": {"od": {"sph": -2, "cyl": -1, "axis": 190}}}`},
		{"negative_pd", `{"rx": {}, "fit": {"od": {"pd": -30}}}`},
		{"unknown_field", `{"rx": {}, "legacy_key": true}`},
	}
	for _, tc := range invalidCases {
		t.Run("invalid_"+tc.name, func(t *testing.T) {
			if _, err := DecodeData(strings.NewReader(tc.json)); err == nil {
				t.Errorf("expected DecodeData(%s) to fail", tc.json)
			}
		})
	}

	// Relative and resolved trace paths
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "rx.json")
	trcPath := filepath.Join(dir, "frames", "model.trc")
	rel := RelativeTracePath(cfgPath, trcPath)
	if rel != "frames/model.trc" {
		t.Errorf("RelativeTracePath = %q, want frames/model.trc", rel)
	}
	if resolved := ResolveTracePath(cfgPath, rel); resolved != trcPath {
		t.Errorf("ResolveTracePath = %q, want %q", resolved, trcPath)
	}
}

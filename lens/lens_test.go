package lens

import (
	"math"
	"testing"

	"jsouthworth.net/go/optical-analyzer/contour"
	"jsouthworth.net/go/optical-analyzer/optics"
)

func almostEqual(a, b, tol float64) bool {
	return math.Abs(a-b) <= tol
}

func TestHighIndexGlassEdgeReduction(t *testing.T) {
	shapes, _ := ResolveShapes("squircle")
	designs, _ := ResolveDesigns("aspheric")
	var prevEdge float64
	for i, idx := range []float64{1.60, 1.70, 1.80, 1.90} {
		rep := BuildReport(ReportInputs{
			OD:              EyePrescription{Sphere: -8.0, PD: 31},
			OS:              EyePrescription{Sphere: -8.0, PD: 31},
			RefractiveIndex: idx,
			Family:          optics.FamilyGlass,
			Frame:           FrameSpec{A: 50, B: 38, DBL: 18},
		}, shapes, designs)
		if rep.Inputs.Family != optics.FamilyGlass {
			t.Errorf("index %.2f: Family = %q, want glass", idx, rep.Inputs.Family)
		}
		edge := rep.Results[0].Designs[0].OD.MaxCaliperThickness
		if i > 0 && edge >= prevEdge {
			t.Errorf("expected glass %.2f edge (%.2f) < previous index edge (%.2f)", idx, edge, prevEdge)
		}
		prevEdge = edge
	}
}

func TestCalcEyeProfileAndBuildReport(t *testing.T) {
	t.Run("circular plano spherical lens matches exact cylinder volume and edge", func(t *testing.T) {
		p := CalcEyeProfile(optics.EyeGeometry{
			IsOD:            true,
			BaseCurve:       4.0,
			FrameA:          50,
			FrameB:          50,
			FrameDBL:        20,
			MonoPD:          35,
			Index:           1.50,
			SpecificGravity: 1.32,
			CenterThickness: 2.0,
			ShapeExp:        contour.ShapeOval,
			Design:          optics.DesignSpherical,
		})
		wantVol := 0.98 * math.Pi * 25.0 * 25.0 * 2.0
		if !almostEqual(p.Volume, wantVol, 1e-3) {
			t.Errorf("Volume = %v, want ~%v", p.Volume, wantVol)
		}
		if !almostEqual(p.MaxCaliperThickness, 2.0, 1e-6) {
			t.Errorf("MaxCaliperThickness = %v, want 2.0", p.MaxCaliperThickness)
		}
	})

	t.Run("higher index and asphericity reduce minus edge thickness", func(t *testing.T) {
		mk := func(idx float64, d optics.LensDesign) EyeProfile {
			return CalcEyeProfile(optics.EyeGeometry{
				IsOD:     true,
				Sphere:   -6.0,
				FrameA:   52,
				FrameB:   40,
				FrameDBL: 18,
				MonoPD:   31,
				Index:    idx,
				ShapeExp: contour.ShapeSquircle,
				Design:   d,
			})
		}
		p150 := mk(1.50, optics.DesignAspheric)
		p167 := mk(1.67, optics.DesignAspheric)
		p174 := mk(1.74, optics.DesignAspheric)
		if !(p174.MaxCaliperThickness < p167.MaxCaliperThickness && p167.MaxCaliperThickness < p150.MaxCaliperThickness) {
			t.Errorf("expected 1.74 (%v) < 1.67 (%v) < 1.50 (%v)", p174.MaxCaliperThickness, p167.MaxCaliperThickness, p150.MaxCaliperThickness)
		}

		sph := mk(1.67, optics.DesignSpherical)
		biasph := mk(1.67, optics.DesignBiAspheric)
		if !(biasph.MaxCaliperThickness < p167.MaxCaliperThickness && p167.MaxCaliperThickness < sph.MaxCaliperThickness) {
			t.Errorf("expected biasph (%v) < asph (%v) < sph (%v)", biasph.MaxCaliperThickness, p167.MaxCaliperThickness, sph.MaxCaliperThickness)
		}
	})

	t.Run("bevel depth and chamfer reduce rim thickness", func(t *testing.T) {
		shapes, _ := ResolveShapes("squircle")
		designs, _ := ResolveDesigns("aspheric")
		flat := BuildReport(ReportInputs{
			OD:              EyePrescription{Sphere: -6.0, PD: 31},
			OS:              EyePrescription{Sphere: -6.0, PD: 31},
			RefractiveIndex: 1.67,
			BevelDepth:      0,
			Frame:           FrameSpec{A: 52, B: 40, DBL: 18},
		}, shapes, designs)
		beveled := BuildReport(ReportInputs{
			OD:              EyePrescription{Sphere: -6.0, PD: 31},
			OS:              EyePrescription{Sphere: -6.0, PD: 31},
			RefractiveIndex: 1.67,
			BevelDepth:      optics.DefaultBevelDepthMM,
			Chamfer:         0.25,
			Frame:           FrameSpec{A: 52, B: 40, DBL: 18},
		}, shapes, designs)
		if beveled.Results[0].Designs[0].OD.MaxCaliperThickness >= flat.Results[0].Designs[0].OD.MaxCaliperThickness {
			t.Errorf("expected beveled+chamfer thinner than flat edge")
		}
	})
}

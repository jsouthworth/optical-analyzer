package dsm

import (
	"math"
	"testing"

	"jsouthworth.net/go/optical-analyzer/optics"
)

const eps = 1e-9

func almostEqual(a, b, tol float64) bool {
	return math.Abs(a-b) <= tol
}

func TestMakeDSM(t *testing.T) {
	data := optics.Data{
		RX: optics.RX{
			OD: optics.Refraction{Sph: -2.50, Cyl: -2.75, Axis: 95},
			OS: optics.Refraction{Sph: -3.50, Cyl: -1.50, Axis: 80},
		},
		Lenses: optics.LensPair{
			OD: optics.LensParameters{Index: 1.60, CenterThickness: 1.4, BaseCurve: 2.5, Abbe: 42},
			OS: optics.LensParameters{Index: 1.67, CenterThickness: 1.5, BaseCurve: 2.0, Abbe: 32},
		},
		Fit: optics.Fit{
			OD: optics.EyeFit{CenterOfRotation: 26.0},
			OS: optics.EyeFit{CenterOfRotation: 27.0},
		},
	}

	od, os := MakeDSM(data)

	if !almostEqual(od.T, 0.0014, eps) || od.N != 1.60 || od.F1 != 2.5 || !almostEqual(od.S, 0.026, eps) {
		t.Fatalf("unexpected OD lens geometry: %+v", od)
	}
	if !almostEqual(od.FvMajor, -2.50, eps) || !almostEqual(od.FvMinor, -5.25, eps) {
		t.Fatalf("unexpected OD powers: major=%v minor=%v", od.FvMajor, od.FvMinor)
	}
	if od.AxisMajor != 95 || od.AxisMinor != 5 {
		t.Fatalf("unexpected OD axes: major=%v minor=%v", od.AxisMajor, od.AxisMinor)
	}

	if !almostEqual(os.T, 0.0015, eps) || os.N != 1.67 || os.F1 != 2.0 || !almostEqual(os.S, 0.027, eps) {
		t.Fatalf("unexpected OS lens geometry: %+v", os)
	}
	if !almostEqual(os.FvMajor, -3.50, eps) || !almostEqual(os.FvMinor, -5.00, eps) {
		t.Fatalf("unexpected OS powers: major=%v minor=%v", os.FvMajor, os.FvMinor)
	}
	if os.AxisMajor != 80 || os.AxisMinor != 170 {
		t.Fatalf("unexpected OS axes: major=%v minor=%v", os.AxisMajor, os.AxisMinor)
	}
}

func TestRadians(t *testing.T) {
	tests := []struct {
		deg  float64
		want float64
	}{
		{0, 0},
		{90, math.Pi / 2},
		{180, math.Pi},
		{360, 2 * math.Pi},
		{-90, -math.Pi / 2},
	}
	for _, tc := range tests {
		if got := radians(tc.deg); !almostEqual(got, tc.want, eps) {
			t.Errorf("radians(%v) = %v, want %v", tc.deg, got, tc.want)
		}
	}
}

func TestDSMOmega(t *testing.T) {
	m := &DSM{AxisMajor: 95}

	tests := []struct {
		meridian float64
		want     float64
	}{
		{95, 90},
		{275, 90},
		{5, 0},
		{185, 0},
		{50, 45},
		{140, 45},
		{230, 45},
		{320, 45},
	}

	for _, tc := range tests {
		if got := m.ω(tc.meridian); !almostEqual(got, tc.want, eps) {
			t.Errorf("ω(%v) = %v, want %v", tc.meridian, got, tc.want)
		}
	}
}

func TestDSMMagnification(t *testing.T) {
	plano := &DSM{T: 0, N: 1.50, F1: 0, S: 0.027}
	if got := plano.Magnification(0); !almostEqual(got, 1.0, eps) {
		t.Errorf("plano thin Magnification(0) = %v, want 1.0", got)
	}

	shapeOnly := &DSM{T: 0.002, N: 1.50, F1: 6.0, S: 0.027}
	wantShape := 1.0 / (1.0 - (0.002/1.50)*6.0)
	if got := shapeOnly.Magnification(0); !almostEqual(got, wantShape, eps) {
		t.Errorf("shape-only Magnification(0) = %v, want %v", got, wantShape)
	}

	powerOnly := &DSM{T: 0, N: 1.50, F1: 0, S: 0.027}
	if got := powerOnly.Magnification(-4.0); !almostEqual(got, 1.0/(1.0+0.108), eps) || got >= 1.0 {
		t.Errorf("minus lens power-only Magnification(-4) = %v, want %v (<1)", got, 1.0/1.108)
	}
	if got := powerOnly.Magnification(+4.0); !almostEqual(got, 1.0/(1.0-0.108), eps) || got <= 1.0 {
		t.Errorf("plus lens power-only Magnification(+4) = %v, want %v (>1)", got, 1.0/0.892)
	}

	m := &DSM{T: 0.0015, N: 1.595, F1: 2.0, S: 0.027}
	wantCombined := (1.0 / (1.0 - (0.0015/1.595)*2.0)) * (1.0 / (1.0 - 0.027*(-5.25)))
	if got := m.Magnification(-5.25); !almostEqual(got, wantCombined, eps) {
		t.Errorf("combined Magnification(-5.25) = %v, want %v", got, wantCombined)
	}
}

func TestDSMRatioAndEllipseIncrement(t *testing.T) {
	sph := &DSM{T: 0.002, N: 1.50, F1: 4.0, S: 0.027, FvMajor: -3.0, FvMinor: -3.0}
	if got := sph.Ratio(); !almostEqual(got, 1.0, eps) {
		t.Errorf("spherical Ratio() = %v, want 1.0", got)
	}
	for _, w := range []float64{0, 30, 45, 60, 90} {
		if got := sph.EllipseIncrement(w); !almostEqual(got, 0, eps) {
			t.Errorf("spherical EllipseIncrement(%v) = %v, want 0", w, got)
		}
	}

	cyl := &DSM{T: 0.0015, N: 1.595, F1: 2.0, S: 0.027, FvMajor: -2.50, FvMinor: -5.25}
	wantRatio := (1.0 - 0.027*(-5.25)) / (1.0 - 0.027*(-2.50))
	if got := cyl.Ratio(); !almostEqual(got, wantRatio, eps) {
		t.Errorf("cyl Ratio() = %v, want %v", got, wantRatio)
	}
	if got := cyl.EllipseIncrement(0); !almostEqual(got, 0, eps) {
		t.Errorf("EllipseIncrement(0) = %v, want 0", got)
	}
	if got := cyl.EllipseIncrement(90); !almostEqual(got, wantRatio-1, eps) {
		t.Errorf("EllipseIncrement(90) = %v, want %v", got, wantRatio-1)
	}
	if got := cyl.EllipseIncrement(45); !almostEqual(got, 0.5*(wantRatio-1), eps) {
		t.Errorf("EllipseIncrement(45) = %v, want %v", got, 0.5*(wantRatio-1))
	}

	plusCyl := &DSM{T: 0.003, N: 1.50, F1: 6.0, S: 0.027, FvMajor: 1.0, FvMinor: 3.0}
	if got := plusCyl.Ratio(); got >= 1.0 {
		t.Errorf("plus-cyl Ratio() = %v, want < 1.0", got)
	}
}

func TestDSMImageLocation(t *testing.T) {
	m := &DSM{
		T:         0.0015,
		N:         1.595,
		F1:        2.0,
		S:         0.027,
		FvMajor:   -2.50,
		FvMinor:   -5.25,
		AxisMajor: 95,
		AxisMinor: 185,
	}

	if got := m.ImageLocation(0, 45); got != 0 {
		t.Errorf("ImageLocation(0, 45) = %v, want 0", got)
	}

	const orig = 15.0
	magMajor := m.Magnification(m.FvMajor)
	magMinor := m.Magnification(m.FvMinor)

	for _, mer := range []float64{95, 275} {
		if got := m.ImageLocation(orig, mer); !almostEqual(got, orig*magMajor, eps) {
			t.Errorf("ImageLocation(%v, %v) = %v, want %v", orig, mer, got, orig*magMajor)
		}
	}

	for _, mer := range []float64{5, 185} {
		if got := m.ImageLocation(orig, mer); !almostEqual(got, orig*magMinor, eps) {
			t.Errorf("ImageLocation(%v, %v) = %v, want %v", orig, mer, got, orig*magMinor)
		}
	}

	lo := math.Min(orig*magMinor, orig*magMajor)
	hi := math.Max(orig*magMinor, orig*magMajor)
	for deg := 0; deg < 360; deg++ {
		loc := m.ImageLocation(orig, float64(deg))
		if loc < lo-eps || loc > hi+eps {
			t.Fatalf("ImageLocation(%v, %d) = %v outside [%v, %v]", orig, deg, loc, lo, hi)
		}
		loc2 := m.ImageLocation(2*orig, float64(deg))
		if !almostEqual(loc2, 2*loc, eps) {
			t.Fatalf("ImageLocation not linear at %d°: f(2x)=%v vs 2*f(x)=%v", deg, loc2, 2*loc)
		}
	}

	sph := &DSM{T: 0.002, N: 1.50, F1: 4.0, S: 0.027, FvMajor: -4.0, FvMinor: -4.0, AxisMajor: 30}
	wantSph := orig * sph.Magnification(-4.0)
	for deg := 0; deg < 360; deg += 15 {
		if got := sph.ImageLocation(orig, float64(deg)); !almostEqual(got, wantSph, eps) {
			t.Errorf("spherical ImageLocation(%v, %d) = %v, want %v", orig, deg, got, wantSph)
		}
	}
}

func TestDSMPrism(t *testing.T) {
	minus := &DSM{T: 0.0015, N: 1.60, F1: 2.5, S: 0.027, FvMajor: -4.0, FvMinor: -4.0}
	if got := minus.Prism(0, 90); got != 0 {
		t.Errorf("Prism(0, 90) = %v, want 0", got)
	}

	plano := &DSM{T: 0, N: 1.50, F1: 0, S: 0.027, FvMajor: 0, FvMinor: 0}
	if got := plano.Prism(20, 0); !almostEqual(got, 0, eps) {
		t.Errorf("thin plano Prism(20, 0) = %v, want 0", got)
	}

	plus := &DSM{T: 0.004, N: 1.50, F1: 6.5, S: 0.027, FvMajor: +4.0, FvMinor: +4.0}
	pPlus := plus.Prism(10, 0)
	if pPlus <= 0 {
		t.Fatalf("plus lens Prism(10, 0) = %v, want > 0", pPlus)
	}
	wantPlus := math.Abs(10-plus.ImageLocation(10, 0)) * 100 / (1000 * plus.S)
	if !almostEqual(pPlus, wantPlus, eps) {
		t.Errorf("plus lens Prism(10, 0) = %v, want %v", pPlus, wantPlus)
	}

	pMinus10 := minus.Prism(10, 45)
	pMinus20 := minus.Prism(20, 45)
	if pMinus10 <= 0 || !almostEqual(pMinus20, 2*pMinus10, eps) {
		t.Errorf("minus lens Prism scaling: P(10)=%v P(20)=%v", pMinus10, pMinus20)
	}

	thinSmallS := &DSM{T: 0, N: 1.50, F1: 0, S: 1e-6, FvMajor: -4.0, FvMinor: -4.0}
	if got := thinSmallS.Prism(10, 0); !almostEqual(got, 4.0, 1e-4) {
		t.Errorf("thin-lens small-S Prism(10mm, -4D) = %v, want ~4.0 (Prentice limit)", got)
	}
}

func TestDSMDegreeDistance(t *testing.T) {
	m := &DSM{S: 0.027}

	if got := m.DegreeDistance(0); !almostEqual(got, 0, eps) {
		t.Errorf("DegreeDistance(0) = %v, want 0", got)
	}
	if got := m.DegreeDistance(45); !almostEqual(got, 27.0, eps) {
		t.Errorf("DegreeDistance(45) = %v, want 27.0", got)
	}
	if got := m.DegreeDistance(30); !almostEqual(got, 27.0/math.Sqrt(3), eps) {
		t.Errorf("DegreeDistance(30) = %v, want %v", got, 27.0/math.Sqrt(3))
	}
	want23 := math.Tan(23*math.Pi/180) * 27.0
	if got := m.DegreeDistance(23); !almostEqual(got, want23, eps) {
		t.Errorf("DegreeDistance(23) = %v, want %v", got, want23)
	}

	prev := m.DegreeDistance(0)
	for deg := 5.0; deg <= 80.0; deg += 5.0 {
		cur := m.DegreeDistance(deg)
		if cur <= prev {
			t.Fatalf("DegreeDistance not increasing at %v°: %v <= %v", deg, cur, prev)
		}
		prev = cur
	}
}

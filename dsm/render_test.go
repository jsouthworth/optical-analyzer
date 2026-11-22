package dsm

import (
	"math"
	"testing"

	"jsouthworth.net/go/optical-analyzer/contour"
	"jsouthworth.net/go/optical-analyzer/optics"
)

func TestMeridianQuadrants(t *testing.T) {
	tests := []struct {
		name string
		x, y float64
		want float64
	}{
		{"origin", 10, 10, 0},
		{"right", 20, 10, 0},
		{"top_right", 20, 0, 45},
		{"up", 10, 0, 90},
		{"top_left", 0, 0, 135},
		{"left", 0, 10, 180},
		{"bottom_left", 0, 20, 225},
		{"down", 10, 20, 270},
		{"bottom_right", 20, 20, 315},
	}
	for _, tc := range tests {
		got := Meridian(10, 10, tc.x, tc.y)
		if math.IsNaN(got) || math.Abs(got-tc.want) > 1e-6 {
			t.Errorf("%s: Meridian(10,10, %v,%v) = %v, want %v", tc.name, tc.x, tc.y, got, tc.want)
		}
	}
}

func TestRenderers(t *testing.T) {
	data := optics.DefaultData()

	odPrism, osPrism := RenderPrism(data)
	if odPrism.Bounds().Dx() != LensImageDim || osPrism.Bounds().Dy() != LensImageDim {
		t.Fatalf("unexpected prism dimensions: %v, %v", odPrism.Bounds(), osPrism.Bounds())
	}

	odCA, osCA := RenderChromaticAberration(data)
	if odCA.Bounds().Dx() != LensImageDim || osCA.Bounds().Dy() != LensImageDim {
		t.Fatalf("unexpected CA dimensions: %v, %v", odCA.Bounds(), osCA.Bounds())
	}
	// Verify dashed 5 mm radius (50 px) reference circle is drawn around the center cross on CA plots
	half := float64(LensImageDim) / 2.0
	rPx := 5.0 * PixelsPerMM
	stroked, gapped := 0, 0
	for deg := 0; deg < 360; deg++ {
		rad := float64(deg) * math.Pi / 180.0
		x := int(math.Round(half + rPx*math.Cos(rad)))
		y := int(math.Round(half + rPx*math.Sin(rad)))
		c := odCA.RGBAAt(x, y)
		if c.R == 0xff && c.G == 0xff && c.B == 0xff {
			gapped++
		} else {
			stroked++
		}
	}
	if stroked == 0 || gapped == 0 {
		t.Errorf("expected dashed 5mm circle around optical center (both stroked and gap pixels), got stroked=%d gapped=%d", stroked, gapped)
	}

	// Verify convergence inset: minus lens has less inset than plano, plus lens has more,
	// and mirror-axis symmetric Rx produces zero vertical reading prismatic difference.
	plano := optics.DefaultData()
	odPlano, _ := MakeDSM(plano)
	inPlano := odPlano.ReadingInsetMM(31.5, optics.DefaultReadingDistanceMM)
	if inPlano < 1.8 || inPlano > 2.2 {
		t.Errorf("plano reading inset = %.3f mm, want ~2.0 mm", inPlano)
	}
	minus := plano
	minus.RX.OD.Sph, minus.RX.OS.Sph = -5.0, -5.0
	odMinus, _ := MakeDSM(minus)
	inMinus := odMinus.ReadingInsetMM(31.5, optics.DefaultReadingDistanceMM)
	plus := plano
	plus.RX.OD.Sph, plus.RX.OS.Sph = +5.0, +5.0
	odPlus, _ := MakeDSM(plus)
	inPlus := odPlus.ReadingInsetMM(31.5, optics.DefaultReadingDistanceMM)
	if !(inMinus < inPlano && inPlano < inPlus) {
		t.Errorf("expected minus inset (%.2f) < plano (%.2f) < plus (%.2f)", inMinus, inPlano, inPlus)
	}
	mirrorRx := plano
	mirrorRx.RX.OD = optics.Refraction{Sph: -4.0, Cyl: -1.5, Axis: 135}
	mirrorRx.RX.OS = optics.Refraction{Sph: -4.0, Cyl: -1.5, Axis: 45}
	mirrorRx.Fit.CenterOfRotation = 27
	rp := ReadingPrismaticDifference(mirrorRx, 23, optics.DefaultReadingDistanceMM)
	if math.Abs(rp.InsetODMM-rp.InsetOSMM) > 1e-6 || math.Abs(rp.VerticalDiffPD) > 1e-6 {
		t.Errorf("mirror-symmetric Rx should have equal inset and zero vertical reading diff: inOD=%.3f inOS=%.3f diff=%.4f", rp.InsetODMM, rp.InsetOSMM, rp.VerticalDiffPD)
	}

	pdiff := RenderPrismaticDifference(data)
	if pdiff.Bounds().Dx() != LensImageDim || pdiff.Bounds().Dy() != LensImageDim {
		t.Fatalf("unexpected prismatic difference dimensions: %v", pdiff.Bounds())
	}

	mag := RenderMagnificationEllipse(data, PreviewMagEllipseBaseSize)
	wantDim := 3 * PreviewMagEllipseBaseSize
	if mag.Bounds().Dx() != wantDim || mag.Bounds().Dy() != wantDim {
		t.Fatalf("unexpected mag ellipse dimensions: %v, want %dx%d", mag.Bounds(), wantDim, wantDim)
	}

	zeroData := data
	zeroData.Lenses.Material = ""
	zeroData.Lenses.Abbe = 0
	zeroData.Lenses.OD.Abbe = 0
	zeroData.Lenses.OS.Abbe = 0
	info := InspectPoint(zeroData, 400, 400)
	if math.IsNaN(info.ODColorSpread) || math.IsInf(info.ODColorSpread, 0) {
		t.Fatalf("invalid ODColorSpread for zero constringence: %v", info.ODColorSpread)
	}
	_, _ = RenderChromaticAberration(zeroData)
}

func TestRenderChromaticAberrationWithTrace(t *testing.T) {
	raw := []byte("HBOX=35.0;35.0\nVBOX=32.0;32.0\nDBL=27.0\nTRCFMT=1;8;E;R;F\nR=1750;1700;1600;1700;1750;1700;1600;1700\n")
	tr, err := contour.ParseOMA(raw)
	if err != nil {
		t.Fatalf("contour.ParseOMA: %v", err)
	}
	data := optics.DefaultData()
	data.Frame = optics.FrameFromTrace(tr, data.Frame)
	od, osImg := RenderChromaticAberrationWithTrace(data, tr, TraceOverlayOptions{
		ShowTrace:       true,
		ShowBoundingBox: true,
		DimOutsideTrace: true,
	})
	if od.Bounds().Dx() != LensImageDim || osImg.Bounds().Dy() != LensImageDim {
		t.Fatalf("unexpected dimensions: %v, %v", od.Bounds(), osImg.Bounds())
	}
}

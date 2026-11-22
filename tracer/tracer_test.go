package tracer

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"

	"jsouthworth.net/go/optical-analyzer/contour"
)

func almostEqual(a, b, tol float64) bool {
	return math.Abs(a-b) <= tol
}

type syntheticFrameSpec struct {
	imgW, imgH    int
	lensA, lensB  float64
	dblPx         float64
	rimThickness  float64
	tiltDeg       float64
	bgColor       color.NRGBA
	frameColor    color.NRGBA
	lensColor     color.NRGBA
	shapeFn       func(nx, ny float64, isOD bool) float64
	keyholeBridge bool
	rimGapPx      int
}

func ellipseShape(nx, ny float64, _ bool) float64 {
	return nx*nx + ny*ny
}

func squircleShape(nx, ny float64, _ bool) float64 {
	return math.Pow(math.Abs(nx), 4) + math.Pow(math.Abs(ny), 4)
}

func aviatorShape(nx, ny float64, isOD bool) float64 {
	localX := nx
	if isOD {
		localX = -nx
	}
	scaleX := 1.0
	if ny > 0 && localX > 0 {
		scaleX = 1.0 + 0.18*ny
	} else if ny > 0 && localX < 0 {
		scaleX = 1.0 - 0.18*ny
	}
	adjX := localX / scaleX
	return adjX*adjX + ny*ny
}

func renderSyntheticFrame(spec syntheticFrameSpec) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, spec.imgW, spec.imgH))
	for y := 0; y < spec.imgH; y++ {
		for x := 0; x < spec.imgW; x++ {
			img.SetNRGBA(x, y, spec.bgColor)
		}
	}

	midX := float64(spec.imgW) / 2.0
	midY := float64(spec.imgH) / 2.0
	halfCenterSep := (spec.lensA + spec.dblPx) / 2.0

	tiltRad := spec.tiltDeg * math.Pi / 180.0
	cosT := math.Cos(-tiltRad)
	sinT := math.Sin(-tiltRad)

	aHalf := spec.lensA / 2.0
	bHalf := spec.lensB / 2.0
	aOuter := aHalf + spec.rimThickness
	bOuter := bHalf + spec.rimThickness

	shape := spec.shapeFn
	if shape == nil {
		shape = ellipseShape
	}

	for y := 0; y < spec.imgH; y++ {
		for x := 0; x < spec.imgW; x++ {
			dx := float64(x) - midX
			dy := float64(y) - midY
			lx := dx*cosT - dy*sinT
			ly := dx*sinT + dy*cosT

			odX := lx + halfCenterSep
			osX := lx - halfCenterSep

			inODInner := shape(odX/aHalf, ly/bHalf, true) <= 1.0
			inOSInner := shape(osX/aHalf, ly/bHalf, false) <= 1.0
			inODOuter := shape(odX/aOuter, ly/bOuter, true) <= 1.0
			inOSOuter := shape(osX/aOuter, ly/bOuter, false) <= 1.0

			inBridge := math.Abs(lx) <= spec.dblPx/2.0+spec.rimThickness &&
				ly >= -bHalf*0.35 && ly <= -bHalf*0.10

			inLowerBridge := spec.keyholeBridge &&
				math.Abs(lx) <= spec.dblPx/2.0+spec.rimThickness &&
				ly >= bHalf*0.10 && ly <= bHalf*0.30

			if inODInner || inOSInner {
				img.SetNRGBA(x, y, spec.lensColor)
			} else if inODOuter || inOSOuter || inBridge || inLowerBridge {
				img.SetNRGBA(x, y, spec.frameColor)
			}
		}
	}

	if spec.rimGapPx > 0 {
		gapCenterX := int(math.Round(midX - halfCenterSep))
		gapStartY := int(math.Round(midY + bHalf - 1))
		gapEndY := int(math.Round(midY + bOuter + 2))
		for y := gapStartY; y <= gapEndY; y++ {
			for x := gapCenterX; x < gapCenterX+spec.rimGapPx; x++ {
				if x >= 0 && x < spec.imgW && y >= 0 && y < spec.imgH {
					img.SetNRGBA(x, y, spec.bgColor)
				}
			}
		}
	}

	return img
}

func TestTraceImage(t *testing.T) {
	white := color.NRGBA{255, 255, 255, 255}
	transparent := color.NRGBA{0, 0, 0, 0}
	darkFrame := color.NRGBA{25, 25, 30, 255}
	tortoise := color.NRGBA{95, 55, 28, 255}

	tests := []struct {
		name        string
		img         image.Image
		frameA      float64
		frameB      float64
		grooveDepth float64
		wantErr     bool
		verify      func(t *testing.T, tr *TracedFrame)
	}{
		{
			name: "circular lenses on white background",
			img: renderSyntheticFrame(syntheticFrameSpec{
				imgW: 400, imgH: 200, lensA: 100, lensB: 100, dblPx: 40, rimThickness: 8,
				bgColor: white, frameColor: darkFrame, lensColor: white, shapeFn: ellipseShape,
			}),
			frameA: 50,
			verify: func(t *testing.T, tr *TracedFrame) {
				if !almostEqual(tr.InferredB, 50.0, 0.75) || !almostEqual(tr.InferredDBL, 20.0, 1.0) {
					t.Errorf("inferred B=%.2f DBL=%.2f, want ~50, ~20", tr.InferredB, tr.InferredDBL)
				}
			},
		},
		{
			name: "transparent PNG with opaque white demo lenses",
			img: renderSyntheticFrame(syntheticFrameSpec{
				imgW: 420, imgH: 200, lensA: 110, lensB: 84, dblPx: 38, rimThickness: 9,
				bgColor: transparent, frameColor: darkFrame, lensColor: white, shapeFn: squircleShape,
			}),
			frameA: 52,
			verify: func(t *testing.T, tr *TracedFrame) {
				if !almostEqual(tr.InferredB, 52.0*(84.0/110.0), 0.75) {
					t.Errorf("InferredB = %.2f", tr.InferredB)
				}
			},
		},
		{
			name: "keyhole bridge and thin rim gap recovery",
			img: renderSyntheticFrame(syntheticFrameSpec{
				imgW: 400, imgH: 200, lensA: 100, lensB: 80, dblPx: 36, rimThickness: 4,
				bgColor: white, frameColor: tortoise, lensColor: white, keyholeBridge: true, rimGapPx: 1,
			}),
			frameA: 50,
			verify: func(t *testing.T, tr *TracedFrame) {
				if !almostEqual(tr.InferredB, 40.0, 1.0) {
					t.Errorf("InferredB = %.2f, want ~40.0", tr.InferredB)
				}
			},
		},
		{
			name: "tilted aviator with default 0.6mm groove depth",
			img: renderSyntheticFrame(syntheticFrameSpec{
				imgW: 460, imgH: 260, lensA: 101.6, lensB: 77.6, dblPx: 38.4, rimThickness: 9,
				tiltDeg: 8, bgColor: white, frameColor: darkFrame, lensColor: white, shapeFn: aviatorShape,
			}),
			frameA: 52, grooveDepth: DefaultGrooveDepthMM,
			verify: func(t *testing.T, tr *TracedFrame) {
				if !almostEqual(tr.InferredB, 40.0, 1.0) || !almostEqual(tr.InferredDBL, 18.0, 1.2) {
					t.Errorf("B=%.2f DBL=%.2f, want ~40, ~18", tr.InferredB, tr.InferredDBL)
				}
				if tr.ODContour.Radii[225] <= tr.ODContour.Radii[315] {
					t.Errorf("expected aviator OD lower-temporal flare")
				}
				mirrored := tr.ODContour.Mirrored()
				for i := 0; i < contour.Steps; i += 30 {
					if !almostEqual(mirrored.Radii[i], tr.OSContour.Radii[i], 1.2) {
						t.Errorf("OD mirror vs OS mismatch at %d", i)
					}
				}
			},
		},
		{
			name:        "invalid groove depth >= A/2 returns error",
			img:         image.NewNRGBA(image.Rect(0, 0, 50, 50)),
			frameA:      50,
			grooveDepth: 25,
			wantErr:     true,
		},
		{
			name:    "missing A returns error",
			img:     image.NewNRGBA(image.Rect(0, 0, 50, 50)),
			frameA:  0,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tr, err := TraceImage(tc.img, tc.frameA, tc.frameB, tc.grooveDepth)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("TraceImage: %v", err)
			}
			tc.verify(t, tr)
		})
	}
}

func TestRenderAndWriteDebugOverlay(t *testing.T) {
	white := color.NRGBA{255, 255, 255, 255}
	dark := color.NRGBA{20, 20, 20, 255}
	src := renderSyntheticFrame(syntheticFrameSpec{
		imgW: 400, imgH: 200, lensA: 100, lensB: 80, dblPx: 36, rimThickness: 8,
		bgColor: white, frameColor: dark, lensColor: white, shapeFn: aviatorShape,
	})
	tr, err := TraceImage(src, 50, 0, DefaultGrooveDepthMM)
	if err != nil {
		t.Fatalf("TraceImage: %v", err)
	}

	overlayImg, err := RenderDebugOverlay(tr, OverlayParams{FrameA: 50, FrameB: tr.InferredB, FrameDBL: tr.InferredDBL, ODPD: 31, OSPD: 31, ODOCH: 22, OSOCH: 22})
	if err != nil {
		t.Fatalf("RenderDebugOverlay: %v", err)
	}
	if overlayImg.Bounds().Dx() != 400 || overlayImg.Bounds().Dy() != 200 {
		t.Fatalf("unexpected overlay bounds: %v", overlayImg.Bounds())
	}

	out := filepath.Join(t.TempDir(), "overlay.png")
	if err := WriteDebugOverlayFile(out, tr, OverlayParams{FrameA: 50, FrameB: tr.InferredB, FrameDBL: tr.InferredDBL, ODPD: 31, OSPD: 31, ODOCH: 22, OSOCH: 22}); err != nil {
		t.Fatalf("WriteDebugOverlayFile: %v", err)
	}
	f, err := os.Open(out)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	if _, err := png.Decode(f); err != nil {
		t.Fatalf("decode overlay PNG: %v", err)
	}
	var buf bytes.Buffer
	if err := WriteDebugOverlay(&buf, nil, OverlayParams{}); err == nil {
		t.Errorf("expected error for nil traced frame")
	}
}

func TestTraceBrowlineWithTemplesNosepadsAndRivets(t *testing.T) {
	// Synthetic browline/clubmaster frame modeled after real catalog photos:
	// - Off-white studio background
	// - Dark thick acetate upper brow + endpieces with bright silver hinge rivets
	// - Thin pale silver lower wire rim (too faint for global Otsu alone)
	// - Dark temple arms behind lenses (OD: bisecting upper-temporal corner into a split pocket;
	//   OS: poking inward as a peninsula)
	// - Nosepad arms + pads protruding inward from both nasal rims
	w, h := 600, 260
	bg := color.NRGBA{236, 233, 225, 255}
	darkBrow := color.NRGBA{28, 28, 32, 255}
	silverWire := color.NRGBA{155, 152, 146, 255}
	rivet := color.NRGBA{220, 220, 218, 255}
	templeCol := color.NRGBA{45, 45, 50, 255}
	padCol := color.NRGBA{135, 132, 128, 255}

	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, bg)
		}
	}

	midX, midY := float64(w)/2.0, float64(h)/2.0+8
	lensA, lensB := 150.0, 120.0
	dblPx := 60.0
	halfSep := (lensA + dblPx) / 2.0
	aHalf, bHalf := lensA/2.0, lensB/2.0

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			lx := float64(x) - midX
			ly := float64(y) - midY
			odX := lx + halfSep
			osX := lx - halfSep

			dOD := math.Hypot(odX/aHalf, ly/bHalf)
			dOS := math.Hypot(osX/aHalf, ly/bHalf)

			// Thin pale silver lower wire rim (3 px wide)
			inWire := (dOD >= 1.0 && dOD <= 1.05) || (dOS >= 1.0 && dOS <= 1.05)
			// Thick dark acetate upper brow (top 35%) + wide endpieces
			inBrow := ((dOD >= 1.0 && dOD <= 1.24 && ly < -bHalf*0.15) ||
				(dOS >= 1.0 && dOS <= 1.24 && ly < -bHalf*0.15))
			inEndpiece := math.Abs(lx) >= halfSep+aHalf && math.Abs(lx) <= halfSep+aHalf+34 &&
				ly >= -bHalf*0.85 && ly <= -bHalf*0.15
			inBridge := math.Abs(lx) <= dblPx/2.0+6 && ly >= -bHalf*0.65 && ly <= -bHalf*0.42

			if inBrow || inEndpiece {
				img.SetNRGBA(x, y, darkBrow)
			} else if inWire || inBridge {
				img.SetNRGBA(x, y, silverWire)
			}

			// Bright silver rivets inside the dark endpieces (both bar and round pin rivets)
			if math.Abs(math.Abs(lx)-(halfSep+aHalf+18)) <= 10 && math.Abs(ly-(-bHalf*0.52)) <= 3 {
				img.SetNRGBA(x, y, rivet)
			}
			if math.Hypot(math.Abs(lx)-(halfSep+aHalf+8), ly-(-bHalf*0.35)) <= 3.5 {
				img.SetNRGBA(x, y, rivet)
			}

			// OD temple: crosses all the way across upper-temporal corner (splits off corner pocket)
			if dOD < 1.0 && odX < -aHalf*0.25 && math.Abs((odX+aHalf*0.68)*1.1-(ly+bHalf*0.28)) < 6.5 {
				img.SetNRGBA(x, y, templeCol)
			}
			// OS temple: hangs inward from upper-temporal brow as a peninsula
			if dOS < 1.0 && osX > aHalf*0.25 && ly < bHalf*0.35 && math.Abs((osX-aHalf*0.62)*1.2+(ly+bHalf*0.20)) < 6.5 {
				img.SetNRGBA(x, y, templeCol)
			}

			// Nosepad arms + pads poking inward on lower-nasal rims
			if dOD < 1.0 && odX > aHalf*0.60 && ly > -bHalf*0.05 && ly < bHalf*0.45 &&
				math.Abs((odX-aHalf*0.82)*0.9+(ly-bHalf*0.18)) < 8.0 {
				img.SetNRGBA(x, y, padCol)
			}
			if dOS < 1.0 && osX < -aHalf*0.60 && ly > -bHalf*0.05 && ly < bHalf*0.45 &&
				math.Abs((osX+aHalf*0.82)*0.9-(ly-bHalf*0.18)) < 8.0 {
				img.SetNRGBA(x, y, padCol)
			}
		}
	}

	tr, err := TraceImage(img, 50.0, 0, DefaultGrooveDepthMM)
	if err != nil {
		t.Fatalf("TraceImage(browline with occlusions): %v", err)
	}

	// Expected B = 50 * (120/150) ≈ 40.0 mm, DBL ≈ 20.0 mm
	if !almostEqual(tr.InferredB, 40.0, 1.5) {
		t.Errorf("InferredB = %.2f mm, want ~40.0 mm", tr.InferredB)
	}
	if !almostEqual(tr.InferredDBL, 20.0, 2.0) {
		t.Errorf("InferredDBL = %.2f mm, want ~20.0 mm", tr.InferredDBL)
	}

	// Verify both OD and OS contours closely match the true 50x40 ellipse despite temples & nosepads
	ideal := contour.Superellipse(50.0, 40.0, contour.ShapeOval)
	var maxErrOD, maxErrOS float64
	for i := 0; i < contour.Steps; i++ {
		if d := math.Abs(tr.ODContour.Radii[i] - ideal.Radii[i]); d > maxErrOD {
			maxErrOD = d
		}
		if d := math.Abs(tr.OSContour.Radii[i] - ideal.Radii[i]); d > maxErrOS {
			maxErrOS = d
		}
	}
	if maxErrOD > 1.6 || maxErrOS > 1.6 {
		t.Errorf("occluded browline contour max radial error too large: OD=%.2f mm, OS=%.2f mm (want <= 1.6 mm)", maxErrOD, maxErrOS)
	}
	odMir := tr.ODContour.Mirrored()
	for i := 0; i < contour.Steps; i++ {
		if math.Abs(odMir.Radii[i]-tr.OSContour.Radii[i]) > 1e-3 {
			t.Errorf("expected symmetric OD/OS contours at %d°: OD.Mirrored=%.4f OS=%.4f", i, odMir.Radii[i], tr.OSContour.Radii[i])
			break
		}
	}
}

package dsm

import (
	"image"
	"image/color"
	"image/png"
	"math"
	"os"

	"github.com/fogleman/gg"
	"jsouthworth.net/go/optical-analyzer/contour"
	"jsouthworth.net/go/optical-analyzer/optics"
)

const (
	// LensImageDim is the width and height in pixels of the prism, chromatic
	// aberration, and prismatic difference plots (10 pixels per mm).
	LensImageDim = 700
	// PixelsPerMM is the spatial scale of the 700x700 lens plots.
	PixelsPerMM = 10.0
	// DefaultMagEllipseBaseSize is the full CLI base circle radius for magnification ellipse export (3x = 6000px).
	DefaultMagEllipseBaseSize = 2000
	// PreviewMagEllipseBaseSize is a responsive preview base circle radius (3x = 900px).
	PreviewMagEllipseBaseSize = 300
)

// SavePNG encodes an image as a PNG file.
func SavePNG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

// Meridian computes the meridian angle in degrees [0, 360) from (originX, originY)
// to (x, y) in standard image coordinates (y increasing downward).
func Meridian(originX, originY, x, y float64) float64 {
	dx := x - originX
	dy := y - originY
	if dx == 0 && dy == 0 {
		return 0
	}
	deg := math.Mod(360.0-gg.Degrees(math.Atan2(dy, dx)), 360.0)
	if deg < 0 {
		deg += 360.0
	}
	return deg
}

func pointDistance(originX, originY, x, y float64) float64 {
	return math.Hypot(y-originY, x-originX)
}

// LateralColorSpread returns chromatic aberration in prism diopters for a lens
// with the given Abbe number.
func LateralColorSpread(m DSM, orig, meridian float64, abbe int) float64 {
	if abbe <= 0 {
		return 0
	}
	return m.Prism(orig, meridian) / float64(abbe)
}

// PrismaticDifference returns the binocular prismatic difference (OD - OS) in prism diopters.
func PrismaticDifference(od, os DSM, orig, meridian float64) float64 {
	if od.S == 0 || os.S == 0 {
		return 0
	}
	if od.S == os.S {
		return ((od.ImageLocation(orig, meridian) -
			os.ImageLocation(orig, meridian)) * 100) / (od.S * 1000)
	}
	odPrism := ((od.ImageLocation(orig, meridian) - orig) * 100) / (od.S * 1000)
	osPrism := ((os.ImageLocation(orig, meridian) - orig) * 100) / (os.S * 1000)
	return odPrism - osPrism
}

// ReadingPrism holds converged near-reading gaze coordinates and vertical
// binocular prismatic imbalance.
type ReadingPrism struct {
	DropODMM       float64 // Downward gaze distance on the OD lens plane (mm)
	DropOSMM       float64 // Downward gaze distance on the OS lens plane (mm)
	InsetODMM      float64 // Nasal convergence inset on the OD lens plane (mm)
	InsetOSMM      float64 // Nasal convergence inset on the OS lens plane (mm)
	VerticalDiffPD float64 // Signed vertical binocular prismatic difference OD - OS (Δ)
}

// ReadingPrismaticDifference computes the converged near-reading gaze drop,
// nasal inset, and vertical binocular prismatic difference (OD - OS, in Δ) at
// downward gaze angle gazeDeg and near working distance readingDistMM.
func ReadingPrismaticDifference(data optics.Data, gazeDeg, readingDistMM float64) ReadingPrism {
	od, os := MakeDSM(data)
	if od.S == 0 || os.S == 0 {
		return ReadingPrism{}
	}
	res := ReadingPrism{
		DropODMM: od.DegreeDistance(gazeDeg),
		DropOSMM: os.DegreeDistance(gazeDeg),
	}
	odPD := data.Fit.OD.PD
	if odPD <= 0 {
		odPD = (data.Frame.A + data.Frame.DBL) / 2.0
	}
	osPD := data.Fit.OS.PD
	if osPD <= 0 {
		osPD = (data.Frame.A + data.Frame.DBL) / 2.0
	}
	res.InsetODMM = od.ReadingInsetMM(odPD, readingDistMM)
	res.InsetOSMM = os.ReadingInsetMM(osPD, readingDistMM)

	rOD := math.Hypot(res.InsetODMM, res.DropODMM)
	rOS := math.Hypot(res.InsetOSMM, res.DropOSMM)
	if rOD == 0 || rOS == 0 {
		return res
	}
	merOD := Meridian(0, 0, res.InsetODMM, res.DropODMM)
	merOS := Meridian(0, 0, -res.InsetOSMM, res.DropOSMM)

	odVert := ((od.ImageLocation(rOD, merOD) - rOD) * 100.0 / (od.S * 1000.0)) * (res.DropODMM / rOD)
	osVert := ((os.ImageLocation(rOS, merOS) - rOS) * 100.0 / (os.S * 1000.0)) * (res.DropOSMM / rOS)
	res.VerticalDiffPD = odVert - osVert
	return res
}

// PointInfo holds optical measurements at a pixel coordinate on a 700x700 lens plot.
type PointInfo struct {
	DXMM          float64 // Horizontal offset from the optical center in mm (+X right)
	DYMM          float64 // Vertical offset from the optical center in mm (+Y up)
	DistanceMM    float64 // Radial distance from the optical center in mm
	GazeDeg       float64 // Gaze rotation angle in degrees (using OD center of rotation)
	ODGazeDeg     float64 // OD gaze rotation angle in degrees
	OSGazeDeg     float64 // OS gaze rotation angle in degrees
	MeridianDeg   float64 // Meridian angle in degrees [0, 360)
	ODPrism       float64 // OD induced prism magnitude in prism diopters (Δ)
	OSPrism       float64 // OS induced prism magnitude in prism diopters (Δ)
	ODColorSpread float64 // OD lateral chromatic aberration in prism diopters (Δ)
	OSColorSpread float64 // OS lateral chromatic aberration in prism diopters (Δ)
	PrismaticDiff float64 // Signed binocular prismatic difference (OD - OS) in prism diopters (Δ)
}

// InspectPoint computes optical quantities at pixel (px, py) on a 700x700 plot.
func InspectPoint(data optics.Data, px, py float64) PointInfo {
	half := float64(LensImageDim) / 2.0
	dxMM := (px - half) / PixelsPerMM
	dyMM := (half - py) / PixelsPerMM // positive upward
	distMM := pointDistance(half, half, px, py) / PixelsPerMM
	meridian := Meridian(half, half, px, py)

	od, os := MakeDSM(data)

	odGazeDeg := 0.0
	if od.S > 0 {
		odGazeDeg = math.Atan(distMM/(od.S*1000.0)) * 180.0 / math.Pi
	}
	osGazeDeg := 0.0
	if os.S > 0 {
		osGazeDeg = math.Atan(distMM/(os.S*1000.0)) * 180.0 / math.Pi
	}

	return PointInfo{
		DXMM:          dxMM,
		DYMM:          dyMM,
		DistanceMM:    distMM,
		GazeDeg:       odGazeDeg,
		ODGazeDeg:     odGazeDeg,
		OSGazeDeg:     osGazeDeg,
		MeridianDeg:   meridian,
		ODPrism:       od.Prism(distMM, meridian),
		OSPrism:       os.Prism(distMM, meridian),
		ODColorSpread: LateralColorSpread(od, distMM, meridian, data.Lenses.EffectiveEye(true).Abbe),
		OSColorSpread: LateralColorSpread(os, distMM, meridian, data.Lenses.EffectiveEye(false).Abbe),
		PrismaticDiff: PrismaticDifference(od, os, distMM, meridian),
	}
}

// PrismPointColor maps a prism or chromatic aberration magnitude to the orange stepped palette.
func PrismPointColor(val float64) color.RGBA {
	val = math.Abs(val)
	switch {
	case val < 0.125:
		return color.RGBA{0xff, 0xff, 0xff, 0xff}
	case val < 0.1875:
		return color.RGBA{0xff, 0xf3, 0xe0, 0xff}
	case val < 0.25:
		return color.RGBA{0xff, 0xe0, 0xb2, 0xff}
	case val < 0.3125:
		return color.RGBA{0xff, 0xcc, 0x80, 0xff}
	case val < 0.375:
		return color.RGBA{0xff, 0xb7, 0x4d, 0xff}
	case val < 0.4375:
		return color.RGBA{0xff, 0xa7, 0x26, 0xff}
	case val < 0.5:
		return color.RGBA{0xff, 0x98, 0x00, 0xff}
	case val < 0.5625:
		return color.RGBA{0xfb, 0x8c, 0x00, 0xff}
	case val < 0.625:
		return color.RGBA{0xf5, 0x7c, 0x00, 0xff}
	default:
		return color.RGBA{0xe6, 0x51, 0x00, 0xff}
	}
}

// PrismaticDiffPointColor maps signed prismatic difference (OD - OS) to the diverging red/white/green palette.
func PrismaticDiffPointColor(prismaticDiff float64) color.RGBA {
	switch {
	case prismaticDiff < -4:
		return color.RGBA{0x0, 0x0, 0x0, 0xff}
	case prismaticDiff < -3.75:
		return color.RGBA{0x49, 0x0b, 0x0b, 0xff}
	case prismaticDiff < -3.50:
		return color.RGBA{0x5f, 0x0f, 0x0f, 0xff}
	case prismaticDiff < -3.25:
		return color.RGBA{0x75, 0x12, 0x12, 0xff}
	case prismaticDiff < -3.00:
		return color.RGBA{0x8b, 0x15, 0x15, 0xff}
	case prismaticDiff < -2.75:
		return color.RGBA{0xa1, 0x19, 0x19, 0xff}
	case prismaticDiff < -2.50:
		return color.RGBA{0xb7, 0x1c, 0x1c, 0xff}
	case prismaticDiff < -2.25:
		return color.RGBA{0xc6, 0x28, 0x28, 0xff}
	case prismaticDiff < -2.00:
		return color.RGBA{0xd3, 0x2f, 0x2f, 0xff}
	case prismaticDiff < -1.75:
		return color.RGBA{0xe5, 0x39, 0x35, 0xff}
	case prismaticDiff < -1.50:
		return color.RGBA{0xf4, 0x43, 0x36, 0xff}
	case prismaticDiff < -1.25:
		return color.RGBA{0xef, 0x53, 0x50, 0xff}
	case prismaticDiff < -1.00:
		return color.RGBA{0xe5, 0x73, 0x73, 0xff}
	case prismaticDiff < -0.75:
		return color.RGBA{0xef, 0x9a, 0x9a, 0xff}
	case prismaticDiff < -0.5:
		return color.RGBA{0xff, 0xcd, 0xd2, 0xff}
	case prismaticDiff < -0.25:
		return color.RGBA{0xff, 0xeb, 0xee, 0xff}
	case prismaticDiff < 0.25:
		return color.RGBA{0xff, 0xff, 0xff, 0xff}
	case prismaticDiff < 0.5:
		return color.RGBA{0xf1, 0xf8, 0xe9, 0xff}
	case prismaticDiff < 0.75:
		return color.RGBA{0xdc, 0xed, 0xc8, 0xff}
	case prismaticDiff < 1:
		return color.RGBA{0xc5, 0xe1, 0xa5, 0xff}
	case prismaticDiff < 1.25:
		return color.RGBA{0xae, 0xd5, 0x81, 0xff}
	case prismaticDiff < 1.50:
		return color.RGBA{0x9c, 0xcc, 0x65, 0xff}
	case prismaticDiff < 1.75:
		return color.RGBA{0x8b, 0xc3, 0x4a, 0xff}
	case prismaticDiff < 2.00:
		return color.RGBA{0x7c, 0xb3, 0x42, 0xff}
	case prismaticDiff < 2.25:
		return color.RGBA{0x68, 0x9f, 0x38, 0xff}
	case prismaticDiff < 2.50:
		return color.RGBA{0x55, 0x8b, 0x2f, 0xff}
	case prismaticDiff < 2.75:
		return color.RGBA{0x33, 0x69, 0x1e, 0xff}
	case prismaticDiff < 3.00:
		return color.RGBA{0x20, 0x49, 0x10, 0xff}
	case prismaticDiff < 3.25:
		return color.RGBA{0x15, 0x33, 0x0a, 0xff}
	case prismaticDiff < 3.50:
		return color.RGBA{0x0f, 0x26, 0x17, 0xff}
	case prismaticDiff < 3.75:
		return color.RGBA{0x09, 0x18, 0x03, 0xff}
	case prismaticDiff < 4:
		return color.RGBA{0x0f, 0x10, 0x01, 0xff}
	default:
		return color.RGBA{0x0, 0x0, 0x0, 0xff}
	}
}

// TraceOverlayOptions configures how an optional frame contour trace is drawn
// onto a lens plot.
type TraceOverlayOptions struct {
	ShowTrace       bool // Draw the frame rim contour outline
	ShowBoundingBox bool // Draw the rectangular A × B boxing frame
	DimOutsideTrace bool // Attenuate heatmap pixels outside the frame contour
}

func drawLensOverlays(dc *gg.Context, m DSM, rectX, rectY, rectW, rectH float64,
	rim *contour.Contour, opts TraceOverlayOptions, centerCircleRadiusMM, readInsetXMM float64) {
	S := float64(LensImageDim)
	half := S / 2.0
	hasRim := rim != nil && rim.AreaMM2() > 0

	if opts.ShowTrace && opts.DimOutsideTrace && hasRim {
		boxCenterX := rectX + rectW/2.0
		boxCenterY := rectY + rectH/2.0
		dc.Push()
		dc.SetFillRuleEvenOdd()
		dc.DrawRectangle(0, 0, S, S)
		dc.NewSubPath()
		for i := 0; i < contour.Steps; i++ {
			x, y := rim.PointAt(i)
			px := boxCenterX + x*PixelsPerMM
			py := boxCenterY - y*PixelsPerMM
			if i == 0 {
				dc.MoveTo(px, py)
			} else {
				dc.LineTo(px, py)
			}
		}
		dc.ClosePath()
		dc.SetRGBA(0.95, 0.96, 0.95, 0.65)
		dc.Fill()
		dc.Pop()
	}

	// Center cross
	dc.Push()
	dc.DrawLine(half, half-25, half, half+25)
	dc.DrawLine(half-25, half, half+25, half)
	dc.SetRGB255(130, 131, 123)
	dc.SetLineWidth(2)
	dc.Stroke()
	dc.Pop()

	// Optional dashed reference circle around optical center
	if centerCircleRadiusMM > 0 {
		dc.Push()
		dc.DrawCircle(half, half, centerCircleRadiusMM*PixelsPerMM)
		dc.SetDash(6, 5)
		dc.SetRGB255(130, 131, 123)
		dc.SetLineWidth(2)
		dc.Stroke()
		dc.Pop()
	}

	// Frame bounding box outline
	if opts.ShowBoundingBox {
		dc.Push()
		dc.DrawRectangle(rectX, rectY, rectW, rectH)
		dc.SetRGB255(130, 131, 123)
		dc.SetLineWidth(2)
		dc.Stroke()
		dc.Pop()
	}

	// Frame rim contour outline
	if opts.ShowTrace && hasRim {
		boxCenterX := rectX + rectW/2.0
		boxCenterY := rectY + rectH/2.0
		dc.Push()
		for i := 0; i < contour.Steps; i++ {
			x, y := rim.PointAt(i)
			px := boxCenterX + x*PixelsPerMM
			py := boxCenterY - y*PixelsPerMM
			if i == 0 {
				dc.MoveTo(px, py)
			} else {
				dc.LineTo(px, py)
			}
		}
		dc.ClosePath()
		dc.SetRGB255(0x1e, 0x3a, 0x5f)
		dc.SetLineWidth(2.5)
		dc.Stroke()
		dc.Pop()
	}

	// Reference marks: 23° reading point dot (shifted nasally by convergence inset when set), 30° and 45° gaze ticks
	dc.Push()
	twentyThree := m.DegreeDistance(23) * PixelsPerMM
	dc.DrawPoint(half+readInsetXMM*PixelsPerMM, half+twentyThree, 5)
	thirty := m.DegreeDistance(30) * PixelsPerMM
	dc.DrawLine(half+thirty, half-25, half+thirty, half+25)
	dc.DrawLine(half-25, half+thirty, half+25, half+thirty)
	dc.DrawLine(half-thirty, half-25, half-thirty, half+25)
	dc.DrawLine(half-25, half-thirty, half+25, half-thirty)
	fortyFive := m.DegreeDistance(45) * PixelsPerMM
	dc.DrawLine(half+fortyFive, half-25, half+fortyFive, half+25)
	dc.DrawLine(half-25, half+fortyFive, half+25, half+fortyFive)
	dc.DrawLine(half-fortyFive, half-25, half-fortyFive, half+25)
	dc.DrawLine(half-25, half-fortyFive, half+25, half-fortyFive)
	dc.SetRGB255(130, 131, 123)
	dc.SetLineWidth(2)
	dc.Stroke()
	dc.Pop()
}

func renderSingleLensHeatmap(m DSM, frame optics.Frame, pd, och float64, right bool,
	rim *contour.Contour, opts TraceOverlayOptions, centerCircleRadiusMM float64,
	colorFn func(distMM, meridian float64) color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, LensImageDim, LensImageDim))
	half := float64(LensImageDim) / 2.0

	for y := 0; y < LensImageDim; y++ {
		fy := float64(y)
		rowOff := y * img.Stride
		for x := 0; x < LensImageDim; x++ {
			fx := float64(x)
			distMM := pointDistance(half, half, fx, fy) / PixelsPerMM
			meridian := Meridian(half, half, fx, fy)
			c := colorFn(distMM, meridian)
			off := rowOff + x*4
			img.Pix[off] = c.R
			img.Pix[off+1] = c.G
			img.Pix[off+2] = c.B
			img.Pix[off+3] = c.A
		}
	}

	if och <= 0 {
		och = frame.B / 2.0
	}
	horizontalFOV := frame.A * PixelsPerMM
	horizontalCenter := (frame.A - (pd - (frame.DBL / 2.0))) * PixelsPerMM
	verticalFOV := frame.B * PixelsPerMM
	verticalCenter := (frame.B - och) * PixelsPerMM

	hcenter := half - horizontalCenter
	if !right {
		hcenter = half - (horizontalFOV - horizontalCenter)
	}

	readInsetXMM := 0.0
	if pd > 0 {
		inset := m.ReadingInsetMM(pd, optics.DefaultReadingDistanceMM)
		if right {
			readInsetXMM = inset // OD nasal is +X (right)
		} else {
			readInsetXMM = -inset // OS nasal is -X (left)
		}
	}

	dc := gg.NewContextForRGBA(img)
	drawLensOverlays(dc, m, hcenter, half-verticalCenter, horizontalFOV, verticalFOV, rim, opts, centerCircleRadiusMM, readInsetXMM)
	return img
}

// RenderPrism renders the OD and OS prism distribution maps (700x700 each).
func RenderPrism(data optics.Data) (odImg, osImg *image.RGBA) {
	od, os := MakeDSM(data)
	boxOnly := TraceOverlayOptions{ShowBoundingBox: true}
	odImg = renderSingleLensHeatmap(od, data.Frame, data.Fit.OD.PD, data.Fit.EffectiveODOCH(data.Frame.B), true, nil, boxOnly, 0,
		func(distMM, meridian float64) color.RGBA {
			return PrismPointColor(od.Prism(distMM, meridian))
		})
	osImg = renderSingleLensHeatmap(os, data.Frame, data.Fit.OS.PD, data.Fit.EffectiveOSOCH(data.Frame.B), false, nil, boxOnly, 0,
		func(distMM, meridian float64) color.RGBA {
			return PrismPointColor(os.Prism(distMM, meridian))
		})
	return odImg, osImg
}

// RenderChromaticAberration renders the OD and OS lateral color spread maps (700x700 each).
func RenderChromaticAberration(data optics.Data) (odImg, osImg *image.RGBA) {
	return RenderChromaticAberrationWithTrace(data, nil, TraceOverlayOptions{ShowBoundingBox: true})
}

// RenderChromaticAberrationWithTrace renders the OD and OS lateral color spread maps
// with an optional frame contour overlay and a 5 mm reference circle around the optical center.
func RenderChromaticAberrationWithTrace(data optics.Data, trace *contour.Trace, opts TraceOverlayOptions) (odImg, osImg *image.RGBA) {
	od, os := MakeDSM(data)
	odAbbe := data.Lenses.EffectiveEye(true).Abbe
	osAbbe := data.Lenses.EffectiveEye(false).Abbe
	var odRim, osRim *contour.Contour
	if trace != nil {
		odRim = &trace.OD
		osRim = &trace.OS
	}
	const centerCircleRadiusMM = 5.0
	odImg = renderSingleLensHeatmap(od, data.Frame, data.Fit.OD.PD, data.Fit.EffectiveODOCH(data.Frame.B), true, odRim, opts, centerCircleRadiusMM,
		func(distMM, meridian float64) color.RGBA {
			return PrismPointColor(LateralColorSpread(od, distMM, meridian, odAbbe))
		})
	osImg = renderSingleLensHeatmap(os, data.Frame, data.Fit.OS.PD, data.Fit.EffectiveOSOCH(data.Frame.B), false, osRim, opts, centerCircleRadiusMM,
		func(distMM, meridian float64) color.RGBA {
			return PrismPointColor(LateralColorSpread(os, distMM, meridian, osAbbe))
		})
	return odImg, osImg
}

// RenderPrismaticDifference renders the binocular prismatic difference map (700x700).
func RenderPrismaticDifference(data optics.Data) *image.RGBA {
	od, os := MakeDSM(data)
	img := image.NewRGBA(image.Rect(0, 0, LensImageDim, LensImageDim))
	half := float64(LensImageDim) / 2.0

	for y := 0; y < LensImageDim; y++ {
		fy := float64(y)
		rowOff := y * img.Stride
		for x := 0; x < LensImageDim; x++ {
			fx := float64(x)
			distMM := pointDistance(half, half, fx, fy) / PixelsPerMM
			meridian := Meridian(half, half, fx, fy)
			c := PrismaticDiffPointColor(PrismaticDifference(od, os, distMM, meridian))
			off := rowOff + x*4
			img.Pix[off] = c.R
			img.Pix[off+1] = c.G
			img.Pix[off+2] = c.B
			img.Pix[off+3] = c.A
		}
	}

	och := (data.Fit.EffectiveODOCH(data.Frame.B) + data.Fit.EffectiveOSOCH(data.Frame.B)) / 2.0
	horizontalFOV := ((data.Frame.A*2 + data.Frame.DBL) -
		(data.Fit.OS.PD + data.Fit.OD.PD)) * PixelsPerMM
	verticalFOV := data.Frame.B * PixelsPerMM
	verticalCenter := (data.Frame.B - och) * PixelsPerMM

	dc := gg.NewContextForRGBA(img)
	drawLensOverlays(dc, od, half-horizontalFOV/2.0, half-verticalCenter, horizontalFOV, verticalFOV,
		nil, TraceOverlayOptions{ShowBoundingBox: true}, 0, 0)
	return img
}

// RenderMagnificationEllipse renders the OD (red) and OS (green) magnification ellipses
// over a 3*baseImageSize square canvas. Pass DefaultMagEllipseBaseSize (2000) for
// full CLI-equivalent 6000x6000 output, or PreviewMagEllipseBaseSize (300) for fast preview.
func RenderMagnificationEllipse(data optics.Data, baseImageSize int) *image.RGBA {
	if baseImageSize <= 0 {
		baseImageSize = PreviewMagEllipseBaseSize
	}
	od, os := MakeDSM(data)

	base := float64(baseImageSize)
	scale := base / float64(DefaultMagEllipseBaseSize)
	lineWidth := math.Max(1.5, 5.0*scale)
	margin := 100.0 * scale
	dashShort := math.Max(4.0, 16.0*scale)
	dashLong := math.Max(6.0, 24.0*scale)

	odMinor := math.Abs(base * od.Magnification(od.FvMinor))
	odMajor := math.Abs(base * od.Magnification(od.FvMajor))
	osMinor := math.Abs(base * os.Magnification(os.FvMinor))
	osMajor := math.Abs(base * os.Magnification(os.FvMajor))

	imgDim := 3 * baseImageSize
	S := float64(imgDim)
	img := image.NewRGBA(image.Rect(0, 0, imgDim, imgDim))
	dc := gg.NewContextForRGBA(img)

	// Background
	dc.Push()
	dc.DrawRectangle(0, 0, S, S)
	dc.SetRGB255(0x23, 0x39, 0x5d)
	dc.Fill()
	dc.Pop()

	// 90/180 cross
	dc.Push()
	dc.DrawLine(S/2, margin, S/2, S-margin)
	dc.DrawLine(margin, S/2, S-margin, S/2)
	dc.SetRGB255(0xd0, 0xd0, 0xd0)
	dc.SetDash(dashShort, dashLong)
	dc.SetLineWidth(lineWidth)
	dc.Stroke()
	dc.Pop()

	// Reference circle
	dc.Push()
	dc.DrawCircle(S/2, S/2, base)
	dc.SetRGB255(0xd0, 0xd0, 0xd0)
	dc.SetDash(dashShort, dashLong)
	dc.SetLineWidth(lineWidth)
	dc.Stroke()
	dc.Pop()

	// OD reference circle
	dc.Push()
	dc.DrawCircle(S/2, S/2, odMinor)
	dc.SetDash(dashLong, dashShort)
	dc.SetRGB255(255, 0, 0)
	dc.SetLineWidth(lineWidth)
	dc.Stroke()
	dc.Pop()

	// OD ellipse and principal meridians
	dc.Push()
	dc.RotateAbout(gg.Radians(90-od.AxisMajor), S/2, S/2)
	dc.DrawLine(S/2, margin, S/2, S-margin)
	dc.DrawLine(margin, S/2, S-margin, S/2)
	dc.DrawEllipse(S/2, S/2, odMinor, odMajor)
	dc.SetRGB255(255, 0, 0)
	dc.SetLineWidth(lineWidth)
	dc.Stroke()
	dc.Pop()

	// OS reference circle
	dc.Push()
	dc.DrawCircle(S/2, S/2, osMinor)
	dc.SetDash(dashLong, dashShort)
	dc.SetRGB255(0, 255, 0)
	dc.SetLineWidth(lineWidth)
	dc.Stroke()
	dc.Pop()

	// OS ellipse and principal meridians
	dc.Push()
	dc.RotateAbout(gg.Radians(90-os.AxisMajor), S/2, S/2)
	dc.DrawLine(S/2, margin, S/2, S-margin)
	dc.DrawLine(margin, S/2, S-margin, S/2)
	dc.DrawEllipse(S/2, S/2, osMinor, osMajor)
	dc.SetRGB255(0, 255, 0)
	dc.SetLineWidth(lineWidth)
	dc.Stroke()
	dc.Pop()

	return img
}

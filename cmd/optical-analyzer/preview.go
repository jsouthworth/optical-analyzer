package main

import (
	"fmt"
	"math"

	qt "github.com/mappu/miqt/qt6"

	"jsouthworth.net/go/optical-analyzer/contour"
	"jsouthworth.net/go/optical-analyzer/lens"
	"jsouthworth.net/go/optical-analyzer/optics"
)

// newThicknessPreviewWidget constructs a custom QWidget that renders the front-view OD/OS
// contours and horizontal meniscus cross-sections using native anti-aliased QPainter,
// automatically adapting to the active system QPalette (light or dark theme).
func newThicknessPreviewWidget(tab *ThicknessTab) *qt.QWidget {
	w := qt.NewQWidget2()
	w.SetMinimumHeight(240)
	w.SetMaximumHeight(300)

	w.OnPaintEvent(func(super func(event *qt.QPaintEvent), event *qt.QPaintEvent) {
		p := qt.NewQPainter2(w.QPaintDevice)
		defer p.End()

		p.SetRenderHint(qt.QPainter__Antialiasing)
		p.SetRenderHint(qt.QPainter__TextAntialiasing)

		width := float64(w.Width())
		height := float64(w.Height())
		if width < 40 || height < 40 {
			return
		}

		pal := w.Palette()
		baseCol := pal.Color(qt.QPalette__Active, qt.QPalette__Base)
		textCol := pal.Color(qt.QPalette__Active, qt.QPalette__WindowText)
		dark := baseCol.Lightness() < 128

		var (
			bgCol      *qt.QColor
			borderCol  *qt.QColor
			gridCol    *qt.QColor
			grooveCol  *qt.QColor
			odFill     *qt.QColor
			odStroke   *qt.QColor
			osFill     *qt.QColor
			osStroke   *qt.QColor
			ocCol      *qt.QColor
			dividerCol *qt.QColor
			mutedText  *qt.QColor
		)
		if dark {
			bgCol = qt.NewQColor11(24, 28, 36, 255)
			borderCol = qt.NewQColor11(62, 70, 84, 255)
			gridCol = qt.NewQColor11(75, 85, 105, 130)
			grooveCol = qt.NewQColor11(145, 160, 185, 130)
			odFill = qt.NewQColor11(56, 189, 148, 52)
			odStroke = qt.NewQColor11(56, 210, 160, 240)
			osFill = qt.NewQColor11(56, 165, 240, 52)
			osStroke = qt.NewQColor11(72, 185, 255, 240)
			ocCol = qt.NewQColor11(240, 75, 75, 255)
			dividerCol = qt.NewQColor11(110, 120, 140, 115)
			mutedText = qt.NewQColor11(210, 218, 230, 220)
		} else {
			bgCol = qt.NewQColor11(246, 248, 251, 255)
			borderCol = qt.NewQColor11(206, 213, 224, 255)
			gridCol = qt.NewQColor11(175, 185, 200, 175)
			grooveCol = qt.NewQColor11(110, 125, 145, 165)
			odFill = qt.NewQColor11(22, 155, 115, 42)
			odStroke = qt.NewQColor11(16, 135, 100, 245)
			osFill = qt.NewQColor11(24, 120, 210, 42)
			osStroke = qt.NewQColor11(20, 110, 200, 245)
			ocCol = qt.NewQColor11(215, 45, 45, 255)
			dividerCol = qt.NewQColor11(185, 195, 210, 185)
			mutedText = qt.NewQColor11(textCol.Red(), textCol.Green(), textCol.Blue(), 210)
		}

		bgPath := qt.NewQPainterPath()
		bgPath.AddRoundedRect2(0.5, 0.5, width-1, height-1, 6, 6)
		p.FillPath(bgPath, qt.NewQBrush3(bgCol))
		p.SetPenWithPen(qt.NewQPen4(qt.NewQBrush3(borderCol), 1.0))
		p.DrawPath(bgPath)

		if len(tab.activeShapes) == 0 || len(tab.report.Results) == 0 {
			p.SetPen(mutedText)
			p.DrawText5(qt.NewQRectF4(0, 0, width, height), int(qt.AlignCenter), "Enter valid frame and material values to preview")
			return
		}

		si := min(tab.selectedShapeIdx, len(tab.activeShapes)-1)
		di := min(tab.selectedDesignIdx, len(tab.activeDesigns)-1)
		if si < 0 || di < 0 || si >= len(tab.report.Results) || di >= len(tab.report.Results[si].Designs) {
			return
		}

		in := tab.report.Inputs
		shapeOpt := tab.activeShapes[si]
		design := tab.activeDesigns[di]
		designRes := tab.report.Results[si].Designs[di]

		odGroove, osGroove := lens.ResolveContours(shapeOpt, in.Frame.A, in.Frame.B)
		odShoulder, osShoulder := odGroove, osGroove
		if in.BevelDepth > 0 {
			odShoulder = contour.Offset(odGroove, -in.BevelDepth)
			osShoulder = contour.Offset(osGroove, -in.BevelDepth)
		}

		captionFont := qt.NewQFont5(w.Font())
		if pt := captionFont.PointSizeF(); pt > 2 {
			captionFont.SetPointSizeF(math.Max(8.5, pt-1.0))
		}
		p.SetFont(captionFont)

		splitX := width * 0.56
		p.SetPenWithPen(qt.NewQPen4(qt.NewQBrush3(dividerCol), 1.0))
		p.DrawLine4(qt.NewQPointF3(splitX, 10), qt.NewQPointF3(splitX, height-10))

		// --- Left panel: Front-view OD & OS contours ---
		frontPadX := 16.0
		frontTop := 28.0
		frontBot := 24.0
		frontW := splitX - 2*frontPadX - 4
		frontH := height - frontTop - frontBot

		p.SetPen(mutedText)
		p.DrawText5(
			qt.NewQRectF4(frontPadX, 6, splitX-frontPadX-8, 20),
			int(qt.AlignLeft|qt.AlignVCenter),
			fmt.Sprintf("Front View (A=%.1f  B=%.1f  DBL=%.1f mm)", in.Frame.A, in.Frame.B, in.Frame.DBL),
		)

		totalFrameW := 2*in.Frame.A + in.Frame.DBL
		if totalFrameW > 0 && in.Frame.B > 0 && frontW > 20 && frontH > 20 {
			scale := math.Min(frontW/totalFrameW, frontH/in.Frame.B)
			centerX := frontPadX + frontW/2
			centerY := frontTop + frontH/2
			halfSep := ((in.Frame.A + in.Frame.DBL) / 2.0) * scale

			odCX := centerX - halfSep
			osCX := centerX + halfSep

			drawLensFront := func(cx float64, grooveC, shoulderC contour.Contour, fillC, strokeC *qt.QColor, isOD bool, tag string) {
				halfW := (in.Frame.A / 2.0) * scale
				halfH := (in.Frame.B / 2.0) * scale

				p.SetPenWithPen(qt.NewQPen4(qt.NewQBrush3(gridCol), 1.0))
				p.SetBrushWithStyle(qt.NoBrush)
				p.DrawRect(qt.NewQRectF4(cx-halfW, centerY-halfH, 2*halfW, 2*halfH))

				if in.BevelDepth > 0 {
					gp := contourToPainterPath(grooveC, cx, centerY, scale)
					p.SetPenWithPen(qt.NewQPen4(qt.NewQBrush3(grooveCol), 1.0))
					p.DrawPath(gp)
				}

				sp := contourToPainterPath(shoulderC, cx, centerY, scale)
				p.FillPath(sp, qt.NewQBrush3(fillC))
				p.SetPenWithPen(qt.NewQPen4(qt.NewQBrush3(strokeC), 2.0))
				p.DrawPath(sp)

				frameCenter := (in.Frame.A + in.Frame.DBL) / 2.0
				var ocX, ocY float64
				if isOD {
					ocX = frameCenter - in.OD.PD
					ocY = in.OD.OCHeight - in.Frame.B/2.0
				} else {
					ocX = -(frameCenter - in.OS.PD)
					ocY = in.OS.OCHeight - in.Frame.B/2.0
				}
				px := cx + ocX*scale
				py := centerY - ocY*scale
				p.SetPenWithPen(qt.NewQPen4(qt.NewQBrush3(ocCol), 1.5))
				p.DrawLine4(qt.NewQPointF3(px-6, py), qt.NewQPointF3(px+6, py))
				p.DrawLine4(qt.NewQPointF3(px, py-6), qt.NewQPointF3(px, py+6))
				p.SetBrushWithStyle(qt.NoBrush)
				p.DrawEllipse3(qt.NewQPointF3(px, py), 3.0, 3.0)

				p.SetPen(mutedText)
				p.DrawText5(qt.NewQRectF4(cx-24, height-22, 48, 18), int(qt.AlignCenter), tag)
			}

			drawLensFront(odCX, odGroove, odShoulder, odFill, odStroke, true, "OD")
			drawLensFront(osCX, osGroove, osShoulder, osFill, osStroke, false, "OS")

			bridgeY := centerY - (in.Frame.B*0.18)*scale
			p.SetPenWithPen(qt.NewQPen4(qt.NewQBrush3(grooveCol), 1.5))
			p.DrawLine4(
				qt.NewQPointF3(odCX+(in.Frame.A/2.0)*scale, bridgeY),
				qt.NewQPointF3(osCX-(in.Frame.A/2.0)*scale, bridgeY),
			)
		}

		// --- Right panel: Horizontal cross-sections for OD & OS ---
		rightX := splitX + 14
		rightW := width - rightX - 12
		if rightW < 60 {
			return
		}

		p.SetPen(mutedText)
		p.DrawText5(
			qt.NewQRectF4(rightX, 6, rightW, 20),
			int(qt.AlignLeft|qt.AlignVCenter),
			fmt.Sprintf("Horizontal Cross-Section (%s)", design.String()),
		)

		subH := (height - 34) / 2
		drawCrossSectionQt(p, rightX, 28, rightW, subH-4, true, in, odShoulder, design, designRes.OD, odFill, odStroke, ocCol, mutedText)
		drawCrossSectionQt(p, rightX, 28+subH+2, rightW, subH-4, false, in, osShoulder, design, designRes.OS, osFill, osStroke, ocCol, mutedText)
	})

	return w
}

func contourToPainterPath(c contour.Contour, cx, cy, scale float64) *qt.QPainterPath {
	path := qt.NewQPainterPath()
	for i := 0; i < contour.Steps; i++ {
		x, y := c.PointAt(i)
		px := cx + x*scale
		py := cy - y*scale
		if i == 0 {
			path.MoveTo2(px, py)
		} else {
			path.LineTo2(px, py)
		}
	}
	path.CloseSubpath()
	return path
}

func drawCrossSectionQt(
	p *qt.QPainter,
	x0, y0, w, h float64,
	isOD bool,
	in lens.ReportInputs,
	c contour.Contour,
	design optics.LensDesign,
	prof lens.EyeProfile,
	fillC, strokeC, ocCol, textCol *qt.QColor,
) {
	if w < 40 || h < 28 {
		return
	}

	rx := in.OS
	label := "OS"
	if isOD {
		rx = in.OD
		label = "OD"
	}

	rIndex := rx.RefractiveIndex
	if rIndex <= 1.0 {
		rIndex = in.RefractiveIndex
	}
	if rIndex <= 1.0 {
		rIndex = optics.DefaultRefractiveIndex
	}

	xMin := -c.Radii[contour.Steps/2]
	xMax := c.Radii[0]
	spanX := xMax - xMin
	if spanX <= 1 {
		return
	}

	frameCenter := (in.Frame.A + in.Frame.DBL) / 2.0
	ocX := frameCenter - rx.PD
	if !isOD {
		ocX = -ocX
	}

	pFront, pBack := optics.ConicParams(design)
	f1 := prof.BaseCurve * (rIndex - 1.0) / 0.530
	alphaRad := rx.Axis * (math.Pi / 180.0)
	sinA := math.Sin(alphaRad)
	pHoriz := rx.Sphere + rx.Cyl*(sinA*sinA)
	f2 := pHoriz - f1

	const samples = 64
	xs := make([]float64, samples+1)
	frontZ := make([]float64, samples+1)
	backZ := make([]float64, samples+1)

	minZ, maxZ := math.Inf(1), math.Inf(-1)
	for i := 0; i <= samples; i++ {
		t := float64(i) / float64(samples)
		x := xMin + t*spanX
		r := math.Abs(x - ocX)
		sag1, _ := optics.CalcConicSagAndSlope(f1, r, rIndex, pFront)
		sag2, _ := optics.CalcConicSagAndSlope(f2, r, rIndex, pBack)
		zf := math.Copysign(sag1, f1)
		zb := prof.CenterThickness + math.Copysign(sag2, -f2)
		if zb-zf < 0.5 {
			zb = zf + 0.5
		}
		xs[i] = x
		frontZ[i] = zf
		backZ[i] = zb
		if zf < minZ {
			minZ = zf
		}
		if zb > maxZ {
			maxZ = zb
		}
	}

	zSpan := maxZ - minZ
	if zSpan < 4.0 {
		zSpan = 4.0
	}

	padTop := 18.0
	padBot := 8.0
	plotH := h - padTop - padBot
	if plotH < 12 {
		return
	}

	scaleX := (w - 8) / spanX
	scaleZ := math.Min(scaleX*1.6, plotH/zSpan)

	baseX := x0 + 4
	baseY := y0 + padTop + (plotH-(maxZ-minZ)*scaleZ)/2

	toPt := func(x, z float64) (float64, float64) {
		return baseX + (x-xMin)*scaleX, baseY + (z-minZ)*scaleZ
	}

	path := qt.NewQPainterPath()
	for i := 0; i <= samples; i++ {
		px, py := toPt(xs[i], frontZ[i])
		if i == 0 {
			path.MoveTo2(px, py)
		} else {
			path.LineTo2(px, py)
		}
	}
	for i := samples; i >= 0; i-- {
		px, py := toPt(xs[i], backZ[i])
		path.LineTo2(px, py)
	}
	path.CloseSubpath()

	p.FillPath(path, qt.NewQBrush3(fillC))
	p.SetPenWithPen(qt.NewQPen4(qt.NewQBrush3(strokeC), 1.6))
	p.DrawPath(path)

	if ocX >= xMin && ocX <= xMax {
		ocPx, ocTop := toPt(ocX, 0)
		_, ocBot := toPt(ocX, prof.CenterThickness)
		p.SetPenWithPen(qt.NewQPen4(qt.NewQBrush3(ocCol), 1.3))
		p.DrawLine4(qt.NewQPointF3(ocPx, ocTop-4), qt.NewQPointF3(ocPx, ocBot+4))
	}

	p.SetPen(textCol)
	header := fmt.Sprintf("%s  Temp: %.2f mm  |  CT: %.2f mm  |  Nasal: %.2f mm",
		label, prof.TempHorizontal, prof.CenterThickness, prof.NasalHorizontal)
	p.DrawText5(qt.NewQRectF4(x0, y0, w, 16), int(qt.AlignLeft|qt.AlignVCenter), header)
}

package main

import (
	"fmt"
	"math"

	qt "github.com/mappu/miqt/qt6"

	"jsouthworth.net/go/optical-analyzer/dsm"
	"jsouthworth.net/go/optical-analyzer/lens"
	"jsouthworth.net/go/optical-analyzer/optics"
)

// SummaryPanel combines the DSM optical summary (BC, CT, shape ratio, magnification,
// aniseikonia, 23° reading drop) and the lens thickness/weight summary (PD, OC, edge
// thicknesses, per-eye and pair weight) into a single compact panel above the main tabs.
type SummaryPanel struct {
	Widget *qt.QGroupBox

	headLabel *qt.QLabel
	odPDOC    *qt.QLabel
	odBCCT    *qt.QLabel
	odMag     *qt.QLabel
	odEdge    *qt.QLabel
	odWeight  *qt.QLabel

	osPDOC   *qt.QLabel
	osBCCT   *qt.QLabel
	osMag    *qt.QLabel
	osEdge   *qt.QLabel
	osWeight *qt.QLabel
}

func NewSummaryPanel() *SummaryPanel {
	sp := &SummaryPanel{}

	sp.Widget = qt.NewQGroupBox3("Summary")
	sp.Widget.SetSizePolicy2(qt.QSizePolicy__Expanding, qt.QSizePolicy__Fixed)
	vbox := qt.NewQVBoxLayout(sp.Widget.QWidget)
	vbox.SetContentsMargins(10, 4, 10, 6)
	vbox.SetSpacing(4)

	sp.headLabel = qt.NewQLabel2()
	headFont := qt.NewQFont5(sp.headLabel.Font())
	headFont.SetBold(true)
	sp.headLabel.SetFont(headFont)
	sp.headLabel.SetWordWrap(true)
	vbox.AddWidget(sp.headLabel.QWidget)

	grid := qt.NewQGridLayout2()
	grid.SetContentsMargins(0, 0, 0, 0)
	grid.SetHorizontalSpacing(14)
	grid.SetVerticalSpacing(2)

	hdrStyle := "color: #666; font-size: 11px;"
	makeHdr := func(text string, align qt.AlignmentFlag) *qt.QLabel {
		l := qt.NewQLabel3(text)
		l.SetStyleSheet(hdrStyle)
		l.SetAlignment(align)
		return l
	}

	headers := []struct {
		text  string
		align qt.AlignmentFlag
	}{
		{"Eye", qt.AlignLeft},
		{"PD / OC", qt.AlignLeft},
		{"BC / CT (r)", qt.AlignLeft},
		{"Mag (Maj / Min)", qt.AlignLeft},
		{"Edge (Max / Avg / Temp / Nasal)", qt.AlignLeft},
		{"Weight", qt.AlignRight},
	}
	for c, h := range headers {
		grid.AddWidget2(makeHdr(h.text, h.align).QWidget, 0, c)
	}

	monoFont := qt.QFontDatabase_SystemFont(qt.QFontDatabase__FixedFont)
	if pt := monoFont.PointSizeF(); pt > 2 {
		monoFont.SetPointSizeF(math.Max(8.5, pt-0.5))
	}

	makeEyeTag := func(text string) *qt.QLabel {
		l := qt.NewQLabel3(text)
		f := qt.NewQFont5(l.Font())
		f.SetBold(true)
		l.SetFont(f)
		return l
	}
	makeVal := func(align qt.AlignmentFlag) *qt.QLabel {
		l := qt.NewQLabel3("—")
		l.SetFont(monoFont)
		l.SetAlignment(align)
		return l
	}

	sp.odPDOC = makeVal(qt.AlignLeft)
	sp.odBCCT = makeVal(qt.AlignLeft)
	sp.odMag = makeVal(qt.AlignLeft)
	sp.odEdge = makeVal(qt.AlignLeft)
	sp.odWeight = makeVal(qt.AlignRight)

	sp.osPDOC = makeVal(qt.AlignLeft)
	sp.osBCCT = makeVal(qt.AlignLeft)
	sp.osMag = makeVal(qt.AlignLeft)
	sp.osEdge = makeVal(qt.AlignLeft)
	sp.osWeight = makeVal(qt.AlignRight)

	grid.AddWidget2(makeEyeTag("OD").QWidget, 1, 0)
	grid.AddWidget2(sp.odPDOC.QWidget, 1, 1)
	grid.AddWidget2(sp.odBCCT.QWidget, 1, 2)
	grid.AddWidget2(sp.odMag.QWidget, 1, 3)
	grid.AddWidget2(sp.odEdge.QWidget, 1, 4)
	grid.AddWidget2(sp.odWeight.QWidget, 1, 5)

	grid.AddWidget2(makeEyeTag("OS").QWidget, 2, 0)
	grid.AddWidget2(sp.osPDOC.QWidget, 2, 1)
	grid.AddWidget2(sp.osBCCT.QWidget, 2, 2)
	grid.AddWidget2(sp.osMag.QWidget, 2, 3)
	grid.AddWidget2(sp.osEdge.QWidget, 2, 4)
	grid.AddWidget2(sp.osWeight.QWidget, 2, 5)

	grid.SetColumnStretch(0, 0)
	grid.SetColumnStretch(1, 0)
	grid.SetColumnStretch(2, 0)
	grid.SetColumnStretch(3, 0)
	grid.SetColumnStretch(4, 1)
	grid.SetColumnStretch(5, 0)

	vbox.AddLayout(grid.QLayout)
	return sp
}

// Update refreshes the combined optical and thickness summary from normalized optics.Data,
// active shape/design metadata, and the computed lens thickness report.
func (sp *SummaryPanel) Update(d optics.Data, shapeOpt lens.ShapeOption, design optics.LensDesign, report lens.Report) {
	od, os := dsm.MakeDSM(d)
	odMajPct := (od.Magnification(od.FvMajor) - 1.0) * 100.0
	odMinPct := (od.Magnification(od.FvMinor) - 1.0) * 100.0
	osMajPct := (os.Magnification(os.FvMajor) - 1.0) * 100.0
	osMinPct := (os.Magnification(os.FvMinor) - 1.0) * 100.0

	maxDiffPct := 0.0
	maxDiffMer := 0
	for m := 0; m < 180; m++ {
		odLoc := od.ImageLocation(1.0, float64(m))
		osLoc := os.ImageLocation(1.0, float64(m))
		diff := math.Abs(odLoc-osLoc) * 100.0
		if diff > maxDiffPct {
			maxDiffPct = diff
			maxDiffMer = m
		}
	}

	rp := dsm.ReadingPrismaticDifference(d, 23, optics.DefaultReadingDistanceMM)
	dropStr := fmt.Sprintf("%.1f mm", rp.DropODMM)
	if math.Abs(rp.DropODMM-rp.DropOSMM) > 0.05 {
		dropStr = fmt.Sprintf("%.1f/%.1f mm", rp.DropODMM, rp.DropOSMM)
	}
	insetStr := fmt.Sprintf("%.1f mm", rp.InsetODMM)
	if math.Abs(rp.InsetODMM-rp.InsetOSMM) > 0.05 {
		insetStr = fmt.Sprintf("%.1f/%.1f mm", rp.InsetODMM, rp.InsetOSMM)
	}
	read23 := fmt.Sprintf("%s, in %s (%+.2f Δ)", dropStr, insetStr, rp.VerticalDiffPD)

	// Material description
	matDesc := ""
	if math.Abs(d.Lenses.OD.Index-d.Lenses.OS.Index) < 1e-6 &&
		d.Lenses.OD.Family == d.Lenses.OS.Family &&
		d.Lenses.OD.Abbe == d.Lenses.OS.Abbe &&
		math.Abs(d.Lenses.OD.SpecificGravity-d.Lenses.OS.SpecificGravity) < 1e-4 {
		matTag := ""
		if d.Lenses.OD.Family != "" {
			matTag = " " + string(d.Lenses.OD.Family)
		}
		matDesc = fmt.Sprintf("n=%.3f%s (V %d, SG %.2f)", d.Lenses.OD.Index, matTag, d.Lenses.OD.Abbe, d.Lenses.OD.SpecificGravity)
	} else {
		matDesc = fmt.Sprintf("OD n=%.3f (V %d) / OS n=%.3f (V %d)",
			d.Lenses.OD.Index, d.Lenses.OD.Abbe, d.Lenses.OS.Index, d.Lenses.OS.Abbe)
	}

	var hasThickness bool
	var designRes lens.DesignResult
	if len(report.Results) > 0 && len(report.Results[0].Designs) > 0 {
		hasThickness = true
		designRes = report.Results[0].Designs[0]
	}

	pairPart := ""
	if hasThickness {
		pairPart = fmt.Sprintf("  •  Pair %.1f g", designRes.PairWeight)
	}

	sp.headLabel.SetText(fmt.Sprintf(
		"%s  •  %s  •  %s%s  •  Max Diff %.2f%% @ %d°  •  23°: %s",
		shapeOpt.Name, design.String(), matDesc, pairPart, maxDiffPct, maxDiffMer, read23,
	))

	odOCH := d.Fit.EffectiveODOCH(d.Frame.B)
	osOCH := d.Fit.EffectiveOSOCH(d.Frame.B)
	sp.odPDOC.SetText(fmt.Sprintf("%.1f / %.1f mm", d.Fit.OD.PD, odOCH))
	sp.osPDOC.SetText(fmt.Sprintf("%.1f / %.1f mm", d.Fit.OS.PD, osOCH))

	sp.odBCCT.SetText(fmt.Sprintf("+%.2f D / %.2f mm (%.3f)", d.Lenses.OD.BaseCurve, d.Lenses.OD.CenterThickness, od.Ratio()))
	sp.osBCCT.SetText(fmt.Sprintf("+%.2f D / %.2f mm (%.3f)", d.Lenses.OS.BaseCurve, d.Lenses.OS.CenterThickness, os.Ratio()))

	sp.odMag.SetText(fmt.Sprintf("%+.2f%% / %+.2f%%", odMajPct, odMinPct))
	sp.osMag.SetText(fmt.Sprintf("%+.2f%% / %+.2f%%", osMajPct, osMinPct))

	if hasThickness {
		sp.odEdge.SetText(fmt.Sprintf("%.2f / %.2f / %.2f / %.2f mm",
			designRes.OD.MaxCaliperThickness, designRes.OD.MeanCaliperThickness, designRes.OD.TempHorizontal, designRes.OD.NasalHorizontal))
		sp.osEdge.SetText(fmt.Sprintf("%.2f / %.2f / %.2f / %.2f mm",
			designRes.OS.MaxCaliperThickness, designRes.OS.MeanCaliperThickness, designRes.OS.TempHorizontal, designRes.OS.NasalHorizontal))
		sp.odWeight.SetText(fmt.Sprintf("%.1f g", designRes.OD.Weight))
		sp.osWeight.SetText(fmt.Sprintf("%.1f g", designRes.OS.Weight))
	} else {
		sp.odEdge.SetText("—")
		sp.osEdge.SetText("—")
		sp.odWeight.SetText("—")
		sp.osWeight.SetText("—")
	}
}

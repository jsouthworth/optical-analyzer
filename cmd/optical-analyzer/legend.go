package main

import (
	"fmt"
	"image/color"
	"math"

	qt "github.com/mappu/miqt/qt6"
	"jsouthworth.net/go/optical-analyzer/dsm"
)

type legendStop struct {
	Label string
	Color color.RGBA
}

func srgbToLinear(c uint8) float64 {
	v := float64(c) / 255.0
	if v <= 0.03928 {
		return v / 12.92
	}
	return math.Pow((v+0.055)/1.055, 2.4)
}

// contrastTextColor picks black or white based on WCAG relative luminance so
// text has maximum contrast against background color c.
func contrastTextColor(c color.RGBA) string {
	lum := 0.2126*srgbToLinear(c.R) + 0.7152*srgbToLinear(c.G) + 0.0722*srgbToLinear(c.B)
	if lum > 0.1791 {
		return "#000000"
	}
	return "#ffffff"
}

func makeSwatchWidget(c color.RGBA, tooltip string, minW int) *qt.QLabel {
	lbl := qt.NewQLabel2()
	lbl.SetMinimumWidth(minW)
	lbl.SetFixedHeight(16)
	lbl.SetToolTip(tooltip)
	fg := contrastTextColor(c)
	lbl.SetStyleSheet(fmt.Sprintf(
		"QLabel { background-color: rgb(%d, %d, %d); border: 1px solid #888; } "+
			"QToolTip { background-color: rgb(%d, %d, %d); color: %s; border: 1px solid #666; padding: 2px 4px; }",
		c.R, c.G, c.B, c.R, c.G, c.B, fg,
	))
	return lbl
}

func buildPrismLegend(title string) *qt.QWidget {
	container := qt.NewQWidget2()
	outer := qt.NewQHBoxLayout(container)
	outer.SetContentsMargins(8, 4, 8, 4)
	outer.SetSpacing(4)

	titleLbl := qt.NewQLabel3(title)
	outer.AddWidget(titleLbl.QWidget)

	stops := []struct {
		val   float64
		label string
	}{
		{0.05, "< 0.125 Δ"},
		{0.15, "0.125–0.1875 Δ"},
		{0.20, "0.1875–0.25 Δ"},
		{0.28, "0.25–0.3125 Δ"},
		{0.34, "0.3125–0.375 Δ"},
		{0.40, "0.375–0.4375 Δ"},
		{0.46, "0.4375–0.50 Δ"},
		{0.52, "0.50–0.5625 Δ"},
		{0.58, "0.5625–0.625 Δ"},
		{0.70, "≥ 0.625 Δ"},
	}

	outer.AddWidget(qt.NewQLabel3("0").QWidget)
	for _, s := range stops {
		sw := makeSwatchWidget(dsm.PrismPointColor(s.val), s.label, 22)
		outer.AddWidget(sw.QWidget)
	}
	outer.AddWidget(qt.NewQLabel3("≥ 0.625 Δ").QWidget)
	outer.AddStretch()
	outer.AddWidget(qt.NewQLabel3("Marks: center cross, 5 mm circle, frame, 23° read dot, 30° & 45° ticks").QWidget)

	return container
}

func buildPrismaticDiffLegend() *qt.QWidget {
	container := qt.NewQWidget2()
	outer := qt.NewQHBoxLayout(container)
	outer.SetContentsMargins(8, 4, 8, 4)
	outer.SetSpacing(2)

	outer.AddWidget(qt.NewQLabel3("OD − OS Prism (Δ):  −4").QWidget)

	samples := []float64{
		-4.1, -3.9, -3.6, -3.4, -3.1, -2.9, -2.6, -2.4, -2.1,
		-1.9, -1.6, -1.4, -1.1, -0.9, -0.6, -0.4,
		0.0,
		0.4, 0.6, 0.9, 1.1, 1.4, 1.6, 1.9, 2.1,
		2.4, 2.6, 2.9, 3.1, 3.4, 3.6, 3.9, 4.1,
	}
	for _, v := range samples {
		tip := fmt.Sprintf("%.2f Δ", v)
		if v < -4 {
			tip = "< −4.00 Δ"
		} else if v > 4 {
			tip = "≥ +4.00 Δ"
		} else if v == 0 {
			tip = "−0.25 to +0.25 Δ"
		}
		sw := makeSwatchWidget(dsm.PrismaticDiffPointColor(v), tip, 10)
		outer.AddWidget(sw.QWidget)
	}

	outer.AddWidget(qt.NewQLabel3("+4 Δ").QWidget)
	outer.AddStretch()
	outer.AddWidget(qt.NewQLabel3("Red: OS > OD  |  White: |Δ| < 0.25  |  Green: OD > OS").QWidget)
	return container
}

func buildMagEllipseLegend() *qt.QWidget {
	container := qt.NewQWidget2()
	outer := qt.NewQHBoxLayout(container)
	outer.SetContentsMargins(8, 4, 8, 4)
	outer.SetSpacing(10)

	items := []legendStop{
		{Label: "Reference Circle (1.00×)", Color: color.RGBA{0xd0, 0xd0, 0xd0, 0xff}},
		{Label: "OD (Right) Ellipse & Minor Ref", Color: color.RGBA{0xff, 0x00, 0x00, 0xff}},
		{Label: "OS (Left) Ellipse & Minor Ref", Color: color.RGBA{0x00, 0xff, 0x00, 0xff}},
	}
	for _, it := range items {
		sw := makeSwatchWidget(it.Color, it.Label, 18)
		outer.AddWidget(sw.QWidget)
		outer.AddWidget(qt.NewQLabel3(it.Label).QWidget)
	}
	outer.AddStretch()
	outer.AddWidget(qt.NewQLabel3("Scroll to zoom, drag to pan, double-click to fit").QWidget)
	return container
}

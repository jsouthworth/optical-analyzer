package main

import (
	"fmt"
	"math"
	"strings"

	qt "github.com/mappu/miqt/qt6"
	"jsouthworth.net/go/optical-analyzer/contour"
	"jsouthworth.net/go/optical-analyzer/lens"
	"jsouthworth.net/go/optical-analyzer/optics"
)

type frameShapeOption struct {
	Label string
	Value string
	Exp   float64
}

var frameShapeOptions = []frameShapeOption{
	{Label: "Oval (n=2.0)", Value: "oval", Exp: contour.ShapeOval},
	{Label: "Soft Rect (n=3.0)", Value: "softrect", Exp: contour.ShapeSoftRect},
	{Label: "Squircle (n=4.0)", Value: "squircle", Exp: contour.ShapeSquircle},
	{Label: "Sharp Box (n=8.0)", Value: "box", Exp: contour.ShapeSharpRectangular},
	{Label: "Trace", Value: "trace", Exp: 0},
}

const defaultFrameShape = "oval"

type materialPreset struct {
	Label  string
	Value  string
	Index  float64
	Family optics.MaterialFamily
	Abbe   int
}

var materialPresets = buildMaterialPresets()

func buildMaterialPresets() []materialPreset {
	specs := optics.StandardMaterials()
	out := make([]materialPreset, 0, len(specs)+2)
	out = append(out, materialPreset{Label: "Auto", Value: "auto", Family: optics.FamilyAuto})
	for _, s := range specs {
		out = append(out, materialPreset{
			Label:  s.Name,
			Value:  s.Key,
			Index:  s.Index,
			Family: s.Family,
			Abbe:   s.Abbe,
		})
	}
	out = append(out, materialPreset{Label: "Custom", Value: "custom", Family: optics.FamilyAuto})
	return out
}

const (
	designBiAspherical = 0
	designAspherical   = 1
	designSpherical    = 2
)

// ControlPanel holds all input widgets for normalized dsm.Data plus a live optical summary,
// scaffolded from dsm-gui with added edging (chamfer/bevel), SG, and Auto material preset support.
type ControlPanel struct {
	ScrollArea *qt.QScrollArea

	// Right Eye (OD)
	odSph    *qt.QDoubleSpinBox
	odCyl    *qt.QDoubleSpinBox
	odAxis   *qt.QDoubleSpinBox
	odPD     *qt.QDoubleSpinBox
	odOCH    *qt.QDoubleSpinBox
	odRotCtr *qt.QDoubleSpinBox

	// Left Eye (OS)
	osSph    *qt.QDoubleSpinBox
	osCyl    *qt.QDoubleSpinBox
	osAxis   *qt.QDoubleSpinBox
	osPD     *qt.QDoubleSpinBox
	osOCH    *qt.QDoubleSpinBox
	osRotCtr *qt.QDoubleSpinBox

	// Lenses & Material
	presetCombo              *qt.QComboBox
	copyLensBtn              *qt.QPushButton
	odIndex, odThick, odBase *qt.QDoubleSpinBox
	odSG                     *qt.QDoubleSpinBox
	odAbbe                   *qt.QSpinBox
	osIndex, osThick, osBase *qt.QDoubleSpinBox
	osSG                     *qt.QDoubleSpinBox
	osAbbe                   *qt.QSpinBox
	odMaterial               optics.MaterialFamily
	osMaterial               optics.MaterialFamily
	designCombo              *qt.QComboBox
	chamferSpin              *qt.QDoubleSpinBox
	bevelSpin                *qt.QDoubleSpinBox

	// Frame
	frameA, frameB, frameDBL *qt.QDoubleSpinBox
	shapeCombo               *qt.QComboBox
	clearTraceBtn            *qt.QPushButton
	lastNonTraceShape        string

	// Optional loaded frame trace (used when Frame Shape == "trace")
	loadedTrace *contour.Trace

	suppressChanges bool
	onChange        func()
	onClearTrace    func()
}

func NewControlPanel(onChange, onClearTrace func()) *ControlPanel {
	cp := &ControlPanel{
		odMaterial:        optics.FamilyPlastic,
		osMaterial:        optics.FamilyPlastic,
		lastNonTraceShape: defaultFrameShape,
		onChange:          onChange,
		onClearTrace:      onClearTrace,
	}

	cp.ScrollArea = qt.NewQScrollArea2()
	cp.ScrollArea.SetWidgetResizable(true)
	cp.ScrollArea.SetHorizontalScrollBarPolicy(qt.ScrollBarAlwaysOff)

	inner := qt.NewQWidget2()
	mainLayout := qt.NewQVBoxLayout(inner)
	mainLayout.SetSpacing(6)
	mainLayout.SetContentsMargins(6, 4, 6, 4)

	mainLayout.AddWidget(cp.buildODGroup().QWidget)
	mainLayout.AddWidget(cp.buildOSGroup().QWidget)
	mainLayout.AddWidget(cp.buildLensGroup().QWidget)
	mainLayout.AddWidget(cp.buildFrameGroup().QWidget)
	mainLayout.AddStretch()

	cp.ScrollArea.SetWidget(inner)

	minHint := inner.MinimumSizeHint()
	minW := minHint.Width() + 28
	if minW < 340 {
		minW = 340
	}
	cp.ScrollArea.SetMinimumWidth(minW)

	return cp
}

func (cp *ControlPanel) traceShapeIndex() int {
	for i, opt := range frameShapeOptions {
		if opt.Value == "trace" {
			return i
		}
	}
	return -1
}

func (cp *ControlPanel) updateTraceShapeAvailability() {
	hasTrace := cp.HasLoadedTrace()
	if cp.clearTraceBtn != nil {
		cp.clearTraceBtn.SetEnabled(hasTrace)
	}
	idx := cp.traceShapeIndex()
	if idx < 0 || cp.shapeCombo == nil {
		return
	}
	// QStandardItemModel (backing QComboBox) accepts Qt::ItemFlags via Qt::UserRole - 1.
	const itemFlagsRole = int(qt.UserRole) - 1
	if hasTrace {
		cp.shapeCombo.SetItemData2(idx, qt.NewQVariant4(int(qt.ItemIsSelectable|qt.ItemIsEnabled)), itemFlagsRole)
	} else {
		cp.shapeCombo.SetItemData2(idx, qt.NewQVariant4(0), itemFlagsRole)
	}
}

// SetLoadedTrace updates the loaded frame trace. When a trace is loaded, Frame Shape
// switches to "Trace" (remembering the previous geometric shape); when cleared, it
// restores the previous geometric shape and disables the "Trace" dropdown option.
func (cp *ControlPanel) SetLoadedTrace(tr *contour.Trace) {
	cp.suppressChanges = true
	defer func() { cp.suppressChanges = false }()

	cp.loadedTrace = tr
	cp.updateTraceShapeAvailability()

	if tr != nil {
		if cur := cp.SelectedShape(); cur != "trace" && cur != "" {
			cp.lastNonTraceShape = cur
		}
		cp.selectShapeCombo("trace")
	} else if cp.SelectedShape() == "trace" {
		fallback := cp.lastNonTraceShape
		if fallback == "" || fallback == "trace" {
			fallback = defaultFrameShape
		}
		cp.selectShapeCombo(fallback)
	}
}

func (cp *ControlPanel) HasLoadedTrace() bool {
	return cp.loadedTrace != nil
}

func (cp *ControlPanel) SelectedShape() string {
	if cp.shapeCombo == nil {
		return defaultFrameShape
	}
	idx := cp.shapeCombo.CurrentIndex()
	if idx < 0 || idx >= len(frameShapeOptions) {
		return defaultFrameShape
	}
	return frameShapeOptions[idx].Value
}

func normalizeShapeValue(shape string) string {
	switch strings.ToLower(strings.TrimSpace(shape)) {
	case "oval", "ellipse", "", "all":
		return "oval"
	case "softrect", "soft-rect", "soft_rect", "p3", "wayfarer":
		return "softrect"
	case "squircle", "rect", "rectangle":
		return "squircle"
	case "box", "sharp", "sharpbox", "sharp-box":
		return "box"
	case "trace":
		return "trace"
	default:
		return defaultFrameShape
	}
}

func (cp *ControlPanel) selectShapeCombo(val string) {
	for i, opt := range frameShapeOptions {
		if opt.Value == val {
			cp.shapeCombo.SetCurrentIndex(i)
			return
		}
	}
	for i, opt := range frameShapeOptions {
		if opt.Value == defaultFrameShape {
			cp.shapeCombo.SetCurrentIndex(i)
			return
		}
	}
}

// SetShape sets the Frame Shape dropdown without triggering change notifications.
func (cp *ControlPanel) SetShape(shape string) {
	cp.suppressChanges = true
	defer func() { cp.suppressChanges = false }()

	val := normalizeShapeValue(shape)
	if val == "trace" && !cp.HasLoadedTrace() {
		val = cp.lastNonTraceShape
		if val == "" || val == "trace" {
			val = defaultFrameShape
		}
	}
	if val != "trace" {
		cp.lastNonTraceShape = val
	}
	cp.selectShapeCombo(val)
}

// ActiveShapeOption returns the active frame shape contour option for thickness modeling,
// lens preview, and material recommendations.
func (cp *ControlPanel) ActiveShapeOption() lens.ShapeOption {
	frameA := cp.frameA.Value()
	frameB := cp.frameB.Value()
	shape := cp.SelectedShape()
	if shape == "trace" && cp.loadedTrace != nil {
		tr := cp.loadedTrace
		odC, osC := tr.OD, tr.OS
		if tr.FrameA > 0 && tr.FrameB > 0 && frameA > 0 && frameB > 0 &&
			(math.Abs(frameA-tr.FrameA) > 0.06 || math.Abs(frameB-tr.FrameB) > 0.06) {
			sx := frameA / tr.FrameA
			sy := frameB / tr.FrameB
			odC = contour.Scale(odC, sx, sy)
			osC = contour.Scale(osC, sx, sy)
		}
		name := tr.Name
		if name == "" {
			name = "Loaded Trace"
		}
		return lens.ShapeOption{
			Name:        name,
			ODContour:   &odC,
			OSContour:   &osC,
			Source:      tr.Source,
			GrooveDepth: tr.GrooveDepthMM,
		}
	}
	for _, opt := range frameShapeOptions {
		if opt.Value == shape && opt.Exp > 0 {
			return lens.ShapeOption{Name: opt.Label, Exp: opt.Exp}
		}
	}
	return lens.ShapeOption{Name: "Oval (n=2.0)", Exp: contour.ShapeOval}
}

// ActiveContourTrace returns a *contour.Trace representing the currently selected Frame Shape
// contour (either the loaded trace when Shape == "trace", or the superellipse contour for
// the chosen geometric shape), used by Chromatic Aberration overlays and CT estimation.
func (cp *ControlPanel) ActiveContourTrace() *contour.Trace {
	frameA := cp.frameA.Value()
	frameB := cp.frameB.Value()
	frameDBL := cp.frameDBL.Value()
	if frameA <= 0 || frameB <= 0 {
		return nil
	}
	if cp.SelectedShape() == "trace" && cp.loadedTrace != nil {
		if cp.loadedTrace.FrameA > 0 && cp.loadedTrace.FrameB > 0 &&
			math.Abs(frameA-cp.loadedTrace.FrameA) <= 0.06 &&
			math.Abs(frameB-cp.loadedTrace.FrameB) <= 0.06 {
			return cp.loadedTrace
		}
		opt := cp.ActiveShapeOption()
		if opt.ODContour != nil && opt.OSContour != nil {
			return &contour.Trace{
				Name:     opt.Name,
				Source:   opt.Source,
				FrameA:   frameA,
				FrameB:   frameB,
				FrameDBL: frameDBL,
				OD:       *opt.ODContour,
				OS:       *opt.OSContour,
			}
		}
		return cp.loadedTrace
	}

	opt := cp.ActiveShapeOption()
	odC, osC := lens.ResolveContours(opt, frameA, frameB)
	return &contour.Trace{
		Name:     opt.Name,
		FrameA:   frameA,
		FrameB:   frameB,
		FrameDBL: frameDBL,
		OD:       odC,
		OS:       osC,
	}
}

func (cp *ControlPanel) notifyChange() {
	if !cp.suppressChanges && cp.onChange != nil {
		cp.onChange()
	}
}

func (cp *ControlPanel) newDoubleSpin(min, max, step float64, decimals int, suffix string) *qt.QDoubleSpinBox {
	sb := qt.NewQDoubleSpinBox2()
	sb.SetRange(min, max)
	sb.SetSingleStep(step)
	sb.SetDecimals(decimals)
	if suffix != "" {
		sb.SetSuffix(suffix)
	}
	sb.SetSizePolicy2(qt.QSizePolicy__Expanding, qt.QSizePolicy__Fixed)
	sb.SetMinimumWidth(64)
	sb.OnValueChanged(func(float64) { cp.notifyChange() })
	return sb
}

func (cp *ControlPanel) newIntSpin(min, max, step int, suffix string) *qt.QSpinBox {
	sb := qt.NewQSpinBox2()
	sb.SetRange(min, max)
	sb.SetSingleStep(step)
	if suffix != "" {
		sb.SetSuffix(suffix)
	}
	sb.SetSizePolicy2(qt.QSizePolicy__Expanding, qt.QSizePolicy__Fixed)
	sb.SetMinimumWidth(64)
	sb.OnValueChanged(func(int) { cp.notifyChange() })
	return sb
}

func addLabeledWidget(grid *qt.QGridLayout, row, col int, label string, w *qt.QWidget) {
	cell := qt.NewQWidget2()
	vl := qt.NewQVBoxLayout(cell)
	vl.SetContentsMargins(0, 0, 0, 0)
	vl.SetSpacing(2)
	lbl := qt.NewQLabel3(label)
	vl.AddWidget(lbl.QWidget)
	vl.AddWidget(w)
	grid.AddWidget2(cell, row, col)
}

func (cp *ControlPanel) buildODGroup() *qt.QGroupBox {
	group := qt.NewQGroupBox3("Right Eye (OD)")
	grid := qt.NewQGridLayout(group.QWidget)
	grid.SetSpacing(6)

	cp.odSph = cp.newDoubleSpin(-30, 30, 0.25, 2, " D")
	cp.odCyl = cp.newDoubleSpin(-15, 15, 0.25, 2, " D")
	cp.odAxis = cp.newDoubleSpin(0, 180, 1, 0, "°")

	cp.odPD = cp.newDoubleSpin(0, 50, 0.5, 1, " mm")
	cp.odPD.SetSpecialValueText("Auto")
	cp.odPD.SetToolTip("Monocular OD PD in mm (0 = Auto (A+DBL)/2)")

	cp.odOCH = cp.newDoubleSpin(0, 80, 0.5, 1, " mm")
	cp.odOCH.SetSpecialValueText("Auto (B/2)")
	cp.odOCH.SetToolTip("OD optical center height from bottom of frame (0 = Auto B/2)")

	cp.odRotCtr = cp.newDoubleSpin(5.0, 100.0, 0.5, 1, " mm")
	cp.odRotCtr.SetToolTip("OD distance from back vertex to center of rotation in mm (typical: 27.0 mm)")

	addLabeledWidget(grid, 0, 0, "Sphere (D)", cp.odSph.QWidget)
	addLabeledWidget(grid, 0, 1, "Cyl (D)", cp.odCyl.QWidget)
	addLabeledWidget(grid, 0, 2, "Axis (°)", cp.odAxis.QWidget)
	addLabeledWidget(grid, 1, 0, "Mono PD", cp.odPD.QWidget)
	addLabeledWidget(grid, 1, 1, "OC Ht", cp.odOCH.QWidget)
	addLabeledWidget(grid, 1, 2, "Rot. Ctr", cp.odRotCtr.QWidget)
	return group
}

func (cp *ControlPanel) buildOSGroup() *qt.QGroupBox {
	group := qt.NewQGroupBox3("Left Eye (OS)")
	vbox := qt.NewQVBoxLayout(group.QWidget)
	vbox.SetSpacing(6)

	grid := qt.NewQGridLayout2()
	grid.SetSpacing(6)

	cp.osSph = cp.newDoubleSpin(-30, 30, 0.25, 2, " D")
	cp.osCyl = cp.newDoubleSpin(-15, 15, 0.25, 2, " D")
	cp.osAxis = cp.newDoubleSpin(0, 180, 1, 0, "°")

	cp.osPD = cp.newDoubleSpin(0, 50, 0.5, 1, " mm")
	cp.osPD.SetSpecialValueText("Auto")
	cp.osPD.SetToolTip("Monocular OS PD in mm (0 = Auto (A+DBL)/2)")

	cp.osOCH = cp.newDoubleSpin(0, 80, 0.5, 1, " mm")
	cp.osOCH.SetSpecialValueText("Auto (B/2)")
	cp.osOCH.SetToolTip("OS optical center height from bottom of frame (0 = Auto B/2)")

	cp.osRotCtr = cp.newDoubleSpin(5.0, 100.0, 0.5, 1, " mm")
	cp.osRotCtr.SetToolTip("OS distance from back vertex to center of rotation in mm (typical: 27.0 mm)")

	addLabeledWidget(grid, 0, 0, "Sphere (D)", cp.osSph.QWidget)
	addLabeledWidget(grid, 0, 1, "Cyl (D)", cp.osCyl.QWidget)
	addLabeledWidget(grid, 0, 2, "Axis (°)", cp.osAxis.QWidget)
	addLabeledWidget(grid, 1, 0, "Mono PD", cp.osPD.QWidget)
	addLabeledWidget(grid, 1, 1, "OC Ht", cp.osOCH.QWidget)
	addLabeledWidget(grid, 1, 2, "Rot. Ctr", cp.osRotCtr.QWidget)
	vbox.AddLayout(grid.QLayout)

	btnRow := qt.NewQHBoxLayout2()
	btnRow.AddStretch()
	copyBtn := qt.NewQPushButton3("Copy OD → OS")
	copyBtn.SetToolTip("Copy OD Sphere, Cyl, Axis, PD, OC Ht, and Rot. Ctr to OS")
	copyBtn.OnClicked(func() {
		cp.suppressChanges = true
		cp.osSph.SetValue(cp.odSph.Value())
		cp.osCyl.SetValue(cp.odCyl.Value())
		cp.osAxis.SetValue(cp.odAxis.Value())
		cp.osPD.SetValue(cp.odPD.Value())
		cp.osOCH.SetValue(cp.odOCH.Value())
		cp.osRotCtr.SetValue(cp.odRotCtr.Value())
		cp.suppressChanges = false
		cp.notifyChange()
	})
	btnRow.AddWidget(copyBtn.QWidget)
	vbox.AddLayout(btnRow.QLayout)

	return group
}

func (cp *ControlPanel) buildLensGroup() *qt.QGroupBox {
	group := qt.NewQGroupBox3("Material && Optics")
	vbox := qt.NewQVBoxLayout(group.QWidget)
	vbox.SetSpacing(6)

	presetRow := qt.NewQHBoxLayout2()
	cp.presetCombo = qt.NewQComboBox2()
	cp.presetCombo.SetSizeAdjustPolicy(qt.QComboBox__AdjustToMinimumContentsLengthWithIcon)
	cp.presetCombo.SetMinimumContentsLength(12)
	cp.presetCombo.SetSizePolicy2(qt.QSizePolicy__Expanding, qt.QSizePolicy__Fixed)
	cp.presetCombo.SetToolTip("Select Auto (#1 recommended), a standard lens material, or Custom to edit Index/Abbe/SG manually")
	for _, p := range materialPresets {
		cp.presetCombo.AddItem(p.Label)
	}
	cp.presetCombo.OnCurrentIndexChanged(func(idx int) {
		if cp.suppressChanges || idx < 0 || idx >= len(materialPresets) {
			return
		}
		p := materialPresets[idx]
		cp.updateMaterialDerivedLockState()
		if p.Value == "custom" {
			cp.odMaterial = optics.FamilyAuto
			cp.osMaterial = optics.FamilyAuto
			cp.notifyChange()
			return
		}
		if p.Index > 0 {
			cp.odMaterial = p.Family
			cp.osMaterial = p.Family
			sg := optics.EstimateSpecificGravity(p.Index, p.Family)
			cp.suppressChanges = true
			cp.odIndex.SetValue(p.Index)
			cp.osIndex.SetValue(p.Index)
			if p.Abbe > 0 {
				cp.odAbbe.SetValue(p.Abbe)
				cp.osAbbe.SetValue(p.Abbe)
			}
			cp.odSG.SetValue(sg)
			cp.osSG.SetValue(sg)
			cp.suppressChanges = false
		}
		cp.notifyChange()
	})
	presetRow.AddWidget2(cp.presetCombo.QWidget, 1)

	cp.copyLensBtn = qt.NewQPushButton3("OD → OS")
	cp.copyLensBtn.SetToolTip("Copy OD lens parameters to OS")
	cp.copyLensBtn.OnClicked(func() {
		cp.suppressChanges = true
		cp.osThick.SetValue(cp.odThick.Value())
		cp.osBase.SetValue(cp.odBase.Value())
		if cp.IsCustomMaterial() {
			cp.osIndex.SetValue(cp.odIndex.Value())
			cp.osAbbe.SetValue(cp.odAbbe.Value())
			cp.osSG.SetValue(cp.odSG.Value())
			cp.osMaterial = cp.odMaterial
		}
		cp.suppressChanges = false
		cp.notifyChange()
	})
	presetRow.AddWidget2(cp.copyLensBtn.QWidget, 0)
	vbox.AddLayout(presetRow.QLayout)

	grid := qt.NewQGridLayout2()
	grid.SetSpacing(6)
	grid.SetColumnStretch(0, 0)
	grid.SetColumnStretch(1, 1)
	grid.SetColumnStretch(2, 1)

	grid.AddWidget2(qt.NewQLabel3("").QWidget, 0, 0)
	odHdr := qt.NewQLabel3("OD (Right)")
	odHdr.SetAlignment(qt.AlignCenter)
	osHdr := qt.NewQLabel3("OS (Left)")
	osHdr.SetAlignment(qt.AlignCenter)
	grid.AddWidget2(odHdr.QWidget, 0, 1)
	grid.AddWidget2(osHdr.QWidget, 0, 2)

	cp.odIndex = cp.newDoubleSpin(1.400, 2.000, 0.01, 3, "")
	cp.osIndex = cp.newDoubleSpin(1.400, 2.000, 0.01, 3, "")

	cp.odThick = cp.newDoubleSpin(0.0, 15.0, 0.1, 2, " mm")
	cp.osThick = cp.newDoubleSpin(0.0, 15.0, 0.1, 2, " mm")
	cp.odThick.SetSpecialValueText("Auto")
	cp.osThick.SetSpecialValueText("Auto")
	cp.odThick.SetToolTip("Center thickness in mm (0 = Auto calculated from material, design, RX, and contour)")
	cp.osThick.SetToolTip("Center thickness in mm (0 = Auto calculated from material, design, RX, and contour)")

	cp.designCombo = qt.NewQComboBox2()
	cp.designCombo.SetSizeAdjustPolicy(qt.QComboBox__AdjustToMinimumContentsLengthWithIcon)
	cp.designCombo.SetMinimumContentsLength(10)
	cp.designCombo.SetSizePolicy2(qt.QSizePolicy__Expanding, qt.QSizePolicy__Fixed)
	cp.designCombo.AddItem("Bi-Aspheric")
	cp.designCombo.AddItem("Aspheric")
	cp.designCombo.AddItem("Spherical")
	cp.designCombo.SetCurrentIndex(designBiAspherical)
	cp.designCombo.SetToolTip("Lens surface design used for auto base curve and center thickness calculation")
	cp.designCombo.OnCurrentIndexChanged(func(int) {
		cp.notifyChange()
	})

	cp.odBase = cp.newDoubleSpin(0.0, 12.0, 0.25, 2, " D")
	cp.osBase = cp.newDoubleSpin(0.0, 12.0, 0.25, 2, " D")
	cp.odBase.SetSpecialValueText("Auto")
	cp.osBase.SetSpecialValueText("Auto")
	cp.odBase.SetToolTip("Front base curve in diopters (0 = Auto calculated from RX and design)")
	cp.osBase.SetToolTip("Front base curve in diopters (0 = Auto calculated from RX and design)")

	cp.odAbbe = cp.newIntSpin(1, 100, 1, "")
	cp.osAbbe = cp.newIntSpin(1, 100, 1, "")
	cp.odAbbe.SetToolTip("Abbe number / constringence (higher = less chromatic aberration)")
	cp.osAbbe.SetToolTip("Abbe number / constringence (higher = less chromatic aberration)")

	cp.odSG = cp.newDoubleSpin(0.0, 5.0, 0.05, 2, "")
	cp.osSG = cp.newDoubleSpin(0.0, 5.0, 0.05, 2, "")
	cp.odSG.SetSpecialValueText("Auto")
	cp.osSG.SetSpecialValueText("Auto")
	cp.odSG.SetToolTip("Specific gravity in g/cm³ (0 = Auto from material)")
	cp.osSG.SetToolTip("Specific gravity in g/cm³ (0 = Auto from material)")

	cp.chamferSpin = cp.newDoubleSpin(0.0, 2.0, 0.05, 2, " mm")
	cp.chamferSpin.SetToolTip("Edge pin-bevel / chamfer thickness reduction in mm")
	cp.bevelSpin = cp.newDoubleSpin(0.0, 5.0, 0.1, 2, " mm")
	cp.bevelSpin.SetToolTip("Eyewire V-bevel / groove depth per side in mm (0 = rimless/flat edge)")

	grid.AddWidget2(qt.NewQLabel3("Index (n):").QWidget, 1, 0)
	grid.AddWidget2(cp.odIndex.QWidget, 1, 1)
	grid.AddWidget2(cp.osIndex.QWidget, 1, 2)

	grid.AddWidget2(qt.NewQLabel3("Design:").QWidget, 2, 0)
	grid.AddWidget3(cp.designCombo.QWidget, 2, 1, 1, 2)

	grid.AddWidget2(qt.NewQLabel3("Base Curve:").QWidget, 3, 0)
	grid.AddWidget2(cp.odBase.QWidget, 3, 1)
	grid.AddWidget2(cp.osBase.QWidget, 3, 2)

	grid.AddWidget2(qt.NewQLabel3("Thickness:").QWidget, 4, 0)
	grid.AddWidget2(cp.odThick.QWidget, 4, 1)
	grid.AddWidget2(cp.osThick.QWidget, 4, 2)

	grid.AddWidget2(qt.NewQLabel3("Abbe / V:").QWidget, 5, 0)
	grid.AddWidget2(cp.odAbbe.QWidget, 5, 1)
	grid.AddWidget2(cp.osAbbe.QWidget, 5, 2)

	grid.AddWidget2(qt.NewQLabel3("Spec. Grav.:").QWidget, 6, 0)
	grid.AddWidget2(cp.odSG.QWidget, 6, 1)
	grid.AddWidget2(cp.osSG.QWidget, 6, 2)

	vbox.AddLayout(grid.QLayout)

	edgingRow := qt.NewQHBoxLayout2()
	edgingRow.SetSpacing(6)
	edgingRow.AddWidget(qt.NewQLabel3("Chamfer:").QWidget)
	edgingRow.AddWidget2(cp.chamferSpin.QWidget, 1)
	edgingRow.AddWidget(qt.NewQLabel3("Bevel:").QWidget)
	edgingRow.AddWidget2(cp.bevelSpin.QWidget, 1)
	vbox.AddLayout(edgingRow.QLayout)

	return group
}

// SelectedMaterialPresetValue returns the Value of the currently selected Materials dropdown option
// ("auto", a material preset like "1.60-plastic", or "custom").
func (cp *ControlPanel) SelectedMaterialPresetValue() string {
	idx := cp.presetCombo.CurrentIndex()
	if idx < 0 || idx >= len(materialPresets) {
		return "custom"
	}
	return materialPresets[idx].Value
}

func (cp *ControlPanel) AutoMaterial() bool {
	return cp.SelectedMaterialPresetValue() == "auto"
}

func (cp *ControlPanel) IsCustomMaterial() bool {
	return cp.SelectedMaterialPresetValue() == "custom"
}

// updateMaterialDerivedLockState greys out all fields derived from the material
// (Index, Abbe/V, and Specific Gravity) whenever anything other than "Custom" is selected.
func (cp *ControlPanel) updateMaterialDerivedLockState() {
	editable := cp.IsCustomMaterial()
	cp.odIndex.SetEnabled(editable)
	cp.osIndex.SetEnabled(editable)
	cp.odAbbe.SetEnabled(editable)
	cp.osAbbe.SetEnabled(editable)
	cp.odSG.SetEnabled(editable)
	cp.osSG.SetEnabled(editable)
}

// ApplyAutoMaterial populates the material-derived controls with the #1 recommendation
// while keeping the Materials dropdown on "Auto".
func (cp *ControlPanel) ApplyAutoMaterial(rec lens.MaterialRecommendation) {
	cp.suppressChanges = true
	defer func() { cp.suppressChanges = false }()

	cp.odMaterial = rec.Family
	cp.osMaterial = rec.Family
	cp.odIndex.SetValue(rec.Index)
	cp.osIndex.SetValue(rec.Index)
	abbe := rec.Abbe
	if abbe <= 0 {
		abbe = optics.EstimateAbbe(rec.Index, rec.Family)
	}
	cp.odAbbe.SetValue(abbe)
	cp.osAbbe.SetValue(abbe)
	sg := rec.SpecificGravity
	if sg <= 0 {
		sg = optics.EstimateSpecificGravity(rec.Index, rec.Family)
	}
	cp.odSG.SetValue(sg)
	cp.osSG.SetValue(sg)
	cp.presetCombo.SetItemText(0, fmt.Sprintf("Auto (%s)", rec.Name))
	cp.selectMaterialPresetCombo("auto")
	cp.updateMaterialDerivedLockState()
}

// SelectMaterial switches the Materials dropdown to the matching named material preset
// (or Custom if unmatched) and updates the material-derived spinboxes without emitting signals.
func (cp *ControlPanel) SelectMaterial(index float64, family optics.MaterialFamily, abbe int) {
	cp.suppressChanges = true
	defer func() { cp.suppressChanges = false }()

	presetVal, mat := matchMaterialPreset(index, family)
	cp.odMaterial = mat
	cp.osMaterial = mat
	cp.selectMaterialPresetCombo(presetVal)
	cp.odIndex.SetValue(index)
	cp.osIndex.SetValue(index)
	if abbe <= 0 {
		abbe = optics.EstimateAbbe(index, mat)
	}
	cp.odAbbe.SetValue(abbe)
	cp.osAbbe.SetValue(abbe)
	sg := optics.EstimateSpecificGravity(index, mat)
	cp.odSG.SetValue(sg)
	cp.osSG.SetValue(sg)
	cp.updateMaterialDerivedLockState()
}

func (cp *ControlPanel) buildFrameGroup() *qt.QGroupBox {
	group := qt.NewQGroupBox3("Frame")
	grid := qt.NewQGridLayout(group.QWidget)
	grid.SetSpacing(6)

	cp.frameA = cp.newDoubleSpin(10, 100, 0.5, 1, " mm")
	cp.frameB = cp.newDoubleSpin(10, 100, 0.5, 1, " mm")
	cp.frameDBL = cp.newDoubleSpin(0, 50, 0.5, 1, " mm")

	addLabeledWidget(grid, 0, 0, "A (mm)", cp.frameA.QWidget)
	addLabeledWidget(grid, 0, 1, "B (mm)", cp.frameB.QWidget)
	addLabeledWidget(grid, 0, 2, "DBL (mm)", cp.frameDBL.QWidget)

	shapeRow := qt.NewQWidget2()
	sl := qt.NewQHBoxLayout(shapeRow)
	sl.SetContentsMargins(0, 0, 0, 0)
	sl.SetSpacing(6)
	sl.AddWidget(qt.NewQLabel3("Shape:").QWidget)

	cp.shapeCombo = qt.NewQComboBox2()
	cp.shapeCombo.SetSizeAdjustPolicy(qt.QComboBox__AdjustToMinimumContentsLengthWithIcon)
	cp.shapeCombo.SetMinimumContentsLength(10)
	cp.shapeCombo.SetSizePolicy2(qt.QSizePolicy__Expanding, qt.QSizePolicy__Fixed)
	cp.shapeCombo.SetToolTip("Frame contour shape used across Chromatic Aberration, Lens Thickness, and Material Recommendation")
	for _, opt := range frameShapeOptions {
		cp.shapeCombo.AddItem(opt.Label)
	}
	cp.selectShapeCombo(defaultFrameShape)
	cp.updateTraceShapeAvailability()
	cp.shapeCombo.OnCurrentIndexChanged(func(idx int) {
		if cp.suppressChanges || idx < 0 || idx >= len(frameShapeOptions) {
			return
		}
		val := frameShapeOptions[idx].Value
		if val == "trace" && !cp.HasLoadedTrace() {
			cp.suppressChanges = true
			fallback := cp.lastNonTraceShape
			if fallback == "" || fallback == "trace" {
				fallback = defaultFrameShape
			}
			cp.selectShapeCombo(fallback)
			cp.suppressChanges = false
			return
		}
		if val != "trace" {
			cp.lastNonTraceShape = val
		}
		cp.notifyChange()
	})
	sl.AddWidget2(cp.shapeCombo.QWidget, 1)

	grid.AddWidget3(shapeRow, 1, 0, 1, 3)

	btnRow := qt.NewQWidget2()
	bl := qt.NewQHBoxLayout(btnRow)
	bl.SetContentsMargins(0, 0, 0, 0)
	bl.AddStretch()
	cp.clearTraceBtn = qt.NewQPushButton3("Clear Trace")
	cp.clearTraceBtn.SetToolTip("Clear the loaded frame trace and restore the previous frame shape")
	cp.clearTraceBtn.SetEnabled(cp.HasLoadedTrace())
	cp.clearTraceBtn.OnClicked(func() {
		if cp.onClearTrace != nil {
			cp.onClearTrace()
		}
	})
	bl.AddWidget(cp.clearTraceBtn.QWidget)
	grid.AddWidget3(btnRow, 2, 0, 1, 3)

	return group
}

func (cp *ControlPanel) SelectedDesign() optics.LensDesign {
	switch cp.designCombo.CurrentIndex() {
	case designAspherical:
		return optics.DesignAspheric
	case designSpherical:
		return optics.DesignSpherical
	default:
		return optics.DesignBiAspheric
	}
}

func matchMaterialPreset(idx float64, current optics.MaterialFamily) (string, optics.MaterialFamily) {
	if spec, ok := optics.LookupMaterial(idx, current); ok {
		return spec.Key, spec.Family
	}
	return "custom", current
}

func (cp *ControlPanel) selectMaterialPresetCombo(val string) {
	for i, p := range materialPresets {
		if p.Value == val {
			cp.presetCombo.SetCurrentIndex(i)
			return
		}
	}
	cp.presetCombo.SetCurrentIndex(len(materialPresets) - 1)
}

// RawData returns the current live widget values for calculation, keeping 0 for
// auto-calculated BC/CT/PD/OCH while including the active material's Index/Abbe/SG.
func (cp *ControlPanel) RawData() optics.Data {
	chamfer := cp.chamferSpin.Value()
	bevel := cp.bevelSpin.Value()
	return optics.Data{
		Version: optics.CurrentSchemaVersion,
		RX: optics.RX{
			OD: optics.Refraction{
				Sph:  cp.odSph.Value(),
				Cyl:  cp.odCyl.Value(),
				Axis: cp.odAxis.Value(),
			},
			OS: optics.Refraction{
				Sph:  cp.osSph.Value(),
				Cyl:  cp.osCyl.Value(),
				Axis: cp.osAxis.Value(),
			},
		},
		Lenses: optics.LensPair{
			Design:     cp.SelectedDesign(),
			Chamfer:    &chamfer,
			BevelDepth: &bevel,
			OD: optics.LensParameters{
				Index:           cp.odIndex.Value(),
				Family:          cp.odMaterial,
				SpecificGravity: cp.odSG.Value(),
				CenterThickness: cp.odThick.Value(),
				BaseCurve:       cp.odBase.Value(),
				Abbe:            cp.odAbbe.Value(),
			},
			OS: optics.LensParameters{
				Index:           cp.osIndex.Value(),
				Family:          cp.osMaterial,
				SpecificGravity: cp.osSG.Value(),
				CenterThickness: cp.osThick.Value(),
				BaseCurve:       cp.osBase.Value(),
				Abbe:            cp.osAbbe.Value(),
			},
		},
		Frame: optics.Frame{
			A:     cp.frameA.Value(),
			B:     cp.frameB.Value(),
			DBL:   cp.frameDBL.Value(),
			Shape: cp.SelectedShape(),
		},
		Fit: optics.Fit{
			OD: optics.EyeFit{
				PD:               cp.odPD.Value(),
				OCH:              cp.odOCH.Value(),
				CenterOfRotation: cp.odRotCtr.Value(),
			},
			OS: optics.EyeFit{
				PD:               cp.osPD.Value(),
				OCH:              cp.osOCH.Value(),
				CenterOfRotation: cp.osRotCtr.Value(),
			},
		},
	}
}

// CommitEdits interprets any pending uncommitted text typed into enabled spinboxes
// (e.g. when the user presses Ctrl+S or clicks a NoFocus toolbar Save button right after typing).
func (cp *ControlPanel) CommitEdits() {
	for _, sb := range []*qt.QDoubleSpinBox{
		cp.odSph, cp.odCyl, cp.odAxis, cp.odPD, cp.odOCH, cp.odRotCtr,
		cp.osSph, cp.osCyl, cp.osAxis, cp.osPD, cp.osOCH, cp.osRotCtr,
		cp.odIndex, cp.odThick, cp.odBase, cp.odSG,
		cp.osIndex, cp.osThick, cp.osBase, cp.osSG,
		cp.chamferSpin, cp.bevelSpin,
		cp.frameA, cp.frameB, cp.frameDBL,
	} {
		if sb != nil && sb.IsEnabled() {
			sb.InterpretText()
		}
	}
	for _, sb := range []*qt.QSpinBox{cp.odAbbe, cp.osAbbe} {
		if sb != nil && sb.IsEnabled() {
			sb.InterpretText()
		}
	}
}

// SaveableData returns RawData cleaned for saving to a .json/.dat file:
// all fields that match their defaults (CenterOfRotation, Design, Chamfer, BevelDepth,
// Auto material, derived preset Abbe/SG, default Frame A/B/DBL and default Shape) are
// omitted so saved files stay minimal and reload cleanly.
func (cp *ControlPanel) SaveableData() optics.Data {
	cp.CommitEdits()
	d := cp.RawData()

	odCor := d.Fit.OD.CenterOfRotation
	osCor := d.Fit.OS.CenterOfRotation
	if math.Abs(odCor-optics.DefaultCenterOfRotationMM) < 1e-6 &&
		math.Abs(osCor-optics.DefaultCenterOfRotationMM) < 1e-6 {
		d.Fit.CenterOfRotation = 0
		d.Fit.OD.CenterOfRotation = 0
		d.Fit.OS.CenterOfRotation = 0
	} else if math.Abs(odCor-osCor) < 1e-6 {
		d.Fit.CenterOfRotation = odCor
		d.Fit.OD.CenterOfRotation = 0
		d.Fit.OS.CenterOfRotation = 0
	}

	if math.Abs(d.Lenses.EffectiveChamfer()-optics.DefaultChamferMM) < 1e-6 {
		d.Lenses.Chamfer = nil
	}
	if math.Abs(d.Lenses.EffectiveBevelDepth()-optics.DefaultBevelDepthMM) < 1e-6 {
		d.Lenses.BevelDepth = nil
	}

	preset := cp.SelectedMaterialPresetValue()
	switch preset {
	case "auto":
		d.Lenses.Material = ""
		d.Lenses.OD.Index = 0
		d.Lenses.OD.Family = ""
		d.Lenses.OD.Abbe = 0
		d.Lenses.OD.SpecificGravity = 0
		d.Lenses.OS.Index = 0
		d.Lenses.OS.Family = ""
		d.Lenses.OS.Abbe = 0
		d.Lenses.OS.SpecificGravity = 0
	case "custom":
		d.Lenses.Material = ""
		// Hoist shared custom material fields to Lenses when OD and OS match.
		if math.Abs(d.Lenses.OD.Index-d.Lenses.OS.Index) < 1e-6 &&
			d.Lenses.OD.Family == d.Lenses.OS.Family &&
			d.Lenses.OD.Abbe == d.Lenses.OS.Abbe &&
			math.Abs(d.Lenses.OD.SpecificGravity-d.Lenses.OS.SpecificGravity) < 1e-6 {
			d.Lenses.Index = d.Lenses.OD.Index
			d.Lenses.Family = d.Lenses.OD.Family
			d.Lenses.Abbe = d.Lenses.OD.Abbe
			d.Lenses.SpecificGravity = d.Lenses.OD.SpecificGravity
			d.Lenses.OD.Index = 0
			d.Lenses.OD.Family = ""
			d.Lenses.OD.Abbe = 0
			d.Lenses.OD.SpecificGravity = 0
			d.Lenses.OS.Index = 0
			d.Lenses.OS.Family = ""
			d.Lenses.OS.Abbe = 0
			d.Lenses.OS.SpecificGravity = 0
		}
	default:
		d.Lenses.Material = preset
		d.Lenses.OD.Index = 0
		d.Lenses.OD.Family = ""
		d.Lenses.OD.Abbe = 0
		d.Lenses.OD.SpecificGravity = 0
		d.Lenses.OS.Index = 0
		d.Lenses.OS.Family = ""
		d.Lenses.OS.Abbe = 0
		d.Lenses.OS.SpecificGravity = 0
	}

	if math.Abs(d.Frame.A-optics.DefaultFrameA) < 1e-6 &&
		math.Abs(d.Frame.B-optics.DefaultFrameB) < 1e-6 &&
		math.Abs(d.Frame.DBL-optics.DefaultFrameDBL) < 1e-6 {
		d.Frame.A = 0
		d.Frame.B = 0
		d.Frame.DBL = 0
	}
	if d.Frame.Shape == defaultFrameShape || d.Frame.Shape == "trace" {
		d.Frame.Shape = ""
	}

	return d
}

// Data resolves all inputs (evaluating 0=auto fields for PD, OCH, BaseCurve, SG, and Thickness)
// into a complete normalized optics.Data ready for rendering.
func (cp *ControlPanel) Data() optics.Data {
	return optics.NormalizeData(cp.RawData(), optics.NormalizeOptions{
		Design:         cp.SelectedDesign(),
		OverrideDesign: true,
		ODFamily:       cp.odMaterial,
		OSFamily:       cp.osMaterial,
		Trace:          cp.ActiveContourTrace(),
	})
}

// SetDefaultState resets all inputs to clean defaults:
// plano Rx, Auto material (#1 recommended), Bi-Aspheric design, 52x40 DBL 18, 0.6 mm bevel, 0=Auto for BC/CT/PD/OCH.
func (cp *ControlPanel) SetDefaultState() {
	cp.suppressChanges = true
	defer func() { cp.suppressChanges = false }()

	cp.odSph.SetValue(0)
	cp.odCyl.SetValue(0)
	cp.odAxis.SetValue(0)
	cp.odPD.SetValue(0)
	cp.odOCH.SetValue(0)
	cp.odRotCtr.SetValue(optics.DefaultCenterOfRotationMM)

	cp.osSph.SetValue(0)
	cp.osCyl.SetValue(0)
	cp.osAxis.SetValue(0)
	cp.osPD.SetValue(0)
	cp.osOCH.SetValue(0)
	cp.osRotCtr.SetValue(optics.DefaultCenterOfRotationMM)

	cp.presetCombo.SetItemText(0, "Auto")
	cp.selectMaterialPresetCombo("auto")
	cp.odMaterial = optics.FamilyPlastic
	cp.osMaterial = optics.FamilyPlastic
	cp.odIndex.SetValue(optics.DefaultRefractiveIndex)
	cp.osIndex.SetValue(optics.DefaultRefractiveIndex)
	cp.odAbbe.SetValue(optics.DefaultAbbeNumber)
	cp.osAbbe.SetValue(optics.DefaultAbbeNumber)
	defSG := optics.EstimateSpecificGravity(optics.DefaultRefractiveIndex, optics.FamilyPlastic)
	cp.odSG.SetValue(defSG)
	cp.osSG.SetValue(defSG)
	cp.updateMaterialDerivedLockState()

	cp.designCombo.SetCurrentIndex(designBiAspherical)
	cp.odBase.SetValue(0)
	cp.osBase.SetValue(0)
	cp.odThick.SetValue(0)
	cp.osThick.SetValue(0)
	cp.chamferSpin.SetValue(optics.DefaultChamferMM)
	cp.bevelSpin.SetValue(optics.DefaultBevelDepthMM)

	cp.frameA.SetValue(optics.DefaultFrameA)
	cp.frameB.SetValue(optics.DefaultFrameB)
	cp.frameDBL.SetValue(optics.DefaultFrameDBL)
	cp.lastNonTraceShape = defaultFrameShape
	cp.selectShapeCombo(defaultFrameShape)
	cp.updateTraceShapeAvailability()
}

// SetData populates all input widgets from a loaded optics.Data file without triggering renders.
// When no material is specified in the config (or Material is "auto"), "Auto" is selected by default.
func (cp *ControlPanel) SetData(d optics.Data) {
	cp.suppressChanges = true
	defer func() { cp.suppressChanges = false }()

	cp.odSph.SetValue(d.RX.OD.Sph)
	cp.odCyl.SetValue(d.RX.OD.Cyl)
	cp.odAxis.SetValue(d.RX.OD.Axis)
	cp.osSph.SetValue(d.RX.OS.Sph)
	cp.osCyl.SetValue(d.RX.OS.Cyl)
	cp.osAxis.SetValue(d.RX.OS.Axis)

	cp.odRotCtr.SetValue(d.Fit.EffectiveODCenterOfRotation())
	cp.osRotCtr.SetValue(d.Fit.EffectiveOSCenterOfRotation())

	odEff := d.Lenses.EffectiveEye(true)
	osEff := d.Lenses.EffectiveEye(false)

	if d.Lenses.IsAutoMaterial() {
		cp.presetCombo.SetItemText(0, "Auto")
		cp.selectMaterialPresetCombo("auto")
		cp.odMaterial = optics.FamilyPlastic
		cp.osMaterial = optics.FamilyPlastic
		cp.odIndex.SetValue(optics.DefaultRefractiveIndex)
		cp.osIndex.SetValue(optics.DefaultRefractiveIndex)
		cp.odAbbe.SetValue(optics.DefaultAbbeNumber)
		cp.osAbbe.SetValue(optics.DefaultAbbeNumber)
		defSG := optics.EstimateSpecificGravity(optics.DefaultRefractiveIndex, optics.FamilyPlastic)
		cp.odSG.SetValue(defSG)
		cp.osSG.SetValue(defSG)
	} else {
		odIdx := odEff.Index
		if odIdx <= 1.0 {
			odIdx = optics.DefaultRefractiveIndex
		}
		osIdx := osEff.Index
		if osIdx <= 1.0 {
			osIdx = odIdx
		}

		odPreset, odMat := matchMaterialPreset(odIdx, odEff.Family)
		osPreset, osMat := matchMaterialPreset(osIdx, osEff.Family)

		odStdAbbe := optics.EstimateAbbe(odIdx, odMat)
		osStdAbbe := optics.EstimateAbbe(osIdx, osMat)
		odStdSG := optics.EstimateSpecificGravity(odIdx, odMat)
		osStdSG := optics.EstimateSpecificGravity(osIdx, osMat)

		odAbbe := odEff.Abbe
		if odAbbe <= 0 {
			odAbbe = odStdAbbe
		}
		osAbbe := osEff.Abbe
		if osAbbe <= 0 {
			osAbbe = osStdAbbe
		}

		odSG := odEff.SpecificGravity
		osSG := osEff.SpecificGravity

		customAbbe := (odEff.Abbe > 0 && odEff.Abbe != odStdAbbe) ||
			(osEff.Abbe > 0 && osEff.Abbe != osStdAbbe)
		customSG := (odSG > 0 && math.Abs(odSG-odStdSG) > 0.01) ||
			(osSG > 0 && math.Abs(osSG-osStdSG) > 0.01)

		presetVal := odPreset
		if odPreset == "custom" || osPreset != odPreset || math.Abs(odIdx-osIdx) >= 0.002 || customAbbe || customSG {
			presetVal = "custom"
		}

		cp.odMaterial = odMat
		cp.osMaterial = osMat
		cp.selectMaterialPresetCombo(presetVal)
		cp.odIndex.SetValue(odIdx)
		cp.osIndex.SetValue(osIdx)
		cp.odAbbe.SetValue(odAbbe)
		cp.osAbbe.SetValue(osAbbe)
		if presetVal != "custom" {
			cp.odSG.SetValue(odStdSG)
			cp.osSG.SetValue(osStdSG)
		} else {
			cp.odSG.SetValue(odSG)
			cp.osSG.SetValue(osSG)
		}
	}
	cp.updateMaterialDerivedLockState()

	cp.odBase.SetValue(odEff.BaseCurve)
	cp.osBase.SetValue(osEff.BaseCurve)
	cp.odThick.SetValue(odEff.CenterThickness)
	cp.osThick.SetValue(osEff.CenterThickness)

	switch d.Lenses.Design {
	case optics.DesignAspheric:
		cp.designCombo.SetCurrentIndex(designAspherical)
	case optics.DesignSpherical:
		cp.designCombo.SetCurrentIndex(designSpherical)
	default:
		cp.designCombo.SetCurrentIndex(designBiAspherical)
	}

	cp.chamferSpin.SetValue(d.Lenses.EffectiveChamfer())
	cp.bevelSpin.SetValue(d.Lenses.EffectiveBevelDepth())

	frameA, frameB, frameDBL := d.Frame.A, d.Frame.B, d.Frame.DBL
	if frameA <= 0 {
		frameA = optics.DefaultFrameA
	}
	if frameB <= 0 {
		frameB = optics.DefaultFrameB
	}
	if frameDBL <= 0 {
		frameDBL = optics.DefaultFrameDBL
	}
	cp.frameA.SetValue(frameA)
	cp.frameB.SetValue(frameB)
	cp.frameDBL.SetValue(frameDBL)
	shapeVal := normalizeShapeValue(d.Frame.Shape)
	if shapeVal == "trace" && !cp.HasLoadedTrace() {
		shapeVal = defaultFrameShape
	}
	if shapeVal != "trace" {
		cp.lastNonTraceShape = shapeVal
	}
	cp.selectShapeCombo(shapeVal)
	cp.updateTraceShapeAvailability()

	cp.odPD.SetValue(d.Fit.OD.PD)
	cp.osPD.SetValue(d.Fit.OS.PD)

	if d.Fit.OD.OCH > 0 {
		cp.odOCH.SetValue(d.Fit.OD.OCH)
	} else {
		cp.odOCH.SetValue(0)
	}
	if d.Fit.OS.OCH > 0 {
		cp.osOCH.SetValue(d.Fit.OS.OCH)
	} else {
		cp.osOCH.SetValue(0)
	}
}

// SetFrame updates Frame A/B/DBL.
func (cp *ControlPanel) SetFrame(f optics.Frame) {
	cp.suppressChanges = true
	if f.A > 0 {
		cp.frameA.SetValue(math.Round(f.A*10.0) / 10.0)
	}
	if f.B > 0 {
		cp.frameB.SetValue(math.Round(f.B*10.0) / 10.0)
	}
	if f.DBL >= 0 {
		cp.frameDBL.SetValue(math.Round(f.DBL*10.0) / 10.0)
	}
	cp.suppressChanges = false
}

// SetBevelDepth updates the bevel depth spinbox without emitting a change signal.
func (cp *ControlPanel) SetBevelDepth(bevelMM float64) {
	cp.suppressChanges = true
	cp.bevelSpin.SetValue(math.Max(0, bevelMM))
	cp.suppressChanges = false
}

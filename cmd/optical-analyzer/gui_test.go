package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"

	qt "github.com/mappu/miqt/qt6"

	"jsouthworth.net/go/optical-analyzer/contour"
	"jsouthworth.net/go/optical-analyzer/optics"
)

func TestContrastTextColor(t *testing.T) {
	if got := contrastTextColor(color.RGBA{0xff, 0xff, 0xff, 0xff}); got != "#000000" {
		t.Errorf("white background: got %s, want #000000", got)
	}
	if got := contrastTextColor(color.RGBA{0xff, 0xf3, 0xe0, 0xff}); got != "#000000" {
		t.Errorf("light cream background: got %s, want #000000", got)
	}
	if got := contrastTextColor(color.RGBA{0x00, 0x00, 0x00, 0xff}); got != "#ffffff" {
		t.Errorf("black background: got %s, want #ffffff", got)
	}
	if got := contrastTextColor(color.RGBA{0x49, 0x0b, 0x0b, 0xff}); got != "#ffffff" {
		t.Errorf("dark red background: got %s, want #ffffff", got)
	}
}

func assertMaterialDerivedEnabled(t *testing.T, cp *ControlPanel, wantEnabled bool, label string) {
	t.Helper()
	if cp.odIndex.IsEnabled() != wantEnabled || cp.osIndex.IsEnabled() != wantEnabled ||
		cp.odAbbe.IsEnabled() != wantEnabled || cp.osAbbe.IsEnabled() != wantEnabled ||
		cp.odSG.IsEnabled() != wantEnabled || cp.osSG.IsEnabled() != wantEnabled {
		t.Errorf("%s: material-derived controls enabled=(idx %v/%v abbe %v/%v sg %v/%v), want all %v",
			label,
			cp.odIndex.IsEnabled(), cp.osIndex.IsEnabled(),
			cp.odAbbe.IsEnabled(), cp.osAbbe.IsEnabled(),
			cp.odSG.IsEnabled(), cp.osSG.IsEnabled(),
			wantEnabled)
	}
}

func TestOpticalAnalyzerGUIAllFeatures(t *testing.T) {
	if os.Getenv("QT_QPA_PLATFORM") == "" {
		os.Setenv("QT_QPA_PLATFORM", "offscreen")
	}

	qt.NewQApplication([]string{"optical-analyzer-test"})

	app := newApp()
	defer app.window.Delete()

	// 1. Default state selects "Auto" material, greys out material-derived fields, and populates #1 recommendation
	app.controls.SetDefaultState()
	app.recomputeAll()
	if !app.controls.AutoMaterial() || app.controls.SelectedMaterialPresetValue() != "auto" {
		t.Fatalf("expected default material preset to be 'auto', got %q", app.controls.SelectedMaterialPresetValue())
	}
	assertMaterialDerivedEnabled(t, app.controls, false, "default Auto material")
	if len(app.lastRecommendation.Recommendations) == 0 {
		t.Fatalf("expected default recommendations to be computed")
	}
	defTop := app.lastRecommendation.Recommendations[0]
	defData := app.controls.Data()
	if defData.RX.OD.Sph != 0 || defData.Frame.A != 52 || defData.Frame.B != 40 || defData.Frame.DBL != 18 {
		t.Fatalf("unexpected default frame/rx state: %+v", defData)
	}
	if math.Abs(defData.Lenses.OD.Index-defTop.Index) > 1e-6 {
		t.Fatalf("default Auto did not populate #1 material index: got %v, want %v (%s)", defData.Lenses.OD.Index, defTop.Index, defTop.Name)
	}
	if defData.Fit.OD.PD != 35.0 || defData.Fit.OD.OCH != 20.0 {
		t.Fatalf("expected auto PD=35.0 OCH=20.0, got PD=%.1f OCH=%.1f", defData.Fit.OD.PD, defData.Fit.OD.OCH)
	}
	if math.Abs(defData.Lenses.EffectiveChamfer()-0.25) > 1e-6 {
		t.Fatalf("expected default chamfer = 0.25 mm, got %v", defData.Lenses.EffectiveChamfer())
	}
	if app.controls.SelectedShape() != "oval" {
		t.Fatalf("expected default Frame Shape = 'oval', got %q", app.controls.SelectedShape())
	}
	if !app.showTraceCheck.IsEnabled() || !app.dimOutsideCheck.IsEnabled() || !app.dimOutsideCheck.IsChecked() {
		t.Fatalf("expected Show Contour and Dim Outside checkboxes to be enabled, with Dim Outside checked by default")
	}
	if app.controls.clearTraceBtn.IsEnabled() {
		t.Fatalf("expected Clear Trace button to be disabled when no trace is loaded")
	}

	dir := t.TempDir()

	// Save defaults to disk -> verify default bevel_depth, chamfer, center_of_rotation, design, material, and frame are omitted
	defaultsDat := filepath.Join(dir, "defaults.json")
	app.saveFile(defaultsDat)
	rawDefaults, err := os.ReadFile(defaultsDat)
	if err != nil {
		t.Fatalf("read defaults.json: %v", err)
	}
	for _, forbidden := range []string{"bevel_depth", "chamfer", "center_of_rotation", "design", "frame", "lenses", "fit", "cyl", "axis"} {
		if bytes.Contains(rawDefaults, []byte(`"`+forbidden+`"`)) {
			t.Errorf("expected default field %q to be omitted from saved defaults.json:\n%s", forbidden, rawDefaults)
		}
	}

	// Explicit 0 bevel/chamfer IS preserved when saving and reloading
	app.controls.bevelSpin.SetValue(0)
	app.controls.chamferSpin.SetValue(0)
	rimlessDat := filepath.Join(dir, "rimless.json")
	app.saveFile(rimlessDat)
	rawRimless, err := os.ReadFile(rimlessDat)
	if err != nil {
		t.Fatalf("read rimless.json: %v", err)
	}
	if !bytes.Contains(rawRimless, []byte(`"bevel_depth": 0`)) || !bytes.Contains(rawRimless, []byte(`"chamfer": 0`)) {
		t.Fatalf("expected explicit 0 bevel_depth and chamfer to be preserved in saved file:\n%s", rawRimless)
	}
	app.controls.SetDefaultState()
	app.openFile(rimlessDat)
	if app.controls.bevelSpin.Value() != 0 || app.controls.chamferSpin.Value() != 0 {
		t.Fatalf("expected reloaded rimless.json to preserve 0 bevel/chamfer, got bevel=%v chamfer=%v",
			app.controls.bevelSpin.Value(), app.controls.chamferSpin.Value())
	}
	app.controls.SetDefaultState()
	app.recomputeAll()

	// 2. Loading a config with NO material specified defaults to "Auto" and greys out material-derived controls
	noMatPath := filepath.Join(dir, "no_material.json")
	noMatCfg := optics.Data{
		Version: optics.CurrentSchemaVersion,
		RX: optics.RX{
			OD: optics.Refraction{Sph: -6.00, Cyl: -1.00, Axis: 90},
			OS: optics.Refraction{Sph: -5.50, Cyl: -0.75, Axis: 85},
		},
		Frame: optics.Frame{A: 50, B: 38, DBL: 18},
		Fit: optics.Fit{
			OD: optics.EyeFit{PD: 30.5},
			OS: optics.EyeFit{PD: 30.0},
		},
	}
	if err := optics.SaveData(noMatPath, noMatCfg); err != nil {
		t.Fatalf("SaveData(no_material): %v", err)
	}
	app.openFile(noMatPath)
	if !app.controls.AutoMaterial() {
		t.Fatalf("expected config without material to default to Auto, got %q", app.controls.SelectedMaterialPresetValue())
	}
	assertMaterialDerivedEnabled(t, app.controls, false, "config without material (Auto)")
	topNoMat := app.lastRecommendation.Recommendations[0]
	if math.Abs(app.controls.odIndex.Value()-topNoMat.Index) > 1e-6 || app.controls.odMaterial != topNoMat.Family {
		t.Fatalf("Auto did not populate #1 for -6D config: got index=%v fam=%q, want %v %q",
			app.controls.odIndex.Value(), app.controls.odMaterial, topNoMat.Index, topNoMat.Family)
	}

	// 3. Loading a config with an explicit named preset selects that preset and greys out Index/Abbe/SG
	samplePath := filepath.Join(dir, "sample.json")
	chamfer := 0.25
	bevel := 0.6
	sample := optics.Data{
		Version: optics.CurrentSchemaVersion,
		RX: optics.RX{
			OD: optics.Refraction{Sph: -5.25, Cyl: -1.00, Axis: 95},
			OS: optics.Refraction{Sph: -4.75, Cyl: -0.50, Axis: 80},
		},
		Lenses: optics.LensPair{
			Material:   "1.60-plastic",
			Design:     optics.DesignBiAspheric,
			Chamfer:    &chamfer,
			BevelDepth: &bevel,
		},
		Frame: optics.Frame{A: 50, B: 38, DBL: 18},
		Fit: optics.Fit{
			CenterOfRotation: 27.0,
			OD:               optics.EyeFit{PD: 30.5},
			OS:               optics.EyeFit{PD: 30.0},
		},
	}
	if err := optics.SaveData(samplePath, sample); err != nil {
		t.Fatalf("SaveData: %v", err)
	}

	app.openFile(samplePath)
	app.window.Show()
	qt.QCoreApplication_ProcessEvents()

	// Verify OD and OS inputs under Material & Optics are symmetric in width
	for _, pair := range []struct {
		name   string
		od, os int
	}{
		{"Index", app.controls.odIndex.Width(), app.controls.osIndex.Width()},
		{"BaseCurve", app.controls.odBase.Width(), app.controls.osBase.Width()},
		{"Thickness", app.controls.odThick.Width(), app.controls.osThick.Width()},
		{"Abbe", app.controls.odAbbe.Width(), app.controls.osAbbe.Width()},
		{"SG", app.controls.odSG.Width(), app.controls.osSG.Width()},
	} {
		if pair.od <= 0 || math.Abs(float64(pair.od-pair.os)) > 1 {
			t.Errorf("Material & Optics %s OD/OS widths not symmetric: OD=%dpx, OS=%dpx", pair.name, pair.od, pair.os)
		}
	}

	if app.controls.SelectedMaterialPresetValue() != "1.60-plastic" {
		t.Fatalf("expected 1.60-plastic preset, got %q", app.controls.SelectedMaterialPresetValue())
	}
	assertMaterialDerivedEnabled(t, app.controls, false, "named preset 1.60-plastic")
	if math.Abs(app.controls.odSG.Value()-1.30) > 1e-6 {
		t.Errorf("expected derived SG 1.30 for 1.60 plastic, got %v", app.controls.odSG.Value())
	}

	// Selecting "Custom" enables Index, Abbe, and SG; switching back to a preset greys them out again
	app.controls.selectMaterialPresetCombo("custom")
	qt.QCoreApplication_ProcessEvents()
	if !app.controls.IsCustomMaterial() {
		t.Fatalf("expected Custom material selection")
	}
	assertMaterialDerivedEnabled(t, app.controls, true, "Custom material")

	app.controls.selectMaterialPresetCombo("1.60-plastic")
	qt.QCoreApplication_ProcessEvents()
	assertMaterialDerivedEnabled(t, app.controls, false, "returned to named preset")

	got := app.controls.Data()
	if got.RX.OD.Sph != -5.25 || got.RX.OS.Axis != 80 || got.Frame.A != 50 ||
		math.Abs(got.Fit.EffectiveODCenterOfRotation()-27.0) > 1e-6 {
		t.Fatalf("controls did not reflect loaded data: %+v", got)
	}

	// Visit all 4 tabs: Lens Thickness, Chromatic Aberration, Prismatic Diff, Mag Ellipse
	if app.tabs.CurrentIndex() != tabThickness || app.tabs.TabText(0) != "Lens Thickness" {
		t.Fatalf("expected 'Lens Thickness' to be the first/initial tab, got idx=%d title=%q", app.tabs.CurrentIndex(), app.tabs.TabText(0))
	}
	for i := 0; i < tabCount; i++ {
		app.tabs.SetCurrentIndex(i)
		qt.QCoreApplication_ProcessEvents()
		if app.dirty[i] {
			t.Errorf("tab %d remained dirty after switching", i)
		}
		for _, v := range app.activeViewers() {
			if v.Image() == nil {
				t.Errorf("tab %d has nil viewer image", i)
			}
			v.ZoomIn()
			v.ZoomOut()
			v.ActualSize()
			v.FitToWindow()
		}
	}

	app.tabs.SetCurrentIndex(tabThickness)
	qt.QCoreApplication_ProcessEvents()
	if app.thicknessTab.table.RowCount() != 2 {
		t.Fatalf("Lens Thickness table rows = %d, want 2 (OD + OS for active Shape & Design)", app.thicknessTab.table.RowCount())
	}
	if gotShape := app.thicknessTab.table.Item(0, 0).Text(); gotShape != "Oval (n=2.0)" {
		t.Fatalf("expected Lens Thickness shape 'Oval (n=2.0)', got %q", gotShape)
	}
	if gotDesign := app.thicknessTab.table.Item(0, 1).Text(); gotDesign != "Bi-Aspheric" {
		t.Fatalf("expected Lens Thickness design 'Bi-Aspheric', got %q", gotDesign)
	}
	if head := app.summary.headLabel.Text(); !bytes.Contains([]byte(head), []byte("Max Diff")) || !bytes.Contains([]byte(head), []byte("Pair ")) || !bytes.Contains([]byte(head), []byte("Δ)")) {
		t.Fatalf("expected unified Summary panel header to include optical, 23° prismatic diff, & weight metrics, got %q", head)
	}
	if edge := app.summary.odEdge.Text(); edge == "" || edge == "—" {
		t.Fatalf("expected unified Summary panel OD edge readout, got %q", edge)
	}
	if mag := app.summary.odMag.Text(); !bytes.Contains([]byte(mag), []byte("%")) {
		t.Fatalf("expected unified Summary panel OD magnification readout, got %q", mag)
	}

	// Verify changing Design in Material & Optics updates Lens Thickness tab
	app.controls.designCombo.SetCurrentIndex(designSpherical)
	app.recomputeAll()
	qt.QCoreApplication_ProcessEvents()
	if gotDesign := app.thicknessTab.table.Item(0, 1).Text(); gotDesign != "Spherical" {
		t.Fatalf("expected Lens Thickness design 'Spherical' after changing Material & Optics design, got %q", gotDesign)
	}
	app.controls.designCombo.SetCurrentIndex(designBiAspherical)
	app.recomputeAll()
	qt.QCoreApplication_ProcessEvents()

	// Verify changing Frame Shape updates Chromatic Aberration contour, Lens Thickness, and Recommendation
	app.tabs.SetCurrentIndex(tabChromatic)
	qt.QCoreApplication_ProcessEvents()
	app.controls.selectShapeCombo("oval")
	app.recomputeAll()
	qt.QCoreApplication_ProcessEvents()
	ovalImg := app.caOD.Image()
	if app.lastRecommendation.Shape != "Oval (n=2.0)" {
		t.Fatalf("expected Recommendation shape 'Oval (n=2.0)', got %q", app.lastRecommendation.Shape)
	}

	app.controls.selectShapeCombo("box")
	app.recomputeAll()
	qt.QCoreApplication_ProcessEvents()
	boxImg := app.caOD.Image()
	if imagesEqualRGBA(ovalImg, boxImg) {
		t.Fatalf("expected Chromatic Aberration OD image to change when switching Frame Shape from Oval to Sharp Box")
	}
	app.tabs.SetCurrentIndex(tabThickness)
	qt.QCoreApplication_ProcessEvents()
	if gotShape := app.thicknessTab.table.Item(0, 0).Text(); gotShape != "Sharp Box (n=8.0)" {
		t.Fatalf("expected Lens Thickness shape 'Sharp Box (n=8.0)', got %q", gotShape)
	}

	// 4. Test Frame Tracer pop-out window: load synthetic photo, preview debug overlay, save OMA .trc, and use as-is
	photoPath := filepath.Join(dir, "frame.png")
	writeSyntheticFramePhoto(t, photoPath)

	app.openTracerWindow()
	qt.QCoreApplication_ProcessEvents()
	if !app.tracerWin.Dialog.IsVisible() {
		t.Fatalf("Frame Tracer pop-out window did not become visible")
	}
	if err := app.tracerWin.LoadPhoto(photoPath); err != nil {
		t.Fatalf("tracerWin.LoadPhoto: %v", err)
	}
	if app.tracerWin.viewer.Image() == nil {
		t.Fatalf("Frame Tracer debug overlay preview image is nil")
	}
	if !app.tracerWin.saveOMABtn.IsEnabled() || !app.tracerWin.useTraceBtn.IsEnabled() {
		t.Fatalf("expected Save OMA and Use Trace buttons to be enabled after tracing")
	}

	savedTRC := filepath.Join(dir, "traced.trc")
	if err := app.tracerWin.SaveOMATrace(savedTRC); err != nil {
		t.Fatalf("SaveOMATrace: %v", err)
	}
	loadedTrace, err := contour.ReadFromPath(savedTRC, "", nil, 0, 0, 0)
	if err != nil || loadedTrace.OD.AreaMM2() <= 0 {
		t.Fatalf("saved OMA trace failed to reload: %v", err)
	}

	app.tracerWin.useCurrentTrace()
	qt.QCoreApplication_ProcessEvents()
	if app.trace == nil {
		t.Fatalf("Use Trace did not apply trace to main app")
	}
	if app.controls.SelectedShape() != "trace" {
		t.Errorf("expected Frame Shape = 'trace' after loading trace, got %q", app.controls.SelectedShape())
	}
	if !app.controls.clearTraceBtn.IsEnabled() {
		t.Errorf("expected Clear Trace button in Frame card to be enabled after loading trace")
	}
	app.tabs.SetCurrentIndex(tabThickness)
	qt.QCoreApplication_ProcessEvents()
	if app.thicknessTab.table.RowCount() != 2 {
		t.Errorf("expected 2 rows (OD/OS) for traced shape & active design, got %d", app.thicknessTab.table.RowCount())
	}
	app.tracerWin.Dialog.Hide()

	// 5. Test Lens Recommendation pop-out window & "Auto" material dropdown option
	app.openRecommendWindow()
	qt.QCoreApplication_ProcessEvents()
	if !app.recommendWin.Dialog.IsVisible() {
		t.Fatalf("Lens Recommendation pop-out window did not become visible")
	}
	if app.recommendWin.table.RowCount() != 12 {
		t.Fatalf("Recommendation table rows = %d, want 12", app.recommendWin.table.RowCount())
	}
	topBefore := app.lastRecommendation.Recommendations[0]

	// Switch Materials dropdown to "Auto"
	app.controls.selectMaterialPresetCombo("auto")
	app.recomputeAll()
	qt.QCoreApplication_ProcessEvents()
	if !app.controls.AutoMaterial() {
		t.Fatalf("expected Auto material to be selected")
	}
	assertMaterialDerivedEnabled(t, app.controls, false, "Auto material selected from dropdown")
	if math.Abs(app.controls.odIndex.Value()-topBefore.Index) > 1e-6 || app.controls.odMaterial != topBefore.Family {
		t.Errorf("Auto dropdown did not apply #1 material: got index=%v fam=%q, want index=%v fam=%q",
			app.controls.odIndex.Value(), app.controls.odMaterial, topBefore.Index, topBefore.Family)
	}

	// Move Thickness to #1 priority while "Auto" is selected -> material should update automatically!
	app.recommendWin.MovePriority(1, -1)
	app.recomputeAll()
	topAfter := app.lastRecommendation.Recommendations[0]
	if math.Abs(app.controls.odIndex.Value()-topAfter.Index) > 1e-6 || app.controls.odMaterial != topAfter.Family {
		t.Errorf("Auto did not update after priority change: got index=%v fam=%q, want %v %q",
			app.controls.odIndex.Value(), app.controls.odMaterial, topAfter.Index, topAfter.Family)
	}
	if !app.controls.AutoMaterial() {
		t.Errorf("expected dropdown to remain on Auto after recompute")
	}

	// Save & reload normalized .json while on Auto -> omits default bevel_depth/chamfer/design/center_of_rotation/index and reloads as Auto
	outDat := filepath.Join(dir, "roundtrip.json")
	app.saveFile(outDat)
	rawSaved, err := os.ReadFile(outDat)
	if err != nil {
		t.Fatalf("read saved .json: %v", err)
	}
	for _, forbidden := range []string{"index", "material", "bevel_depth", "chamfer", "center_of_rotation", "design"} {
		if bytes.Contains(rawSaved, []byte(`"`+forbidden+`"`)) {
			t.Fatalf("expected default/derived %q to be omitted when saving:\n%s", forbidden, rawSaved)
		}
	}
	if reloaded, err := optics.LoadData(outDat); err != nil {
		t.Fatalf("reload roundtrip.json: %v", err)
	} else {
		if !reloaded.Lenses.IsAutoMaterial() || reloaded.Lenses.EffectiveBevelDepth() != optics.DefaultBevelDepthMM ||
			reloaded.Lenses.EffectiveChamfer() != optics.DefaultChamferMM {
			t.Fatalf("unexpected reloaded roundtrip defaults: %+v", reloaded.Lenses)
		}
	}

	// Manually applying a specific material from Recommendation window switches dropdown to that named preset
	// and saving writes a single compact "material": "<key>" instead of repeating per-eye index/family.
	manualPick := app.lastRecommendation.Recommendations[2]
	app.applyRecommendedMaterial(manualPick)
	if app.controls.AutoMaterial() {
		t.Errorf("manual apply should switch dropdown off Auto")
	}
	assertMaterialDerivedEnabled(t, app.controls, false, "manual apply named preset")
	if math.Abs(app.controls.odIndex.Value()-manualPick.Index) > 1e-6 {
		t.Errorf("manual apply index = %v, want %v", app.controls.odIndex.Value(), manualPick.Index)
	}
	manualPath := filepath.Join(dir, "manual_preset.json")
	app.saveFile(manualPath)
	rawManual, err := os.ReadFile(manualPath)
	if err != nil {
		t.Fatalf("read manual_preset.json: %v", err)
	}
	if !bytes.Contains(rawManual, []byte(`"material": "`)) || bytes.Contains(rawManual, []byte(`"index"`)) {
		t.Errorf("expected compact preset material key without redundant index:\n%s", rawManual)
	}
	app.recommendWin.Dialog.Hide()

	// 6. Clear trace restores previous non-trace shape; reloading OMA .trc sets Frame Shape back to "trace"
	app.clearTrace()
	if app.trace != nil {
		t.Fatalf("expected trace to be cleared")
	}
	if app.controls.SelectedShape() != "box" {
		t.Fatalf("expected clearing trace to restore previous non-trace shape 'box', got %q", app.controls.SelectedShape())
	}
	if app.controls.clearTraceBtn.IsEnabled() {
		t.Fatalf("expected Clear Trace button in Frame card to be disabled after clearing trace")
	}
	app.loadTraceFile(savedTRC)
	if app.trace == nil {
		t.Fatalf("expected loadTraceFile to load trace")
	}
	if app.controls.SelectedShape() != "trace" {
		t.Fatalf("expected loadTraceFile to set Frame Shape = 'trace', got %q", app.controls.SelectedShape())
	}

	// 6b. Explicit non-Auto OCH (including 20.0 mm == DefaultFrameB/2 with a trace whose B != 40,
	// and uncommitted typed spinbox text) must be preserved across save & reload, and trace path is saved relative.
	app.controls.odOCH.SetValue(20.0)
	app.controls.osOCH.SetValue(20.0)
	ochTraceDat := filepath.Join(dir, "och_trace.json")
	app.saveFile(ochTraceDat)
	rawOCHTrace, err := os.ReadFile(ochTraceDat)
	if err != nil {
		t.Fatalf("read och_trace.json: %v", err)
	}
	if !bytes.Contains(rawOCHTrace, []byte(`"och": 20`)) {
		t.Fatalf("expected explicit och: 20 in saved och_trace.json:\n%s", rawOCHTrace)
	}
	if !bytes.Contains(rawOCHTrace, []byte(`"trace": "traced.trc"`)) {
		t.Fatalf("expected relative trace path \"traced.trc\" in saved och_trace.json:\n%s", rawOCHTrace)
	}
	app.controls.SetDefaultState()
	app.openFile(ochTraceDat)
	if math.Abs(app.controls.odOCH.Value()-20.0) > 1e-6 || math.Abs(app.controls.osOCH.Value()-20.0) > 1e-6 {
		t.Fatalf("expected reloaded och_trace.dat to preserve OCH=20.0 (not reset to Auto), got OD=%v OS=%v",
			app.controls.odOCH.Value(), app.controls.osOCH.Value())
	}

	// Uncommitted text typed into OC Ht spinbox should be committed on Save
	app.controls.odOCH.LineEdit().SetText("22.5")
	app.controls.osOCH.LineEdit().SetText("21.5")
	ochTypedDat := filepath.Join(dir, "och_typed.dat")
	app.saveFile(ochTypedDat)
	app.controls.SetDefaultState()
	app.openFile(ochTypedDat)
	if math.Abs(app.controls.odOCH.Value()-22.5) > 1e-6 || math.Abs(app.controls.osOCH.Value()-21.5) > 1e-6 {
		t.Fatalf("expected uncommitted typed OCH (22.5/21.5) to be saved & reloaded, got OD=%v OS=%v",
			app.controls.odOCH.Value(), app.controls.osOCH.Value())
	}

	// 7. Export thickness report to CSV & verify non-empty
	csvPath := filepath.Join(dir, "report.csv")
	if err := app.thicknessTab.ExportReport(csvPath); err != nil {
		t.Fatalf("ExportReport(csv): %v", err)
	}
	if st, err := os.Stat(csvPath); err != nil || st.Size() == 0 {
		t.Fatalf("exported CSV missing or empty: %v", err)
	}

	if shotDir := os.Getenv("OPTICAL_GUI_SCREENSHOT_DIR"); shotDir != "" {
		app.controls.selectMaterialPresetCombo("auto")
		app.recomputeAll()
		for i := 0; i < tabCount; i++ {
			app.tabs.SetCurrentIndex(i)
			qt.QCoreApplication_ProcessEvents()
			pm := app.window.Grab()
			pm.Save(filepath.Join(shotDir, fmt.Sprintf("tab%d.png", i)))
		}
		app.openRecommendWindow()
		qt.QCoreApplication_ProcessEvents()
		app.recommendWin.Dialog.Grab().Save(filepath.Join(shotDir, "popout_recommend.png"))
		app.recommendWin.Dialog.Hide()
	}
}

func writeSyntheticFramePhoto(t *testing.T, path string) {
	t.Helper()
	white := color.NRGBA{255, 255, 255, 255}
	dark := color.NRGBA{20, 20, 20, 255}
	img := image.NewNRGBA(image.Rect(0, 0, 400, 200))
	for y := 0; y < 200; y++ {
		for x := 0; x < 400; x++ {
			img.SetNRGBA(x, y, white)
			lx, ly := float64(x)-200, float64(y)-100
			odX, osX := lx+70, lx-70
			inOD := (odX/50)*(odX/50)+(ly/40)*(ly/40) <= 1
			inOS := (osX/50)*(osX/50)+(ly/40)*(ly/40) <= 1
			outOD := (odX/58)*(odX/58)+(ly/48)*(ly/48) <= 1
			outOS := (osX/58)*(osX/58)+(ly/48)*(ly/48) <= 1
			bridge := math.Abs(lx) <= 25 && ly >= -12 && ly <= -4
			if !inOD && !inOS && (outOD || outOS || bridge) {
				img.SetNRGBA(x, y, dark)
			}
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create synthetic photo: %v", err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatalf("encode synthetic photo: %v", err)
	}
}

func imagesEqualRGBA(a, b image.Image) bool {
	if a == nil || b == nil {
		return a == b
	}
	ra, rb := a.Bounds(), b.Bounds()
	if ra != rb {
		return false
	}
	for y := ra.Min.Y; y < ra.Max.Y; y++ {
		for x := ra.Min.X; x < ra.Max.X; x++ {
			r1, g1, b1, a1 := a.At(x, y).RGBA()
			r2, g2, b2, a2 := b.At(x, y).RGBA()
			if r1 != r2 || g1 != g2 || b1 != b2 || a1 != a2 {
				return false
			}
		}
	}
	return true
}

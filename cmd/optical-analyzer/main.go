package main

import (
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"

	qt "github.com/mappu/miqt/qt6"

	"jsouthworth.net/go/optical-analyzer/contour"
	"jsouthworth.net/go/optical-analyzer/dsm"
	"jsouthworth.net/go/optical-analyzer/internal/version"
	"jsouthworth.net/go/optical-analyzer/lens"
	"jsouthworth.net/go/optical-analyzer/optics"
)

const (
	tabThickness     = 0
	tabChromatic     = 1
	tabPrismaticDiff = 2
	tabMagEllipse    = 3
	tabCount         = 4
)

type App struct {
	window      *qt.QMainWindow
	statusBar   *qt.QStatusBar
	controls    *ControlPanel
	summary     *SummaryPanel
	tabs        *qt.QTabWidget
	renderTimer *qt.QTimer

	// Viewers for DSM visualizations
	pdiffViewer *ImageViewer
	caOD        *ImageViewer
	caOS        *ImageViewer
	magViewer   *ImageViewer

	// Lens Thickness tab
	thicknessTab *ThicknessTab

	// Pop-out windows
	tracerWin    *FrameTracerWindow
	recommendWin *RecommendWindow

	// Active trace state (shared between DSM overlays and Lens Thickness)
	trace           *contour.Trace
	traceOpts       dsm.TraceOverlayOptions
	showTraceCheck  *qt.QCheckBox
	showBoxCheck    *qt.QCheckBox
	dimOutsideCheck *qt.QCheckBox

	lastRecommendation lens.RecommendationReport

	currentFile string
	lastDir     string
	dirty       [tabCount]bool
}

func main() {
	var dataFile string
	var traceFile string
	var screenshotPath string
	var dimOutsideFlag bool
	var showVersion bool
	flag.StringVar(&dataFile, "data", "", "optional optical .json/.dat file to load on startup")
	flag.StringVar(&traceFile, "trace", "", "optional frame trace file (.trc/.oma, .json, .svg) to load on startup")
	flag.StringVar(&screenshotPath, "screenshot", "", "optional PNG path to render a screenshot and exit")
	flag.BoolVar(&dimOutsideFlag, "dim-outside", true, "dim chromatic aberration outside the frame contour")
	flag.BoolVar(&showVersion, "version", false, "print version and exit")
	flag.Parse()
	if showVersion {
		fmt.Printf("optical-analyzer %s\n", version.String())
		return
	}
	if dataFile == "" && flag.NArg() > 0 {
		dataFile = flag.Arg(0)
	}

	if screenshotPath != "" && os.Getenv("QT_QPA_PLATFORM") == "" && os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		_ = os.Setenv("QT_QPA_PLATFORM", "offscreen")
	}

	qt.NewQApplication(os.Args)
	qt.QCoreApplication_SetApplicationName("optical-analyzer")
	qt.QCoreApplication_SetApplicationVersion(version.String())
	qt.QGuiApplication_SetDesktopFileName("net.jsouthworth.OpticalAnalyzer")

	app := newApp()
	if dataFile != "" {
		app.openFile(dataFile)
	} else {
		app.controls.SetDefaultState()
		app.recomputeAll()
		app.updateTitle()
		app.statusBar.ShowMessage("Loaded default prescription — File → Open to load a saved optical data file")
	}
	if traceFile != "" {
		app.loadTraceFile(traceFile)
	}
	app.dimOutsideCheck.SetChecked(dimOutsideFlag)

	app.window.Show()
	if screenshotPath != "" {
		qt.QCoreApplication_ProcessEvents()
		pm := app.window.Grab()
		if !pm.Save(screenshotPath) {
			fmt.Fprintf(os.Stderr, "failed to save screenshot to %s\n", screenshotPath)
			os.Exit(1)
		}
		return
	}
	qt.QApplication_Exec()
}

func newApp() *App {
	app := &App{
		traceOpts: dsm.TraceOverlayOptions{
			ShowTrace:       true,
			ShowBoundingBox: true,
			DimOutsideTrace: true,
		},
	}

	app.window = qt.NewQMainWindow2()
	app.window.Resize(1340, 920)
	app.window.SetMinimumSize2(940, 620)

	app.statusBar = qt.NewQStatusBar(app.window.QWidget)
	app.window.SetStatusBar(app.statusBar)

	app.renderTimer = qt.NewQTimer2(app.window.QObject)
	app.renderTimer.SetSingleShot(true)
	app.renderTimer.OnTimeout(func() {
		app.recomputeAll()
	})

	app.controls = NewControlPanel(
		func() { app.renderTimer.Start(50) },
		func() { app.clearTrace() },
	)

	app.summary = NewSummaryPanel()

	app.tabs = app.buildTabs()
	app.tabs.OnCurrentChanged(func(idx int) {
		app.renderActiveTab()
	})

	app.tracerWin = NewFrameTracerWindow(
		app.window.QWidget,
		func() optics.Data { return app.controls.Data() },
		func(tr *contour.Trace) { app.applyContourTrace(tr, true) },
	)

	app.recommendWin = NewRecommendWindow(
		app.window.QWidget,
		func() { app.recomputeAll() },
		func(rec lens.MaterialRecommendation) { app.applyRecommendedMaterial(rec) },
	)

	rightPane := qt.NewQWidget2()
	rightLay := qt.NewQVBoxLayout(rightPane)
	rightLay.SetContentsMargins(4, 4, 4, 4)
	rightLay.SetSpacing(6)
	rightLay.AddWidget2(app.summary.Widget.QWidget, 0)
	rightLay.AddWidget2(app.tabs.QWidget, 1)

	splitter := qt.NewQSplitter3(qt.Horizontal)
	splitter.AddWidget(app.controls.ScrollArea.QWidget)
	splitter.AddWidget(rightPane)
	splitter.SetStretchFactor(0, 0)
	splitter.SetStretchFactor(1, 1)
	leftW := app.controls.ScrollArea.MinimumWidth()
	if leftW < 350 {
		leftW = 350
	}
	splitter.SetSizes([]int{leftW, 1340 - leftW})

	app.window.SetCentralWidget(splitter.QWidget)
	app.buildMenus()

	if cwd, err := os.Getwd(); err == nil {
		app.lastDir = cwd
	}

	return app
}

func (app *App) buildTabs() *qt.QTabWidget {
	tabs := qt.NewQTabWidget2()

	// Tab 0: Lens Thickness
	app.thicknessTab = NewThicknessTab()
	tabs.AddTab(app.thicknessTab.Widget, "Lens Thickness")

	// Tab 1: Chromatic Aberration (OD / OS)
	caPage := qt.NewQWidget2()
	caLayout := qt.NewQVBoxLayout(caPage)
	caLayout.SetContentsMargins(4, 4, 4, 4)

	caSplit, caOD, caOS := makeDualLensPane("OD (Right)", "OS (Left)")
	app.caOD = caOD
	app.caOS = caOS
	caLayout.AddWidget2(app.buildTraceBar(func() []*ImageViewer { return []*ImageViewer{app.caOD, app.caOS} }), 0)
	app.caOD.SetHoverCallbacks(
		func(x, y float64) {
			info := dsm.InspectPoint(app.controls.Data(), x, y)
			app.statusBar.ShowMessage(fmt.Sprintf(
				"OD Chromatic Spread: %.4f Δ (Prism: %.3f Δ)   |   Pos: (%+.1f, %+.1f) mm   |   r: %.1f mm (%.1f°)   |   Meridian: %.1f°",
				info.ODColorSpread, info.ODPrism, info.DXMM, info.DYMM, info.DistanceMM, info.ODGazeDeg, info.MeridianDeg,
			))
		},
		func() { app.statusBar.ClearMessage() },
	)
	app.caOS.SetHoverCallbacks(
		func(x, y float64) {
			info := dsm.InspectPoint(app.controls.Data(), x, y)
			app.statusBar.ShowMessage(fmt.Sprintf(
				"OS Chromatic Spread: %.4f Δ (Prism: %.3f Δ)   |   Pos: (%+.1f, %+.1f) mm   |   r: %.1f mm (%.1f°)   |   Meridian: %.1f°",
				info.OSColorSpread, info.OSPrism, info.DXMM, info.DYMM, info.DistanceMM, info.OSGazeDeg, info.MeridianDeg,
			))
		},
		func() { app.statusBar.ClearMessage() },
	)
	caLayout.AddWidget2(caSplit.QWidget, 1)
	caLayout.AddWidget2(buildPrismLegend("Lateral Color Spread (Δ):"), 0)
	tabs.AddTab(caPage, "Chromatic Aberration (OD / OS)")

	// Tab 2: Prismatic Difference
	pdiffPage := qt.NewQWidget2()
	pdiffLayout := qt.NewQVBoxLayout(pdiffPage)
	pdiffLayout.SetContentsMargins(4, 4, 4, 4)
	app.pdiffViewer = NewImageViewer(pdiffPage)
	app.pdiffViewer.SetHoverCallbacks(
		func(x, y float64) {
			info := dsm.InspectPoint(app.controls.Data(), x, y)
			app.statusBar.ShowMessage(fmt.Sprintf(
				"Prismatic Diff: %+.3f Δ   |   Pos: (%+.1f, %+.1f) mm   |   r: %.1f mm (%.1f°)   |   Meridian: %.1f°",
				info.PrismaticDiff, info.DXMM, info.DYMM, info.DistanceMM, info.GazeDeg, info.MeridianDeg,
			))
		},
		func() { app.statusBar.ClearMessage() },
	)
	pdiffLayout.AddWidget2(buildZoomBar(func() []*ImageViewer { return []*ImageViewer{app.pdiffViewer} }), 0)
	pdiffLayout.AddWidget2(app.pdiffViewer.View.QWidget, 1)
	pdiffLayout.AddWidget2(buildPrismaticDiffLegend(), 0)
	tabs.AddTab(pdiffPage, "Prismatic Difference")

	// Tab 3: Magnification Ellipse
	magPage := qt.NewQWidget2()
	magLayout := qt.NewQVBoxLayout(magPage)
	magLayout.SetContentsMargins(4, 4, 4, 4)
	app.magViewer = NewImageViewer(magPage)
	magLayout.AddWidget2(buildZoomBar(func() []*ImageViewer { return []*ImageViewer{app.magViewer} }), 0)
	magLayout.AddWidget2(app.magViewer.View.QWidget, 1)
	magLayout.AddWidget2(buildMagEllipseLegend(), 0)
	tabs.AddTab(magPage, "Magnification Ellipse")

	return tabs
}

func makeDualLensPane(leftTitle, rightTitle string) (*qt.QSplitter, *ImageViewer, *ImageViewer) {
	split := qt.NewQSplitter3(qt.Horizontal)

	leftBox := qt.NewQWidget2()
	leftLay := qt.NewQVBoxLayout(leftBox)
	leftLay.SetContentsMargins(2, 2, 2, 2)
	leftLbl := qt.NewQLabel3(leftTitle)
	leftLbl.SetAlignment(qt.AlignCenter)
	leftLbl.SetStyleSheet("font-weight: bold;")
	leftViewer := NewImageViewer(leftBox)
	leftLay.AddWidget(leftLbl.QWidget)
	leftLay.AddWidget(leftViewer.View.QWidget)

	rightBox := qt.NewQWidget2()
	rightLay := qt.NewQVBoxLayout(rightBox)
	rightLay.SetContentsMargins(2, 2, 2, 2)
	rightLbl := qt.NewQLabel3(rightTitle)
	rightLbl.SetAlignment(qt.AlignCenter)
	rightLbl.SetStyleSheet("font-weight: bold;")
	rightViewer := NewImageViewer(rightBox)
	rightLay.AddWidget(rightLbl.QWidget)
	rightLay.AddWidget(rightViewer.View.QWidget)

	split.AddWidget(leftBox)
	split.AddWidget(rightBox)
	split.SetStretchFactor(0, 1)
	split.SetStretchFactor(1, 1)

	return split, leftViewer, rightViewer
}

func (app *App) buildMenus() {
	mb := app.window.MenuBar()

	// File menu
	fileMenu := mb.AddMenuWithTitle("&File")

	newAct := qt.NewQAction5("&New / Reset Defaults", app.window.QObject)
	newAct.SetShortcut(qt.NewQKeySequence2("Ctrl+N"))
	newAct.OnTriggered(func() {
		app.currentFile = ""
		app.controls.SetDefaultState()
		app.recomputeAll()
		app.updateTitle()
		app.statusBar.ShowMessage("Reset to default parameters")
	})
	fileMenu.AddAction(newAct)

	openAct := qt.NewQAction5("&Open...", app.window.QObject)
	openAct.SetShortcut(qt.NewQKeySequence2("Ctrl+O"))
	openAct.OnTriggered(func() {
		path := qt.QFileDialog_GetOpenFileName4(
			app.window.QWidget, "Open Optical Data File", app.lastDir, "Optical Data Files (*.json *.dat);;All Files (*)",
		)
		if path != "" {
			app.openFile(path)
		}
	})
	fileMenu.AddAction(openAct)

	saveAct := qt.NewQAction5("&Save", app.window.QObject)
	saveAct.SetShortcut(qt.NewQKeySequence2("Ctrl+S"))
	saveAct.OnTriggered(func() {
		if app.currentFile == "" {
			app.saveFileAs()
			return
		}
		app.saveFile(app.currentFile)
	})
	fileMenu.AddAction(saveAct)

	saveAsAct := qt.NewQAction5("Save &As...", app.window.QObject)
	saveAsAct.SetShortcut(qt.NewQKeySequence2("Ctrl+Shift+S"))
	saveAsAct.OnTriggered(func() { app.saveFileAs() })
	fileMenu.AddAction(saveAsAct)

	fileMenu.AddSeparator()

	loadTraceAct := qt.NewQAction5("Load &Trace...", app.window.QObject)
	loadTraceAct.SetShortcut(qt.NewQKeySequence2("Ctrl+T"))
	loadTraceAct.SetToolTip("Load a frame trace (.trc/.oma, .json, or .svg) to overlay on chromatic aberration and model thickness")
	loadTraceAct.OnTriggered(func() { app.promptLoadTrace() })
	fileMenu.AddAction(loadTraceAct)

	tracePhotoAct := qt.NewQAction5("Trace &Photo...", app.window.QObject)
	tracePhotoAct.SetShortcut(qt.NewQKeySequence2("Ctrl+P"))
	tracePhotoAct.SetToolTip("Open the Frame Photo Tracer pop-out window")
	tracePhotoAct.OnTriggered(func() { app.openTracerWindow() })
	fileMenu.AddAction(tracePhotoAct)

	clearTraceAct := qt.NewQAction5("&Clear Frame Trace", app.window.QObject)
	clearTraceAct.OnTriggered(func() { app.clearTrace() })
	fileMenu.AddAction(clearTraceAct)

	fileMenu.AddSeparator()

	quitAct := qt.NewQAction5("&Quit", app.window.QObject)
	quitAct.SetShortcut(qt.NewQKeySequence2("Ctrl+Q"))
	quitAct.OnTriggered(func() { qt.QCoreApplication_Quit() })
	fileMenu.AddAction(quitAct)

	// Tools menu
	toolsMenu := mb.AddMenuWithTitle("&Tools")

	toolsMenu.AddAction(tracePhotoAct)

	recommendAct := qt.NewQAction5("Lens &Recommendation...", app.window.QObject)
	recommendAct.SetShortcut(qt.NewQKeySequence2("Ctrl+R"))
	recommendAct.OnTriggered(func() { app.openRecommendWindow() })
	toolsMenu.AddAction(recommendAct)

	// Export menu
	exportMenu := mb.AddMenuWithTitle("&Export")

	exportCurrentAct := qt.NewQAction5("&Export Current Tab...", app.window.QObject)
	exportCurrentAct.SetShortcut(qt.NewQKeySequence2("Ctrl+E"))
	exportCurrentAct.OnTriggered(func() { app.exportCurrentTab() })
	exportMenu.AddAction(exportCurrentAct)

	exportAllAct := qt.NewQAction5("Export &All Plots to Directory...", app.window.QObject)
	exportAllAct.SetShortcut(qt.NewQKeySequence2("Ctrl+Shift+E"))
	exportAllAct.OnTriggered(func() { app.exportAllPlots() })
	exportMenu.AddAction(exportAllAct)

	exportReportAct := qt.NewQAction5("Export Lens Thickness &Report...", app.window.QObject)
	exportReportAct.OnTriggered(func() { app.exportThicknessReport() })
	exportMenu.AddAction(exportReportAct)

	// View menu
	viewMenu := mb.AddMenuWithTitle("&View")

	zoomInAct := qt.NewQAction5("Zoom &In", app.window.QObject)
	zoomInAct.SetShortcut(qt.NewQKeySequence2("Ctrl+="))
	zoomInAct.OnTriggered(func() {
		for _, v := range app.activeViewers() {
			v.ZoomIn()
		}
	})
	viewMenu.AddAction(zoomInAct)

	zoomOutAct := qt.NewQAction5("Zoom &Out", app.window.QObject)
	zoomOutAct.SetShortcut(qt.NewQKeySequence2("Ctrl+-"))
	zoomOutAct.OnTriggered(func() {
		for _, v := range app.activeViewers() {
			v.ZoomOut()
		}
	})
	viewMenu.AddAction(zoomOutAct)

	fitAct := qt.NewQAction5("&Fit to Window", app.window.QObject)
	fitAct.SetShortcut(qt.NewQKeySequence2("Ctrl+0"))
	fitAct.OnTriggered(func() {
		for _, v := range app.activeViewers() {
			v.FitToWindow()
		}
	})
	viewMenu.AddAction(fitAct)

	actualAct := qt.NewQAction5("&Actual Size (100%)", app.window.QObject)
	actualAct.SetShortcut(qt.NewQKeySequence2("Ctrl+1"))
	actualAct.OnTriggered(func() {
		for _, v := range app.activeViewers() {
			v.ActualSize()
		}
	})
	viewMenu.AddAction(actualAct)

	// Help menu
	helpMenu := mb.AddMenuWithTitle("&Help")
	aboutAct := qt.NewQAction5("&About Optical Analyzer", app.window.QObject)
	aboutAct.OnTriggered(func() {
		qt.QMessageBox_About(
			app.window.QWidget,
			"About Optical Analyzer",
			fmt.Sprintf(
				"<h3>Optical Analyzer %s</h3>"+
					"<p>Ophthalmic spectacle lens optics, conic thickness/weight modeling, material recommendation, and frame contour tracing.</p>"+
					"<p>Dynamic Spectacle Magnification (DSM) model based on the work of Arnulf Remole (University of Waterloo).</p>"+
					"<p>Licensed under 0BSD (Qt 6 via miqt).</p>",
				version.String(),
			),
		)
	})
	helpMenu.AddAction(aboutAct)

	// Main toolbar
	tb := app.window.AddToolBarWithTitle("Main")
	tb.SetMovable(false)
	tb.AddAction(openAct)
	tb.AddAction(saveAct)
	tb.AddSeparator()
	tb.AddAction(loadTraceAct)
	tb.AddAction(tracePhotoAct)
	tb.AddAction(recommendAct)
	tb.AddSeparator()
	tb.AddAction(exportCurrentAct)
	tb.AddAction(exportAllAct)
}

func (app *App) activeViewers() []*ImageViewer {
	switch app.tabs.CurrentIndex() {
	case tabPrismaticDiff:
		return []*ImageViewer{app.pdiffViewer}
	case tabChromatic:
		return []*ImageViewer{app.caOD, app.caOS}
	case tabMagEllipse:
		return []*ImageViewer{app.magViewer}
	default:
		return nil
	}
}

func (app *App) markAllDirty() {
	for i := range app.dirty {
		app.dirty[i] = true
	}
}

func (app *App) computeRecommendation() lens.RecommendationReport {
	raw := app.controls.RawData()
	if raw.Frame.A <= 0 || raw.Frame.B <= 0 {
		return lens.RecommendationReport{}
	}
	bevel := raw.Lenses.EffectiveBevelDepth()
	if bevel > 0 && (2.0*bevel >= raw.Frame.A || 2.0*bevel >= raw.Frame.B) {
		return lens.RecommendationReport{}
	}

	shapeOpt := app.controls.ActiveShapeOption()
	design := app.controls.SelectedDesign()
	inputs := lens.ReportInputsFromData(raw)
	opts := app.recommendWin.Options(raw.Fit.EffectiveODCenterOfRotation(), raw.Fit.EffectiveOSCenterOfRotation())
	return lens.RecommendMaterials(inputs, shapeOpt, design, opts)
}

func (app *App) computeThicknessReport(raw optics.Data, shapeOpt lens.ShapeOption, design optics.LensDesign) lens.Report {
	bevel := raw.Lenses.EffectiveBevelDepth()
	if raw.Frame.A <= 0 || raw.Frame.B <= 0 ||
		raw.Lenses.OD.Index <= 1.0 || raw.Lenses.OS.Index <= 1.0 ||
		(bevel > 0 && (2.0*bevel >= raw.Frame.A || 2.0*bevel >= raw.Frame.B)) {
		return lens.Report{}
	}
	return lens.BuildReport(
		lens.ReportInputsFromData(raw),
		[]lens.ShapeOption{shapeOpt},
		[]optics.LensDesign{design},
	)
}

func (app *App) recomputeAll() {
	rec := app.computeRecommendation()
	app.lastRecommendation = rec

	if app.controls.AutoMaterial() && len(rec.Recommendations) > 0 {
		app.controls.ApplyAutoMaterial(rec.Recommendations[0])
	}

	app.recommendWin.Refresh(rec)

	raw := app.controls.RawData()
	shapeOpt := app.controls.ActiveShapeOption()
	design := app.controls.SelectedDesign()
	data := app.controls.Data()
	report := app.computeThicknessReport(raw, shapeOpt, design)

	app.summary.Update(data, shapeOpt, design, report)
	app.updateTraceControls()
	app.markAllDirty()
	app.renderActiveTab()
}

func (app *App) applyRecommendedMaterial(rec lens.MaterialRecommendation) {
	app.controls.SelectMaterial(rec.Index, rec.Family, rec.Abbe)
	app.recomputeAll()
	app.statusBar.ShowMessage(fmt.Sprintf("Applied material: %s", rec.Name))
}

func (app *App) openTracerWindow() {
	raw := app.controls.RawData()
	app.tracerWin.ShowWithDefaults(raw.Frame.A, raw.Lenses.EffectiveBevelDepth(), app.lastDir)
}

func (app *App) openRecommendWindow() {
	app.recommendWin.Refresh(app.lastRecommendation)
	app.recommendWin.ShowAndRaise()
}

func (app *App) renderActiveTab() {
	idx := app.tabs.CurrentIndex()
	if idx < 0 || idx >= len(app.dirty) || !app.dirty[idx] {
		return
	}
	switch idx {
	case tabPrismaticDiff:
		data := app.controls.Data()
		app.pdiffViewer.SetImage(dsm.RenderPrismaticDifference(data))
	case tabChromatic:
		data := app.controls.Data()
		od, osImg := dsm.RenderChromaticAberrationWithTrace(data, app.controls.ActiveContourTrace(), app.traceOpts)
		app.caOD.SetImage(od)
		app.caOS.SetImage(osImg)
	case tabMagEllipse:
		data := app.controls.Data()
		app.magViewer.SetImage(dsm.RenderMagnificationEllipse(data, dsm.PreviewMagEllipseBaseSize))
	case tabThickness:
		app.thicknessTab.Recalculate(app.controls.RawData(), app.controls.ActiveShapeOption(), app.controls.SelectedDesign())
	}
	app.dirty[idx] = false
}

func (app *App) openFile(path string) {
	data, err := optics.LoadData(path)
	if err != nil {
		qt.QMessageBox_Critical(app.window.QWidget, "Open Error", fmt.Sprintf("Failed to load %s:\n%v", path, err))
		return
	}
	app.currentFile = path
	app.lastDir = filepath.Dir(path)
	app.controls.SetData(data)
	if data.Frame.Trace != "" && data.Frame.Trace != "-" {
		tracePath := optics.ResolveTracePath(path, data.Frame.Trace)
		if _, err := os.Stat(tracePath); err == nil {
			app.loadTraceFileWithFormat(tracePath, data.Frame.TraceFormat)
		}
	}
	app.recomputeAll()
	app.updateTitle()
	app.statusBar.ShowMessage(fmt.Sprintf("Loaded %s", path))
}

func (app *App) saveFile(path string) {
	raw := app.controls.SaveableData()
	if app.controls.SelectedShape() == "trace" && app.trace != nil && app.trace.Source != "" && app.trace.Source != "stdin" {
		if _, err := os.Stat(app.trace.Source); err == nil {
			raw.Frame.Trace = optics.RelativeTracePath(path, app.trace.Source)
			tf := optics.FrameFromTrace(app.trace, optics.Frame{})
			if math.Abs(raw.Frame.A-math.Round(tf.A*10)/10) < 1e-6 &&
				math.Abs(raw.Frame.B-math.Round(tf.B*10)/10) < 1e-6 &&
				math.Abs(raw.Frame.DBL-math.Round(tf.DBL*10)/10) < 1e-6 {
				raw.Frame.A = 0
				raw.Frame.B = 0
				raw.Frame.DBL = 0
			}
		}
	}
	if err := optics.SaveData(path, raw); err != nil {
		qt.QMessageBox_Critical(app.window.QWidget, "Save Error", fmt.Sprintf("Failed to save %s:\n%v", path, err))
		return
	}
	app.currentFile = path
	app.lastDir = filepath.Dir(path)
	app.updateTitle()
	app.statusBar.ShowMessage(fmt.Sprintf("Saved %s", path))
}

func (app *App) saveFileAs() {
	defaultPath := app.lastDir
	if app.currentFile != "" {
		defaultPath = app.currentFile
	} else {
		defaultPath = filepath.Join(app.lastDir, "prescription.json")
	}
	path := qt.QFileDialog_GetSaveFileName4(
		app.window.QWidget, "Save Optical Data File", defaultPath, "Optical Data Files (*.json *.dat);;All Files (*)",
	)
	if path != "" {
		app.saveFile(path)
	}
}

func (app *App) exportThicknessReport() {
	// Ensure report is up to date even if Lens Thickness tab wasn't visited yet.
	if app.dirty[tabThickness] {
		app.thicknessTab.Recalculate(app.controls.RawData(), app.controls.ActiveShapeOption(), app.controls.SelectedDesign())
		app.dirty[tabThickness] = false
	}
	defaultPath := filepath.Join(app.lastDir, "lens-report.json")
	path := qt.QFileDialog_GetSaveFileName4(
		app.window.QWidget, "Export Lens Thickness Report", defaultPath,
		"JSON Report (*.json);;CSV Table (*.csv);;Text Summary (*.txt)",
	)
	if path == "" {
		return
	}
	if err := app.thicknessTab.ExportReport(path); err != nil {
		qt.QMessageBox_Critical(app.window.QWidget, "Export Error", err.Error())
		return
	}
	app.lastDir = filepath.Dir(path)
	app.statusBar.ShowMessage("Exported report to " + path)
}

func (app *App) exportCurrentTab() {
	idx := app.tabs.CurrentIndex()
	if idx == tabThickness {
		app.exportThicknessReport()
		return
	}

	data := app.controls.Data()
	baseName := "out"
	if app.currentFile != "" {
		b := filepath.Base(app.currentFile)
		baseName = b[:len(b)-len(filepath.Ext(b))]
	}

	switch idx {
	case tabPrismaticDiff:
		defaultPath := filepath.Join(app.lastDir, baseName+"_prismatic_diff.png")
		path := qt.QFileDialog_GetSaveFileName4(app.window.QWidget, "Export Prismatic Difference PNG", defaultPath, "PNG Images (*.png)")
		if path == "" {
			return
		}
		if filepath.Ext(path) == "" {
			path += ".png"
		}
		if err := dsm.SavePNG(path, dsm.RenderPrismaticDifference(data)); err != nil {
			qt.QMessageBox_Critical(app.window.QWidget, "Export Error", err.Error())
			return
		}
		app.lastDir = filepath.Dir(path)
		app.statusBar.ShowMessage("Exported " + path)

	case tabChromatic:
		defaultPath := filepath.Join(app.lastDir, baseName+"_chromatic_aberration.png")
		path := qt.QFileDialog_GetSaveFileName4(app.window.QWidget, "Export Chromatic Aberration PNGs (creates _od.png and _os.png)", defaultPath, "PNG Images (*.png)")
		if path == "" {
			return
		}
		stem := path[:len(path)-len(filepath.Ext(path))]
		odPath := stem + "_od.png"
		osPath := stem + "_os.png"

		odImg, osImg := dsm.RenderChromaticAberrationWithTrace(data, app.controls.ActiveContourTrace(), app.traceOpts)
		if err := dsm.SavePNG(odPath, odImg); err != nil {
			qt.QMessageBox_Critical(app.window.QWidget, "Export Error", err.Error())
			return
		}
		if err := dsm.SavePNG(osPath, osImg); err != nil {
			qt.QMessageBox_Critical(app.window.QWidget, "Export Error", err.Error())
			return
		}
		app.lastDir = filepath.Dir(path)
		app.statusBar.ShowMessage(fmt.Sprintf("Exported %s and %s", filepath.Base(odPath), filepath.Base(osPath)))

	case tabMagEllipse:
		defaultPath := filepath.Join(app.lastDir, baseName+"_mag_ellipse.png")
		path := qt.QFileDialog_GetSaveFileName4(app.window.QWidget, "Export Magnification Ellipse PNG (6000×6000)", defaultPath, "PNG Images (*.png)")
		if path == "" {
			return
		}
		if filepath.Ext(path) == "" {
			path += ".png"
		}
		app.statusBar.ShowMessage("Rendering 6000×6000 magnification ellipse...")
		qt.QCoreApplication_ProcessEvents()
		if err := dsm.SavePNG(path, dsm.RenderMagnificationEllipse(data, dsm.DefaultMagEllipseBaseSize)); err != nil {
			qt.QMessageBox_Critical(app.window.QWidget, "Export Error", err.Error())
			return
		}
		app.lastDir = filepath.Dir(path)
		app.statusBar.ShowMessage("Exported " + path)
	}
}

func (app *App) exportAllPlots() {
	dir := qt.QFileDialog_GetExistingDirectory3(app.window.QWidget, "Export All Plots to Directory", app.lastDir)
	if dir == "" {
		return
	}
	app.lastDir = dir
	data := app.controls.Data()
	prefix := "optical"
	if app.currentFile != "" {
		b := filepath.Base(app.currentFile)
		prefix = b[:len(b)-len(filepath.Ext(b))]
	}

	app.statusBar.ShowMessage("Exporting all plots...")
	qt.QCoreApplication_ProcessEvents()

	pdiff := dsm.RenderPrismaticDifference(data)
	caOD, caOS := dsm.RenderChromaticAberrationWithTrace(data, app.controls.ActiveContourTrace(), app.traceOpts)
	mag := dsm.RenderMagnificationEllipse(data, dsm.DefaultMagEllipseBaseSize)

	files := []struct {
		name string
		save func(string) error
	}{
		{prefix + "_ca_od.png", func(p string) error { return dsm.SavePNG(p, caOD) }},
		{prefix + "_ca_os.png", func(p string) error { return dsm.SavePNG(p, caOS) }},
		{prefix + "_prismatic_diff.png", func(p string) error { return dsm.SavePNG(p, pdiff) }},
		{prefix + "_mag_ellipse.png", func(p string) error { return dsm.SavePNG(p, mag) }},
	}

	for _, f := range files {
		full := filepath.Join(dir, f.name)
		if err := f.save(full); err != nil {
			qt.QMessageBox_Critical(app.window.QWidget, "Export Error", fmt.Sprintf("Failed to write %s:\n%v", full, err))
			return
		}
	}
	app.statusBar.ShowMessage(fmt.Sprintf("Exported all 4 plots to %s", dir))
}

func addZoomButtons(lay *qt.QHBoxLayout, viewers func() []*ImageViewer) {
	zoomInBtn := qt.NewQPushButton3("Zoom In")
	zoomInBtn.SetToolTip("Zoom in (Ctrl+=)")
	zoomInBtn.OnClicked(func() {
		for _, v := range viewers() {
			v.ZoomIn()
		}
	})
	lay.AddWidget(zoomInBtn.QWidget)

	zoomOutBtn := qt.NewQPushButton3("Zoom Out")
	zoomOutBtn.SetToolTip("Zoom out (Ctrl+-)")
	zoomOutBtn.OnClicked(func() {
		for _, v := range viewers() {
			v.ZoomOut()
		}
	})
	lay.AddWidget(zoomOutBtn.QWidget)

	fitBtn := qt.NewQPushButton3("Fit to Window")
	fitBtn.SetToolTip("Fit image to window (Ctrl+0)")
	fitBtn.OnClicked(func() {
		for _, v := range viewers() {
			v.FitToWindow()
		}
	})
	lay.AddWidget(fitBtn.QWidget)

	actualBtn := qt.NewQPushButton3("Actual Size (100%)")
	actualBtn.SetToolTip("Reset zoom to 100% (Ctrl+1)")
	actualBtn.OnClicked(func() {
		for _, v := range viewers() {
			v.ActualSize()
		}
	})
	lay.AddWidget(actualBtn.QWidget)
}

func buildZoomBar(viewers func() []*ImageViewer) *qt.QWidget {
	bar := qt.NewQWidget2()
	lay := qt.NewQHBoxLayout(bar)
	lay.SetContentsMargins(4, 2, 4, 2)
	lay.SetSpacing(6)
	lay.AddStretch()
	addZoomButtons(lay, viewers)
	return bar
}

func (app *App) buildTraceBar(viewers func() []*ImageViewer) *qt.QWidget {
	bar := qt.NewQWidget2()
	lay := qt.NewQHBoxLayout(bar)
	lay.SetContentsMargins(4, 2, 4, 2)
	lay.SetSpacing(8)

	app.showTraceCheck = qt.NewQCheckBox3("Show Contour")
	app.showTraceCheck.SetChecked(app.traceOpts.ShowTrace)
	app.showTraceCheck.OnToggled(func(checked bool) {
		app.traceOpts.ShowTrace = checked
		app.dirty[tabChromatic] = true
		app.renderActiveTab()
	})
	lay.AddWidget(app.showTraceCheck.QWidget)

	app.showBoxCheck = qt.NewQCheckBox3("Show Box")
	app.showBoxCheck.SetChecked(app.traceOpts.ShowBoundingBox)
	app.showBoxCheck.OnToggled(func(checked bool) {
		app.traceOpts.ShowBoundingBox = checked
		app.dirty[tabChromatic] = true
		app.renderActiveTab()
	})
	lay.AddWidget(app.showBoxCheck.QWidget)

	app.dimOutsideCheck = qt.NewQCheckBox3("Dim Outside")
	app.dimOutsideCheck.SetChecked(app.traceOpts.DimOutsideTrace)
	app.dimOutsideCheck.OnToggled(func(checked bool) {
		app.traceOpts.DimOutsideTrace = checked
		app.dirty[tabChromatic] = true
		app.renderActiveTab()
	})
	lay.AddWidget(app.dimOutsideCheck.QWidget)

	lay.AddStretch()
	addZoomButtons(lay, viewers)

	return bar
}

func (app *App) promptLoadTrace() {
	path := qt.QFileDialog_GetOpenFileName4(
		app.window.QWidget, "Open Frame Trace", app.lastDir, "Frame Trace Files (*.trc *.oma *.json *.svg);;All Files (*)",
	)
	if path != "" {
		app.loadTraceFile(path)
	}
}

func (app *App) loadTraceFile(path string) {
	app.loadTraceFileWithFormat(path, "")
}

func (app *App) loadTraceFileWithFormat(path, format string) {
	tr, err := contour.ReadFromPath(path, format, nil, 0, 0, 0)
	if err != nil {
		qt.QMessageBox_Critical(app.window.QWidget, "Frame Trace Error", fmt.Sprintf("Failed to load %s:\n%v", path, err))
		return
	}
	if tr.Name == "" {
		tr.Name = tr.Model
	}
	if tr.Name == "" {
		tr.Name = fmt.Sprintf("Trace (%s)", filepath.Base(path))
	}
	app.lastDir = filepath.Dir(path)
	app.applyTrace(tr, false)
	app.statusBar.ShowMessage(fmt.Sprintf("Loaded frame trace: %s", tr.Summary()))
}

func (app *App) applyContourTrace(tr *contour.Trace, applyGroove bool) {
	if tr == nil {
		return
	}
	app.applyTrace(tr, applyGroove)
	app.statusBar.ShowMessage(fmt.Sprintf("Applied traced frame: %s", tr.Summary()))
}

func (app *App) applyTrace(tr *contour.Trace, applyGroove bool) {
	app.trace = tr
	app.controls.SetLoadedTrace(tr)
	app.traceOpts.ShowTrace = true

	app.controls.SetFrame(optics.FrameFromTrace(tr, app.controls.Data().Frame))
	if applyGroove && tr != nil && tr.GrooveDepthMM >= 0 {
		app.controls.SetBevelDepth(tr.GrooveDepthMM)
	}

	app.updateTraceControls()
	app.recomputeAll()
}

func (app *App) clearTrace() {
	app.trace = nil
	app.controls.SetLoadedTrace(nil)
	app.updateTraceControls()
	app.recomputeAll()
	app.statusBar.ShowMessage("Cleared frame trace")
}

func (app *App) updateTraceControls() {
	app.showTraceCheck.SetEnabled(true)
	app.dimOutsideCheck.SetEnabled(true)
	app.showTraceCheck.SetChecked(app.traceOpts.ShowTrace)
}

func (app *App) updateTitle() {
	if app.currentFile == "" {
		app.window.SetWindowTitle("Optical Analyzer")
		return
	}
	app.window.SetWindowTitle(fmt.Sprintf("Optical Analyzer — %s", filepath.Base(app.currentFile)))
}

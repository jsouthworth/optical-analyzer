package main

import (
	"fmt"
	"math"
	"path/filepath"
	"strings"

	qt "github.com/mappu/miqt/qt6"

	"jsouthworth.net/go/optical-analyzer/contour"
	"jsouthworth.net/go/optical-analyzer/optics"
	"jsouthworth.net/go/optical-analyzer/tracer"
)

// FrameTracerWindow is a separate pop-out window for tracing front-view frame photos,
// previewing the trace debug overlay, saving in OMA .trc format, or applying the trace as-is.
type FrameTracerWindow struct {
	Dialog *qt.QDialog

	photoEdit    *qt.QLineEdit
	aSpin        *qt.QDoubleSpinBox
	bSpin        *qt.QDoubleSpinBox
	dblSpin      *qt.QDoubleSpinBox
	grooveSpin   *qt.QDoubleSpinBox
	traceBtn     *qt.QPushButton
	errLabel     *qt.QLabel
	viewer       *ImageViewer
	placeholder  *qt.QLabel
	previewStack *qt.QStackedWidget
	infoLabel    *qt.QLabel
	saveOMABtn   *qt.QPushButton
	useTraceBtn  *qt.QPushButton

	photoPath   string
	savedPath   string
	tracedFrame *tracer.TracedFrame
	lastDir     string
	syncing     bool

	getCurrentData func() optics.Data
	onUseTrace     func(tr *contour.Trace)
}

func NewFrameTracerWindow(parent *qt.QWidget, getCurrentData func() optics.Data, onUseTrace func(*contour.Trace)) *FrameTracerWindow {
	w := &FrameTracerWindow{
		getCurrentData: getCurrentData,
		onUseTrace:     onUseTrace,
	}

	w.Dialog = qt.NewQDialog(parent)
	w.Dialog.SetWindowTitle("Frame Photo Tracer")
	w.Dialog.Resize(900, 640)
	w.Dialog.SetMinimumSize2(680, 480)
	w.Dialog.SetWindowFlag2(qt.WindowMinMaxButtonsHint, true)

	root := qt.NewQVBoxLayout(w.Dialog.QWidget)
	root.SetContentsMargins(10, 10, 10, 10)
	root.SetSpacing(8)

	// Row 1: Photo selection
	fileRow := qt.NewQHBoxLayout2()
	fileRow.SetSpacing(6)
	fileRow.AddWidget(qt.NewQLabel3("Frame Photo:").QWidget)
	w.photoEdit = qt.NewQLineEdit2()
	w.photoEdit.SetReadOnly(true)
	w.photoEdit.SetPlaceholderText("Select a front-view PNG or JPEG photo of the eyeglass frame...")
	fileRow.AddWidget2(w.photoEdit.QWidget, 1)

	browseBtn := qt.NewQPushButton3("Browse Photo...")
	browseBtn.OnClicked(func() { w.browsePhoto() })
	fileRow.AddWidget(browseBtn.QWidget)
	root.AddLayout(fileRow.QLayout)

	// Row 2: Calibration measurements
	paramRow := qt.NewQHBoxLayout2()
	paramRow.SetSpacing(8)

	w.aSpin = qt.NewQDoubleSpinBox2()
	w.aSpin.SetRange(10.0, 90.0)
	w.aSpin.SetSingleStep(0.5)
	w.aSpin.SetDecimals(1)
	w.aSpin.SetSuffix(" mm")
	w.aSpin.SetValue(optics.DefaultFrameA)
	w.aSpin.SetToolTip("Stamped frame A (boxing width) in mm — required for scale calibration")

	w.bSpin = qt.NewQDoubleSpinBox2()
	w.bSpin.SetRange(0.0, 80.0)
	w.bSpin.SetSingleStep(0.5)
	w.bSpin.SetDecimals(1)
	w.bSpin.SetSuffix(" mm")
	w.bSpin.SetSpecialValueText("Auto (infer)")
	w.bSpin.SetValue(0.0)
	w.bSpin.SetToolTip("Optional frame B (boxing height) in mm — leave on Auto to infer from photo")

	w.dblSpin = qt.NewQDoubleSpinBox2()
	w.dblSpin.SetRange(0.0, 40.0)
	w.dblSpin.SetSingleStep(0.5)
	w.dblSpin.SetDecimals(1)
	w.dblSpin.SetSuffix(" mm")
	w.dblSpin.SetSpecialValueText("Auto (infer)")
	w.dblSpin.SetValue(0.0)
	w.dblSpin.SetToolTip("Optional bridge DBL in mm — leave on Auto to infer from photo")

	w.grooveSpin = qt.NewQDoubleSpinBox2()
	w.grooveSpin.SetRange(0.0, 3.0)
	w.grooveSpin.SetSingleStep(0.1)
	w.grooveSpin.SetDecimals(2)
	w.grooveSpin.SetSuffix(" mm")
	w.grooveSpin.SetValue(tracer.DefaultGrooveDepthMM)
	w.grooveSpin.SetToolTip("Eyewire groove depth per side in mm (0 = rimless / exact visible opening)")

	retraceOnEdit := func(float64) {
		if w.syncing || w.photoPath == "" {
			return
		}
		w.RunTrace()
	}
	w.aSpin.OnValueChanged(retraceOnEdit)
	w.bSpin.OnValueChanged(retraceOnEdit)
	w.dblSpin.OnValueChanged(retraceOnEdit)
	w.grooveSpin.OnValueChanged(retraceOnEdit)

	paramRow.AddWidget(qt.NewQLabel3("A:").QWidget)
	paramRow.AddWidget(w.aSpin.QWidget)
	paramRow.AddWidget(qt.NewQLabel3("B:").QWidget)
	paramRow.AddWidget(w.bSpin.QWidget)
	paramRow.AddWidget(qt.NewQLabel3("DBL:").QWidget)
	paramRow.AddWidget(w.dblSpin.QWidget)
	paramRow.AddWidget(qt.NewQLabel3("Groove:").QWidget)
	paramRow.AddWidget(w.grooveSpin.QWidget)

	w.traceBtn = qt.NewQPushButton3("Trace Photo")
	w.traceBtn.SetEnabled(false)
	w.traceBtn.OnClicked(func() { w.RunTrace() })
	paramRow.AddWidget(w.traceBtn.QWidget)
	paramRow.AddStretch()
	root.AddLayout(paramRow.QLayout)

	// Error banner
	w.errLabel = qt.NewQLabel2()
	w.errLabel.SetWordWrap(true)
	w.errLabel.SetStyleSheet("QLabel { padding: 6px 10px; border-radius: 4px; background-color: rgba(215, 45, 45, 0.16); color: #d94444; font-weight: bold; }")
	w.errLabel.SetVisible(false)
	root.AddWidget(w.errLabel.QWidget)

	// Center debug overlay preview area
	previewBox := qt.NewQGroupBox3("Trace Debug Preview (Green: OD, Cyan: OS, Yellow: Boxing, Red: Optical Centers)")
	previewLay := qt.NewQVBoxLayout(previewBox.QWidget)
	previewLay.SetContentsMargins(6, 6, 6, 6)
	previewLay.SetSpacing(4)

	w.previewStack = qt.NewQStackedWidget2()
	w.placeholder = qt.NewQLabel3("Click \"Browse Photo...\" to load a clean front-view frame image and preview the detected trace.")
	w.placeholder.SetAlignment(qt.AlignCenter)
	w.placeholder.SetWordWrap(true)

	w.viewer = NewImageViewer(previewBox.QWidget)
	w.previewStack.AddWidget(w.placeholder.QWidget)
	w.previewStack.AddWidget(w.viewer.View.QWidget)
	w.previewStack.SetCurrentIndex(0)

	previewLay.AddWidget2(w.previewStack.QWidget, 1)

	zoomRow := qt.NewQHBoxLayout2()
	zoomHint := qt.NewQLabel3("Scroll to zoom, drag to pan, double-click to fit")
	zoomRow.AddWidget(zoomHint.QWidget)
	zoomRow.AddStretch()
	fitBtn := qt.NewQPushButton3("Fit")
	fitBtn.OnClicked(func() { w.viewer.FitToWindow() })
	zoomInBtn := qt.NewQPushButton3("+")
	zoomInBtn.SetFixedWidth(28)
	zoomInBtn.OnClicked(func() { w.viewer.ZoomIn() })
	zoomOutBtn := qt.NewQPushButton3("−")
	zoomOutBtn.SetFixedWidth(28)
	zoomOutBtn.OnClicked(func() { w.viewer.ZoomOut() })
	zoomRow.AddWidget(zoomOutBtn.QWidget)
	zoomRow.AddWidget(zoomInBtn.QWidget)
	zoomRow.AddWidget(fitBtn.QWidget)
	previewLay.AddLayout(zoomRow.QLayout)

	root.AddWidget2(previewBox.QWidget, 1)

	// Trace info readout
	w.infoLabel = qt.NewQLabel3("No trace computed yet.")
	w.infoLabel.SetWordWrap(true)
	root.AddWidget(w.infoLabel.QWidget)

	// Bottom action bar
	btnRow := qt.NewQHBoxLayout2()
	btnRow.SetSpacing(8)

	w.saveOMABtn = qt.NewQPushButton3("Save OMA Trace (.trc)...")
	w.saveOMABtn.SetToolTip("Save the traced lens contours in OMA/VCA .trc format")
	w.saveOMABtn.SetEnabled(false)
	w.saveOMABtn.OnClicked(func() { w.promptSaveOMA() })
	btnRow.AddWidget(w.saveOMABtn.QWidget)

	btnRow.AddStretch()

	w.useTraceBtn = qt.NewQPushButton3("Use Trace")
	w.useTraceBtn.SetToolTip("Apply this traced contour directly to the analyzer as-is (without requiring saving)")
	w.useTraceBtn.SetEnabled(false)
	w.useTraceBtn.SetDefault(true)
	w.useTraceBtn.OnClicked(func() { w.useCurrentTrace() })
	btnRow.AddWidget(w.useTraceBtn.QWidget)

	closeBtn := qt.NewQPushButton3("Close")
	closeBtn.OnClicked(func() { w.Dialog.Hide() })
	btnRow.AddWidget(closeBtn.QWidget)

	root.AddLayout(btnRow.QLayout)

	return w
}

// ShowForDefaults refreshes default calibration A/groove from the main window when opening.
func (w *FrameTracerWindow) ShowWithDefaults(frameA, grooveDepth float64, dir string) {
	if dir != "" && w.lastDir == "" {
		w.lastDir = dir
	}
	if w.photoPath == "" {
		w.syncing = true
		if frameA > 0 {
			w.aSpin.SetValue(frameA)
		}
		if grooveDepth >= 0 {
			w.grooveSpin.SetValue(grooveDepth)
		}
		w.syncing = false
	}
	w.Dialog.Show()
	w.Dialog.Raise()
	w.Dialog.ActivateWindow()
}

func (w *FrameTracerWindow) browsePhoto() {
	path := qt.QFileDialog_GetOpenFileName4(
		w.Dialog.QWidget,
		"Select Front-View Frame Photo",
		w.lastDir,
		"Images (*.png *.jpg *.jpeg);;All Files (*)",
	)
	if path == "" {
		return
	}
	w.LoadPhoto(path)
}

// LoadPhoto sets the frame image path and immediately traces it for preview.
func (w *FrameTracerWindow) LoadPhoto(path string) error {
	w.photoPath = path
	w.savedPath = ""
	w.lastDir = filepath.Dir(path)
	w.photoEdit.SetText(path)
	w.traceBtn.SetEnabled(true)
	return w.RunTrace()
}

// RunTrace traces the currently loaded frame photo and renders the debug overlay preview.
func (w *FrameTracerWindow) RunTrace() error {
	if w.photoPath == "" {
		return fmt.Errorf("no photo selected")
	}

	frameA := w.aSpin.Value()
	frameB := w.bSpin.Value()
	frameDBL := w.dblSpin.Value()
	groove := w.grooveSpin.Value()

	traced, err := tracer.TraceFile(w.photoPath, nil, frameA, frameB, groove)
	if err != nil {
		w.tracedFrame = nil
		w.saveOMABtn.SetEnabled(false)
		w.useTraceBtn.SetEnabled(false)
		w.errLabel.SetText(err.Error())
		w.errLabel.SetVisible(true)
		w.previewStack.SetCurrentIndex(0)
		w.infoLabel.SetText("Trace failed — adjust calibration or choose a cleaner front-view frame photo.")
		return err
	}

	effB := frameB
	bTag := "override"
	if effB <= 0 {
		effB = traced.InferredB
		bTag = "inferred"
	}
	effDBL := frameDBL
	dblTag := "override"
	if effDBL <= 0 {
		effDBL = traced.InferredDBL
		dblTag = "inferred"
	}

	var curData optics.Data
	if w.getCurrentData != nil {
		curData = w.getCurrentData()
	}
	overlayImg, err := tracer.RenderDebugOverlay(traced, tracer.OverlayParams{
		FrameA:   frameA,
		FrameB:   effB,
		FrameDBL: effDBL,
		ODPD:     curData.Fit.OD.PD,
		OSPD:     curData.Fit.OS.PD,
		ODOCH:    curData.Fit.OD.OCH,
		OSOCH:    curData.Fit.OS.OCH,
	})
	if err != nil {
		w.tracedFrame = nil
		w.saveOMABtn.SetEnabled(false)
		w.useTraceBtn.SetEnabled(false)
		w.errLabel.SetText(err.Error())
		w.errLabel.SetVisible(true)
		return err
	}

	w.tracedFrame = traced
	w.errLabel.SetVisible(false)
	w.viewer.SetImage(overlayImg)
	w.previewStack.SetCurrentIndex(1)
	w.viewer.FitToWindow()
	w.saveOMABtn.SetEnabled(true)
	w.useTraceBtn.SetEnabled(true)

	tiltDeg := traced.TiltRad * 180.0 / math.Pi
	w.infoLabel.SetText(fmt.Sprintf(
		"Detected: A = %.1f mm  |  B = %.1f mm (%s)  |  DBL = %.1f mm (%s)  |  Groove = %.2f mm/side  |  Tilt = %+.1f°",
		frameA, effB, bTag, effDBL, dblTag, groove, tiltDeg,
	))
	return nil
}

// CurrentContourTrace returns the active traced contour pair as a *contour.Trace.
func (w *FrameTracerWindow) CurrentContourTrace() *contour.Trace {
	if w.tracedFrame == nil {
		return nil
	}
	name := fmt.Sprintf("Photo (%s)", filepath.Base(w.photoPath))
	source := w.savedPath
	if source == "" {
		source = w.photoPath
	}
	tr := w.tracedFrame.ToContourTrace(name, source, w.aSpin.Value(), w.bSpin.Value(), w.dblSpin.Value())
	return &tr
}

// SaveOMATrace writes the current traced contour in OMA/VCA .trc format to path.
func (w *FrameTracerWindow) SaveOMATrace(path string) error {
	tr := w.CurrentContourTrace()
	if tr == nil {
		return fmt.Errorf("no active trace to save")
	}
	if err := contour.WriteToPath(path, string(contour.FormatOMA), nil, *tr); err != nil {
		w.errLabel.SetText(err.Error())
		w.errLabel.SetVisible(true)
		return err
	}
	w.savedPath = path
	w.lastDir = filepath.Dir(path)
	w.errLabel.SetVisible(false)
	w.infoLabel.SetText(fmt.Sprintf("Saved OMA trace to %s (A=%.1f, B=%.1f, DBL=%.1f mm)", filepath.Base(path), tr.FrameA, tr.FrameB, tr.FrameDBL))
	return nil
}

func (w *FrameTracerWindow) promptSaveOMA() {
	if w.tracedFrame == nil {
		return
	}
	defaultPath := filepath.Join(w.lastDir, "frame-trace.trc")
	if w.photoPath != "" {
		stem := strings.TrimSuffix(w.photoPath, filepath.Ext(w.photoPath))
		if stem != "" {
			defaultPath = stem + ".trc"
		}
	}
	path := qt.QFileDialog_GetSaveFileName4(
		w.Dialog.QWidget,
		"Save OMA Frame Trace",
		defaultPath,
		"OMA/VCA Trace (*.trc *.oma);;All Files (*)",
	)
	if path == "" {
		return
	}
	if filepath.Ext(path) == "" {
		path += ".trc"
	}
	_ = w.SaveOMATrace(path)
}

func (w *FrameTracerWindow) useCurrentTrace() {
	tr := w.CurrentContourTrace()
	if tr == nil {
		return
	}
	if w.onUseTrace != nil {
		w.onUseTrace(tr)
	}
}

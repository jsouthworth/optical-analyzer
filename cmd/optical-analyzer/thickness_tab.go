package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	qt "github.com/mappu/miqt/qt6"

	"jsouthworth.net/go/optical-analyzer/lens"
	"jsouthworth.net/go/optical-analyzer/optics"
)

const (
	roleShapeIdx  = int(qt.UserRole) + 1
	roleDesignIdx = int(qt.UserRole) + 2
)

type ThicknessTab struct {
	Widget *qt.QWidget

	errLabel *qt.QLabel
	preview  *qt.QWidget
	table    *qt.QTableWidget

	activeShapes      []lens.ShapeOption
	activeDesigns     []optics.LensDesign
	report            lens.Report
	selectedShapeIdx  int
	selectedDesignIdx int
	errMsg            string

	syncing bool
}

func NewThicknessTab() *ThicknessTab {
	tab := &ThicknessTab{}

	tab.Widget = qt.NewQWidget2()
	root := qt.NewQVBoxLayout(tab.Widget)
	root.SetContentsMargins(6, 6, 6, 6)
	root.SetSpacing(6)

	// Inline error banner
	tab.errLabel = qt.NewQLabel2()
	tab.errLabel.SetWordWrap(true)
	tab.errLabel.SetStyleSheet("QLabel { padding: 6px 10px; border-radius: 4px; background-color: rgba(215, 45, 45, 0.16); color: #d94444; font-weight: bold; }")
	tab.errLabel.SetVisible(false)
	root.AddWidget(tab.errLabel.QWidget)

	// Live preview
	previewBox := qt.NewQGroupBox3("Live Lens Preview (Front Contours && Horizontal Cross-Section)")
	previewLay := qt.NewQVBoxLayout(previewBox.QWidget)
	previewLay.SetContentsMargins(8, 8, 8, 8)
	tab.preview = newThicknessPreviewWidget(tab)
	previewLay.AddWidget(tab.preview)
	root.AddWidget(previewBox.QWidget)

	// Results table
	tab.table = qt.NewQTableWidget2()
	tab.table.SetColumnCount(11)
	tab.table.SetHorizontalHeaderLabels([]string{
		"Shape", "Design", "Eye", "BC", "CT", "Max", "Avg", "Temp", "Nasal", "Wt (g)", "Pair (g)",
	})
	tab.table.SetSelectionBehavior(qt.QAbstractItemView__SelectRows)
	tab.table.SetSelectionMode(qt.QAbstractItemView__ExtendedSelection)
	tab.table.SetEditTriggers(qt.QAbstractItemView__NoEditTriggers)
	tab.table.SetAlternatingRowColors(true)
	tab.table.SetShowGrid(false)
	tab.table.VerticalHeader().SetVisible(false)
	tab.table.SetSortingEnabled(true)

	hdr := tab.table.HorizontalHeader()
	hdr.SetSectionResizeMode2(0, qt.QHeaderView__Stretch)
	for c := 1; c < 11; c++ {
		hdr.SetSectionResizeMode2(c, qt.QHeaderView__ResizeToContents)
	}

	tab.table.OnCellClicked(func(row, _ int) {
		tab.selectFromTableRow(row)
	})
	tab.table.OnCurrentCellChanged(func(currentRow, _, _, _ int) {
		if tab.syncing || currentRow < 0 {
			return
		}
		tab.selectFromTableRow(currentRow)
	})

	root.AddWidget2(tab.table.QWidget, 1)

	return tab
}

// Recalculate updates the Lens Thickness report, preview, summary, and table from raw optics.Data,
// the active Frame Shape contour, and the active Lens Design from Material & Optics.
func (tab *ThicknessTab) Recalculate(rawData optics.Data, shapeOpt lens.ShapeOption, design optics.LensDesign) {
	tab.errMsg = ""
	clearResults := func(msg string) {
		tab.errMsg = msg
		tab.report = lens.Report{}
		tab.activeShapes = nil
		tab.activeDesigns = nil
		tab.refreshUI()
	}

	frameA := rawData.Frame.A
	frameB := rawData.Frame.B
	bevel := rawData.Lenses.EffectiveBevelDepth()

	if frameA <= 0 || frameB <= 0 {
		clearResults("Frame A and B measurements must be > 0.")
		return
	}
	if rawData.Lenses.OD.Index <= 1.0 || rawData.Lenses.OS.Index <= 1.0 {
		clearResults("Refractive index must be > 1.0.")
		return
	}
	if bevel > 0 && (2.0*bevel >= frameA || 2.0*bevel >= frameB) {
		clearResults(fmt.Sprintf("Bevel depth (%.2f mm/side) must be less than half of frame A and B.", bevel))
		return
	}

	shapes := []lens.ShapeOption{shapeOpt}
	designs := []optics.LensDesign{design}

	inputs := lens.ReportInputsFromData(rawData)
	tab.activeShapes = shapes
	tab.activeDesigns = designs
	tab.report = lens.BuildReport(inputs, shapes, designs)

	tab.selectedShapeIdx = 0
	tab.selectedDesignIdx = 0

	tab.refreshUI()
}

func (tab *ThicknessTab) refreshUI() {
	if tab.errMsg != "" {
		tab.errLabel.SetText(tab.errMsg)
		tab.errLabel.SetVisible(true)
	} else {
		tab.errLabel.SetVisible(false)
	}
	tab.refreshTable()
	tab.highlightSelectedPairInTable()
	tab.preview.Update()
}

func (tab *ThicknessTab) selectFromTableRow(row int) {
	item := tab.table.Item(row, 0)
	if item == nil {
		return
	}
	si := item.Data(roleShapeIdx).ToInt()
	di := item.Data(roleDesignIdx).ToInt()
	if si == tab.selectedShapeIdx && di == tab.selectedDesignIdx {
		return
	}
	tab.selectedShapeIdx = si
	tab.selectedDesignIdx = di
	tab.highlightSelectedPairInTable()
	tab.preview.Update()
}

func (tab *ThicknessTab) highlightSelectedPairInTable() {
	tab.syncing = true
	defer func() { tab.syncing = false }()

	tab.table.ClearSelection()
	rows := tab.table.RowCount()
	for r := 0; r < rows; r++ {
		item := tab.table.Item(r, 0)
		if item == nil {
			continue
		}
		if item.Data(roleShapeIdx).ToInt() == tab.selectedShapeIdx &&
			item.Data(roleDesignIdx).ToInt() == tab.selectedDesignIdx {
			tab.table.SetRangeSelected(qt.NewQTableWidgetSelectionRange2(r, 0, r, 10), true)
		}
	}
}

func (tab *ThicknessTab) refreshTable() {
	tab.syncing = true
	defer func() { tab.syncing = false }()

	wasSorting := tab.table.IsSortingEnabled()
	tab.table.SetSortingEnabled(false)

	totalRows := 0
	for _, s := range tab.report.Results {
		totalRows += len(s.Designs) * 2
	}
	tab.table.SetRowCount(totalRows)

	addItem := func(row, col int, text string, align qt.AlignmentFlag, si, di int) {
		it := qt.NewQTableWidgetItem2(text)
		it.SetTextAlignment(int(align | qt.AlignVCenter))
		if col == 0 {
			it.SetData(roleShapeIdx, qt.NewQVariant4(si))
			it.SetData(roleDesignIdx, qt.NewQVariant4(di))
		}
		tab.table.SetItem(row, col, it)
	}

	r := 0
	for si, s := range tab.report.Results {
		for di, d := range s.Designs {
			eyes := []struct {
				tag  string
				prof lens.EyeProfile
			}{
				{"OD", d.OD},
				{"OS", d.OS},
			}
			for _, e := range eyes {
				addItem(r, 0, s.Shape, qt.AlignLeft, si, di)
				addItem(r, 1, d.Design, qt.AlignLeft, si, di)
				addItem(r, 2, e.tag, qt.AlignHCenter, si, di)
				addItem(r, 3, fmt.Sprintf("+%.2f", e.prof.BaseCurve), qt.AlignRight, si, di)
				addItem(r, 4, fmt.Sprintf("%.2f", e.prof.CenterThickness), qt.AlignRight, si, di)
				addItem(r, 5, fmt.Sprintf("%.2f", e.prof.MaxCaliperThickness), qt.AlignRight, si, di)
				addItem(r, 6, fmt.Sprintf("%.2f", e.prof.MeanCaliperThickness), qt.AlignRight, si, di)
				addItem(r, 7, fmt.Sprintf("%.2f", e.prof.TempHorizontal), qt.AlignRight, si, di)
				addItem(r, 8, fmt.Sprintf("%.2f", e.prof.NasalHorizontal), qt.AlignRight, si, di)
				addItem(r, 9, fmt.Sprintf("%.1f", e.prof.Weight), qt.AlignRight, si, di)
				addItem(r, 10, fmt.Sprintf("%.1f", d.PairWeight), qt.AlignRight, si, di)
				r++
			}
		}
	}

	tab.table.SetSortingEnabled(wasSorting)
}

func (tab *ThicknessTab) ExportReport(path string) error {
	format := lens.FormatJSON
	switch strings.ToLower(filepath.Ext(path)) {
	case ".csv":
		format = lens.FormatCSV
	case ".txt", ".text":
		format = lens.FormatText
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return lens.WriteReport(f, tab.report, format)
}

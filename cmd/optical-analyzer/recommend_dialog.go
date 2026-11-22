package main

import (
	"fmt"

	qt "github.com/mappu/miqt/qt6"

	"jsouthworth.net/go/optical-analyzer/lens"
	"jsouthworth.net/go/optical-analyzer/optics"
)

type priorityItem struct {
	criterion lens.RecommendCriterion
	enabled   bool
}

type prioritySlotWidgets struct {
	check *qt.QCheckBox
	left  *qt.QPushButton
	right *qt.QPushButton
}

func defaultPriorityItems() []priorityItem {
	order := lens.DefaultPriorityOrder()
	items := make([]priorityItem, len(order))
	for i, c := range order {
		items[i] = priorityItem{criterion: c, enabled: true}
	}
	return items
}

func weightsFromPriorityItems(items []priorityItem) lens.RecommendWeights {
	enabled := make([]lens.RecommendCriterion, 0, len(items))
	for _, it := range items {
		if it.enabled {
			enabled = append(enabled, it.criterion)
		}
	}
	return lens.WeightsForPriorityOrder(enabled)
}

func criterionLabelAndWeight(c lens.RecommendCriterion, w lens.RecommendWeights) (label string, tip string, weight float64) {
	switch c {
	case lens.CriterionClarity:
		return "Clarity", "±30° DSM chromatic dispersion clarity (uncheck to ignore)", w.Clarity
	case lens.CriterionThickness:
		return "Thick", "Maximum lens edge/center thickness (uncheck to ignore)", w.Thickness
	case lens.CriterionWeight:
		return "Weight", "Total OD + OS pair weight in grams (uncheck to ignore)", w.Weight
	case lens.CriterionCost:
		return "Cost", "Relative optician single-vision lens cost (uncheck to ignore)", w.Cost
	case lens.CriterionAvailability:
		return "Avail", "Regional optical lab availability (uncheck to ignore)", w.Availability
	default:
		return string(c), "", 0
	}
}

// RecommendWindow is a separate pop-out window for the Lens Material Recommendation engine.
type RecommendWindow struct {
	Dialog *qt.QDialog

	headline      *qt.QLabel
	subLabel      *qt.QLabel
	regionCombo   *qt.QComboBox
	filterCombo   *qt.QComboBox
	applyBtn      *qt.QPushButton
	prioritySlots []prioritySlotWidgets
	table         *qt.QTableWidget

	region   optics.MarketRegion
	filter   optics.MaterialFamily
	priority []priorityItem
	report   lens.RecommendationReport

	syncing        bool
	onPrefsChanged func()
	onApply        func(rec lens.MaterialRecommendation)
}

func NewRecommendWindow(
	parent *qt.QWidget,
	onPrefsChanged func(),
	onApply func(lens.MaterialRecommendation),
) *RecommendWindow {
	rw := &RecommendWindow{
		region:         optics.RegionUS,
		filter:         optics.FamilyAuto,
		priority:       defaultPriorityItems(),
		onPrefsChanged: onPrefsChanged,
		onApply:        onApply,
	}

	rw.Dialog = qt.NewQDialog(parent)
	rw.Dialog.SetWindowTitle("Lens Material Recommendation")
	rw.Dialog.Resize(1080, 560)
	rw.Dialog.SetMinimumSize2(780, 400)
	rw.Dialog.SetWindowFlag2(qt.WindowMinMaxButtonsHint, true)

	lay := qt.NewQVBoxLayout(rw.Dialog.QWidget)
	lay.SetContentsMargins(10, 10, 10, 10)
	lay.SetSpacing(8)

	// Top headline + controls row
	topRow := qt.NewQHBoxLayout2()
	topRow.SetSpacing(6)

	rw.headline = qt.NewQLabel2()
	recFont := qt.NewQFont5(rw.headline.Font())
	recFont.SetBold(true)
	rw.headline.SetFont(recFont)
	rw.headline.SetWordWrap(true)
	topRow.AddWidget2(rw.headline.QWidget, 1)

	rw.regionCombo = qt.NewQComboBox2()
	rw.regionCombo.AddItem("US Market")
	rw.regionCombo.AddItem("EU / Global")
	rw.regionCombo.OnCurrentIndexChanged(func(idx int) {
		if rw.syncing {
			return
		}
		if idx == 1 {
			rw.region = optics.RegionEU
		} else {
			rw.region = optics.RegionUS
		}
		if rw.onPrefsChanged != nil {
			rw.onPrefsChanged()
		}
	})
	topRow.AddWidget(rw.regionCombo.QWidget)

	rw.filterCombo = qt.NewQComboBox2()
	rw.filterCombo.AddItem("All Materials")
	rw.filterCombo.AddItem("Plastic Only")
	rw.filterCombo.AddItem("Glass Only")
	rw.filterCombo.OnCurrentIndexChanged(func(idx int) {
		if rw.syncing {
			return
		}
		switch idx {
		case 1:
			rw.filter = optics.FamilyPlastic
		case 2:
			rw.filter = optics.FamilyGlass
		default:
			rw.filter = optics.FamilyAuto
		}
		if rw.onPrefsChanged != nil {
			rw.onPrefsChanged()
		}
	})
	topRow.AddWidget(rw.filterCombo.QWidget)

	rw.applyBtn = qt.NewQPushButton3("Apply #1")
	rw.applyBtn.OnClicked(func() {
		if len(rw.report.Recommendations) > 0 && rw.onApply != nil {
			rw.onApply(rw.report.Recommendations[0])
		}
	})
	topRow.AddWidget(rw.applyBtn.QWidget)
	lay.AddLayout(topRow.QLayout)

	// Reorderable priority ranking strip
	priorityRow := qt.NewQHBoxLayout2()
	priorityRow.SetSpacing(6)
	priorityRow.AddWidget(qt.NewQLabel3("Priority:").QWidget)

	rw.prioritySlots = make([]prioritySlotWidgets, len(rw.priority))
	for i := range rw.priority {
		idx := i
		chip := qt.NewQFrame2()
		chip.SetFrameShape(qt.QFrame__StyledPanel)
		chipLay := qt.NewQHBoxLayout(chip.QWidget)
		chipLay.SetContentsMargins(4, 2, 4, 2)
		chipLay.SetSpacing(2)

		btnLeft := qt.NewQPushButton3("◀")
		btnLeft.SetFlat(true)
		btnLeft.SetFixedWidth(20)
		btnLeft.SetToolTip("Move higher priority")
		btnLeft.OnClicked(func() {
			if rw.MovePriority(idx, -1) && rw.onPrefsChanged != nil {
				rw.onPrefsChanged()
			}
		})

		cb := qt.NewQCheckBox2()
		cb.OnToggled(func(checked bool) {
			if rw.syncing {
				return
			}
			_ = rw.TogglePriority(idx, checked)
			if rw.onPrefsChanged != nil {
				rw.onPrefsChanged()
			}
		})

		btnRight := qt.NewQPushButton3("▶")
		btnRight.SetFlat(true)
		btnRight.SetFixedWidth(20)
		btnRight.SetToolTip("Move lower priority")
		btnRight.OnClicked(func() {
			if rw.MovePriority(idx, 1) && rw.onPrefsChanged != nil {
				rw.onPrefsChanged()
			}
		})

		chipLay.AddWidget(btnLeft.QWidget)
		chipLay.AddWidget2(cb.QWidget, 1)
		chipLay.AddWidget(btnRight.QWidget)

		rw.prioritySlots[idx] = prioritySlotWidgets{
			check: cb,
			left:  btnLeft,
			right: btnRight,
		}
		priorityRow.AddWidget2(chip.QWidget, 1)
	}

	resetBtn := qt.NewQPushButton3("Reset")
	resetBtn.SetToolTip("Restore default priority order")
	resetBtn.OnClicked(func() {
		rw.priority = defaultPriorityItems()
		if rw.onPrefsChanged != nil {
			rw.onPrefsChanged()
		}
	})
	priorityRow.AddWidget(resetBtn.QWidget)
	lay.AddLayout(priorityRow.QLayout)

	rw.subLabel = qt.NewQLabel2()
	rw.subLabel.SetWordWrap(true)
	lay.AddWidget(rw.subLabel.QWidget)

	// Ranked materials table
	rw.table = qt.NewQTableWidget2()
	rw.table.SetColumnCount(15)
	rw.table.SetHorizontalHeaderLabels([]string{
		"#", "Material", "n", "Abbe", "Avg±30°Δ", "Max±30°Δ", "Thk", "Wt", "Cost", "Clarity", "Thick", "Weight", "Value", "Avail", "Score",
	})
	rw.table.SetSelectionBehavior(qt.QAbstractItemView__SelectRows)
	rw.table.SetSelectionMode(qt.QAbstractItemView__SingleSelection)
	rw.table.SetEditTriggers(qt.QAbstractItemView__NoEditTriggers)
	rw.table.SetAlternatingRowColors(true)
	rw.table.SetShowGrid(false)
	rw.table.VerticalHeader().SetVisible(false)

	recHdr := rw.table.HorizontalHeader()
	recHdr.SetSectionResizeMode2(0, qt.QHeaderView__ResizeToContents)
	recHdr.SetSectionResizeMode2(1, qt.QHeaderView__Stretch)
	for c := 2; c < 15; c++ {
		recHdr.SetSectionResizeMode2(c, qt.QHeaderView__ResizeToContents)
	}

	rw.table.OnCellDoubleClicked(func(row, _ int) {
		recs := rw.report.Recommendations
		if row >= 0 && row < len(recs) && rw.onApply != nil {
			rw.onApply(recs[row])
		}
	})

	lay.AddWidget2(rw.table.QWidget, 1)

	// Bottom bar
	bottomRow := qt.NewQHBoxLayout2()
	bottomRow.AddStretch()
	closeBtn := qt.NewQPushButton3("Close")
	closeBtn.OnClicked(func() { rw.Dialog.Hide() })
	bottomRow.AddWidget(closeBtn.QWidget)
	lay.AddLayout(bottomRow.QLayout)

	return rw
}

func (rw *RecommendWindow) ShowAndRaise() {
	rw.Dialog.Show()
	rw.Dialog.Raise()
	rw.Dialog.ActivateWindow()
}

func (rw *RecommendWindow) Options(odCorMM, osCorMM float64) lens.RecommendOptions {
	return lens.RecommendOptions{
		ODCenterOfRotationMM: odCorMM,
		OSCenterOfRotationMM: osCorMM,
		FamilyFilter:         rw.filter,
		Region:               rw.region,
		Weights:              weightsFromPriorityItems(rw.priority),
	}
}

func (rw *RecommendWindow) MovePriority(idx, delta int) bool {
	target := idx + delta
	if idx < 0 || idx >= len(rw.priority) || target < 0 || target >= len(rw.priority) {
		return false
	}
	rw.priority[idx], rw.priority[target] = rw.priority[target], rw.priority[idx]
	return true
}

func (rw *RecommendWindow) TogglePriority(idx int, enabled bool) bool {
	if idx < 0 || idx >= len(rw.priority) {
		return false
	}
	if !enabled {
		active := 0
		for _, it := range rw.priority {
			if it.enabled {
				active++
			}
		}
		if active <= 1 && rw.priority[idx].enabled {
			return false
		}
	}
	rw.priority[idx].enabled = enabled
	return true
}

func (rw *RecommendWindow) syncPrioritySlots(weights lens.RecommendWeights) {
	rw.syncing = true
	defer func() { rw.syncing = false }()

	n := len(rw.priority)
	activeRank := 0
	for i, it := range rw.priority {
		if i >= len(rw.prioritySlots) {
			break
		}
		slot := rw.prioritySlots[i]
		lbl, tip, wt := criterionLabelAndWeight(it.criterion, weights)
		slot.check.SetChecked(it.enabled)
		if it.enabled {
			activeRank++
			slot.check.SetText(fmt.Sprintf("%d. %s", activeRank, lbl))
			slot.check.SetToolTip(fmt.Sprintf("%s (%.0f%% weight)", tip, wt*100))
		} else {
			slot.check.SetText(lbl)
			slot.check.SetToolTip(tip)
		}
		slot.left.SetEnabled(i > 0)
		slot.right.SetEnabled(i < n-1)
	}
}

func (rw *RecommendWindow) Refresh(rec lens.RecommendationReport) {
	rw.report = rec
	recs := rec.Recommendations
	rw.table.SetRowCount(len(recs))
	rw.applyBtn.SetEnabled(len(recs) > 0)

	weights := rec.Weights
	if len(recs) == 0 {
		weights = weightsFromPriorityItems(rw.priority)
	}
	rw.syncPrioritySlots(weights)

	if len(recs) == 0 {
		rw.headline.SetText("Enter valid frame and prescription values to see material recommendations.")
		rw.subLabel.SetText("")
		return
	}

	best := recs[0]
	regionTag := "US"
	if rec.Region == optics.RegionEU {
		regionTag = "EU/Global"
	}
	rw.headline.SetText(fmt.Sprintf("Recommended: %s  •  Score %.1f/100  (±%.0f° %.4f Δ, %.2f mm, %.1f g, %.1f×, Avail %.0f)",
		best.Name, best.TotalScore, rec.MaxGaze, best.MeanDispersion, best.MaxThickness, best.PairWeight, best.RelativeCost, best.AvailabilityScore))
	rw.subLabel.SetText(fmt.Sprintf("For %s / %s (%s market)  •  DSM ±%.0f° chromatic dispersion  •  Double-click any row to apply.",
		rec.Shape, rec.Design, regionTag, rec.MaxGaze))

	addRecItem := func(row, col int, text string, align qt.AlignmentFlag, bold bool) {
		it := qt.NewQTableWidgetItem2(text)
		it.SetTextAlignment(int(align | qt.AlignVCenter))
		if bold {
			f := qt.NewQFont5(rw.table.Font())
			f.SetBold(true)
			it.SetFont(f)
		}
		rw.table.SetItem(row, col, it)
	}

	for r, item := range recs {
		bold := r == 0
		addRecItem(r, 0, fmt.Sprintf("%d", item.Rank), qt.AlignHCenter, bold)
		addRecItem(r, 1, item.Name, qt.AlignLeft, bold)
		addRecItem(r, 2, fmt.Sprintf("%.3f", item.Index), qt.AlignRight, bold)
		addRecItem(r, 3, fmt.Sprintf("%d", item.Abbe), qt.AlignRight, bold)
		addRecItem(r, 4, fmt.Sprintf("%.4f", item.MeanDispersion), qt.AlignRight, bold)
		addRecItem(r, 5, fmt.Sprintf("%.4f", item.MaxDispersion), qt.AlignRight, bold)
		addRecItem(r, 6, fmt.Sprintf("%.2f", item.MaxThickness), qt.AlignRight, bold)
		addRecItem(r, 7, fmt.Sprintf("%.1f", item.PairWeight), qt.AlignRight, bold)
		addRecItem(r, 8, fmt.Sprintf("%.1f×", item.RelativeCost), qt.AlignRight, bold)
		addRecItem(r, 9, fmt.Sprintf("%.1f", item.ClarityScore), qt.AlignRight, bold)
		addRecItem(r, 10, fmt.Sprintf("%.1f", item.ThicknessScore), qt.AlignRight, bold)
		addRecItem(r, 11, fmt.Sprintf("%.1f", item.WeightScore), qt.AlignRight, bold)
		addRecItem(r, 12, fmt.Sprintf("%.1f", item.CostScore), qt.AlignRight, bold)
		addRecItem(r, 13, fmt.Sprintf("%.0f", item.AvailabilityScore), qt.AlignRight, bold)
		addRecItem(r, 14, fmt.Sprintf("%.1f", item.TotalScore), qt.AlignRight, bold)
	}
}

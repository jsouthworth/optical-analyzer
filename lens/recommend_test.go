package lens

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"math"
	"strings"
	"testing"

	"jsouthworth.net/go/optical-analyzer/optics"
)

func TestRecommendMaterialsOrderingAndWeights(t *testing.T) {
	shapes, _ := ResolveShapes("squircle")
	designs, _ := ResolveDesigns("aspheric")

	t.Run("high-minus Rx evaluates all 12 materials and weights clarity > thickness > weight > cost", func(t *testing.T) {
		inputs := ReportInputs{
			OD:         EyePrescription{Sphere: -6.00, Cyl: -1.00, Axis: 90, PD: 31.0, OCHeight: 21.0},
			OS:         EyePrescription{Sphere: -5.50, Cyl: -0.75, Axis: 85, PD: 30.5, OCHeight: 21.0},
			Chamfer:    0.25,
			BevelDepth: optics.DefaultBevelDepthMM,
			Frame:      FrameSpec{A: 52, B: 40, DBL: 18},
		}

		rec := RecommendMaterials(inputs, shapes[0], designs[0], RecommendOptions{})
		if len(rec.Recommendations) != 12 {
			t.Fatalf("got %d recommendations, want 12", len(rec.Recommendations))
		}
		if rec.MaxGaze != 30.0 {
			t.Errorf("MaxGaze = %v, want 30", rec.MaxGaze)
		}
		if !(rec.Weights.Clarity > rec.Weights.Thickness &&
			rec.Weights.Thickness > rec.Weights.Weight &&
			rec.Weights.Weight > rec.Weights.Cost &&
			rec.Weights.Cost > 0 &&
			rec.Weights.Availability > 0) {
			t.Errorf("unexpected default weights: %+v", rec.Weights)
		}
		if rec.Region != optics.RegionUS {
			t.Errorf("default Region = %q, want us", rec.Region)
		}

		for i, r := range rec.Recommendations {
			if r.Rank != i+1 {
				t.Errorf("row %d rank = %d, want %d", i, r.Rank, i+1)
			}
			if r.Key == "" {
				t.Errorf("row %d missing catalog Key", i)
			}
			if i > 0 && r.TotalScore > rec.Recommendations[i-1].TotalScore+1e-9 {
				t.Errorf("recommendations not sorted descending at index %d: %.3f > %.3f",
					i, r.TotalScore, rec.Recommendations[i-1].TotalScore)
			}
			if r.MeanDispersion <= 0 || r.MaxDispersion < r.MeanDispersion {
				t.Errorf("%s invalid dispersion: mean=%v max=%v", r.Name, r.MeanDispersion, r.MaxDispersion)
			}
		}

		byKey := make(map[string]MaterialRecommendation, len(rec.Recommendations))
		for _, r := range rec.Recommendations {
			byKey[r.Key] = r
		}
		cr39 := byKey["1.50-plastic"]
		poly := byKey["1.59-plastic"]
		g170 := byKey["1.70-glass"]
		g190 := byKey["1.90-glass"]

		if !(cr39.MeanDispersion < poly.MeanDispersion && cr39.ClarityScore > poly.ClarityScore) {
			t.Errorf("expected CR-39 dispersion (%.4f, clarity %.1f) better than Poly (%.4f, clarity %.1f)",
				cr39.MeanDispersion, cr39.ClarityScore, poly.MeanDispersion, poly.ClarityScore)
		}
		if !(g170.MeanDispersion < g190.MeanDispersion) {
			t.Errorf("expected 1.70 glass dispersion (%.4f) < 1.90 glass (%.4f)",
				g170.MeanDispersion, g190.MeanDispersion)
		}
		if !(g190.MaxThickness < cr39.MaxThickness && g190.PairWeight > poly.PairWeight) {
			t.Errorf("expected 1.90 glass thinner than CR-39 and heavier than Poly: 1.90(thk=%.2f wt=%.1f) cr39(thk=%.2f) poly(wt=%.1f)",
				g190.MaxThickness, g190.PairWeight, cr39.MaxThickness, poly.PairWeight)
		}
		if cr39.RelativeCost != 1.0 || cr39.CostScore != 100.0 || !(g190.RelativeCost > cr39.RelativeCost && cr39.CostScore > g190.CostScore) {
			t.Errorf("unexpected relative cost scoring: cr39(cost=%.1fx score=%.1f) g190(cost=%.1fx score=%.1f)",
				cr39.RelativeCost, cr39.CostScore, g190.RelativeCost, g190.CostScore)
		}
		if !(cr39.AvailabilityScore == 100 && g190.AvailabilityScore < 20 && byKey["1.70-plastic"].AvailabilityScore <= 15) {
			t.Errorf("unexpected US availability: cr39=%.0f g190=%.0f eyry=%.0f",
				cr39.AvailabilityScore, g190.AvailabilityScore, byKey["1.70-plastic"].AvailabilityScore)
		}

		recEU := RecommendMaterials(inputs, shapes[0], designs[0], RecommendOptions{Region: optics.RegionEU})
		euByKey := make(map[string]MaterialRecommendation, len(recEU.Recommendations))
		for _, r := range recEU.Recommendations {
			euByKey[r.Key] = r
		}
		if !(euByKey["1.90-glass"].AvailabilityScore > g190.AvailabilityScore &&
			euByKey["1.70-plastic"].AvailabilityScore > byKey["1.70-plastic"].AvailabilityScore) {
			t.Errorf("expected EU availability higher than US for 1.90 glass and 1.70 EYRY")
		}
	})

	t.Run("plano meniscus lenses have negligible shape-factor dispersion and near-perfect clarity without NaN", func(t *testing.T) {
		inputs := ReportInputs{
			OD:         EyePrescription{Sphere: 0, PD: 35},
			OS:         EyePrescription{Sphere: 0, PD: 35},
			BevelDepth: optics.DefaultBevelDepthMM,
			Frame:      FrameSpec{A: 52, B: 40, DBL: 18},
		}
		rec := RecommendMaterials(inputs, shapes[0], designs[0], RecommendOptions{})
		for _, r := range rec.Recommendations {
			if math.IsNaN(r.TotalScore) || r.MeanDispersion >= 0.02 || r.ClarityScore < 95.0 {
				t.Errorf("plano %s got disp=%v clarity=%v total=%v", r.Name, r.MeanDispersion, r.ClarityScore, r.TotalScore)
			}
		}
	})

	t.Run("material family filter restricts to plastic or glass only", func(t *testing.T) {
		inputs := ReportInputs{
			OD:    EyePrescription{Sphere: -4.0},
			OS:    EyePrescription{Sphere: -4.0},
			Frame: FrameSpec{A: 50, B: 38, DBL: 18},
		}
		plastics := RecommendMaterials(inputs, shapes[0], designs[0], RecommendOptions{FamilyFilter: optics.FamilyPlastic})
		if len(plastics.Recommendations) != 7 {
			t.Fatalf("plastic filter count = %d, want 7", len(plastics.Recommendations))
		}
		for _, r := range plastics.Recommendations {
			if r.Family != optics.FamilyPlastic {
				t.Errorf("non-plastic in plastic filter: %+v", r)
			}
		}

		glasses := RecommendMaterials(inputs, shapes[0], designs[0], RecommendOptions{FamilyFilter: optics.FamilyGlass})
		if len(glasses.Recommendations) != 5 {
			t.Fatalf("glass filter count = %d, want 5", len(glasses.Recommendations))
		}
		for _, r := range glasses.Recommendations {
			if r.Family != optics.FamilyGlass {
				t.Errorf("non-glass in glass filter: %+v", r)
			}
		}
	})

	t.Run("text, JSON, and CSV formatters succeed", func(t *testing.T) {
		inputs := ReportInputs{
			OD:    EyePrescription{Sphere: -4.0},
			OS:    EyePrescription{Sphere: -4.0},
			Frame: FrameSpec{A: 52, B: 40, DBL: 18},
		}
		rec := RecommendMaterials(inputs, shapes[0], designs[0], RecommendOptions{})

		var txtBuf, jsonBuf, csvBuf bytes.Buffer
		if err := WriteRecommendationReport(&txtBuf, rec, FormatText); err != nil {
			t.Fatalf("WriteRecommendationReport(text): %v", err)
		}
		if !strings.Contains(txtBuf.String(), "Recommended:") {
			t.Errorf("text report missing Recommended summary line")
		}

		if err := WriteRecommendationReport(&jsonBuf, rec, FormatJSON); err != nil {
			t.Fatalf("WriteRecommendationReport(json): %v", err)
		}
		var decoded RecommendationReport
		if err := json.Unmarshal(jsonBuf.Bytes(), &decoded); err != nil {
			t.Fatalf("json.Unmarshal: %v", err)
		}
		if len(decoded.Recommendations) != 12 || decoded.Recommendations[0].Key == "" {
			t.Errorf("JSON recommendations invalid: len=%d key=%q", len(decoded.Recommendations), decoded.Recommendations[0].Key)
		}

		if err := WriteRecommendationReport(&csvBuf, rec, FormatCSV); err != nil {
			t.Fatalf("WriteRecommendationReport(csv): %v", err)
		}
		rows, err := csv.NewReader(&csvBuf).ReadAll()
		if err != nil {
			t.Fatalf("csv.ReadAll: %v", err)
		}
		if len(rows) != 13 {
			t.Errorf("CSV row count = %d, want 13", len(rows))
		}
	})

	t.Run("priority ranking parses and normalizes cleanly when subset enabled", func(t *testing.T) {
		order, err := ParsePriorityOrder("clarity,weight,thickness")
		if err != nil {
			t.Fatalf("ParsePriorityOrder: %v", err)
		}
		w := WeightsForPriorityOrder(order)
		if !almostEqual(w.Clarity, 0.50, 1e-9) ||
			!almostEqual(w.Weight, 0.3125, 1e-9) ||
			!almostEqual(w.Thickness, 0.1875, 1e-9) ||
			w.Cost != 0 || w.Availability != 0 {
			t.Errorf("unexpected 3-criterion weights: %+v", w)
		}
		if _, err := ParsePriorityOrder("clarity,bogus"); err == nil {
			t.Errorf("expected error for invalid criterion")
		}
	})
}

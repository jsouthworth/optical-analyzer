package lens

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"

	"jsouthworth.net/go/optical-analyzer/contour"
	"jsouthworth.net/go/optical-analyzer/dsm"
	"jsouthworth.net/go/optical-analyzer/optics"
)

const (
	// DefaultRecommendMaxGazeDeg is the half-angle (degrees on either side of
	// the optical center) used to evaluate chromatic dispersion clarity.
	DefaultRecommendMaxGazeDeg = 30.0

	// DefaultWeightClarity is the default scoring weight for ±30° chromatic dispersion clarity (rank 1).
	DefaultWeightClarity = 0.40
	// DefaultWeightThickness is the default scoring weight for maximum lens thickness (rank 2).
	DefaultWeightThickness = 0.25
	// DefaultWeightMass is the default scoring weight for lens pair weight (rank 3).
	DefaultWeightMass = 0.15
	// DefaultWeightCost is the default scoring weight for relative material cost (rank 4).
	DefaultWeightCost = 0.10
	// DefaultWeightAvailability is the default scoring weight for regional lab availability (rank 5).
	DefaultWeightAvailability = 0.10

	dispersionFloorPD = 0.125 // prism diopters (standard lateral color threshold)
	weightFloorGrams  = 10.0  // grams per pair
	thicknessFloorMM  = 2.0   // millimeters
	costFloorRel      = 2.0   // relative cost multiplier (CR-39 = 1.0x)
)

// RecommendCriterion identifies one of the five lens recommendation criteria.
type RecommendCriterion string

const (
	// CriterionClarity ranks materials by DSM ±30° lateral chromatic dispersion clarity.
	CriterionClarity RecommendCriterion = "clarity"
	// CriterionThickness ranks materials by maximum edge/center caliper thickness.
	CriterionThickness RecommendCriterion = "thickness"
	// CriterionWeight ranks materials by finished lens pair weight in grams.
	CriterionWeight RecommendCriterion = "weight"
	// CriterionCost ranks materials by relative single-vision lens pair cost.
	CriterionCost RecommendCriterion = "cost"
	// CriterionAvailability ranks materials by regional optical lab stock availability.
	CriterionAvailability RecommendCriterion = "availability"
)

// DefaultPriorityOrder returns the default ranked priority order from highest to lowest.
func DefaultPriorityOrder() []RecommendCriterion {
	return []RecommendCriterion{
		CriterionClarity,
		CriterionThickness,
		CriterionWeight,
		CriterionCost,
		CriterionAvailability,
	}
}

// WeightsForPriorityOrder converts an ordered list of enabled criteria (highest
// to lowest priority) into normalized RecommendWeights using the standard rank
// schedule (40, 25, 15, 10, 10). Omitted criteria receive weight 0.
func WeightsForPriorityOrder(order []RecommendCriterion) RecommendWeights {
	rankWeights := []float64{
		DefaultWeightClarity,
		DefaultWeightThickness,
		DefaultWeightMass,
		DefaultWeightCost,
		DefaultWeightAvailability,
	}
	var w RecommendWeights
	seen := make(map[RecommendCriterion]bool, len(order))
	rank := 0
	for _, c := range order {
		if seen[c] || rank >= len(rankWeights) {
			continue
		}
		seen[c] = true
		rw := rankWeights[rank]
		rank++
		switch c {
		case CriterionClarity:
			w.Clarity = rw
		case CriterionThickness:
			w.Thickness = rw
		case CriterionWeight:
			w.Weight = rw
		case CriterionCost:
			w.Cost = rw
		case CriterionAvailability:
			w.Availability = rw
		}
	}
	return normalizeWeights(w)
}

// ParsePriorityOrder parses a comma-separated list of criteria in priority order
// (e.g., "clarity,thickness,weight,cost,availability").
func ParsePriorityOrder(input string) ([]RecommendCriterion, error) {
	if strings.TrimSpace(input) == "" {
		return DefaultPriorityOrder(), nil
	}
	parts := strings.Split(input, ",")
	order := make([]RecommendCriterion, 0, len(parts))
	seen := make(map[RecommendCriterion]bool, len(parts))
	for _, raw := range parts {
		tok := strings.ToLower(strings.TrimSpace(raw))
		if tok == "" {
			continue
		}
		var c RecommendCriterion
		switch tok {
		case "clarity", "abbe", "dispersion":
			c = CriterionClarity
		case "thickness", "thick", "thin":
			c = CriterionThickness
		case "weight", "mass", "wt":
			c = CriterionWeight
		case "cost", "price", "value":
			c = CriterionCost
		case "availability", "avail":
			c = CriterionAvailability
		default:
			return nil, fmt.Errorf("invalid priority criterion %q: use clarity, thickness, weight, cost, or availability", raw)
		}
		if !seen[c] {
			seen[c] = true
			order = append(order, c)
		}
	}
	if len(order) == 0 {
		return nil, fmt.Errorf("priority list must include at least one criterion")
	}
	return order, nil
}

// RecommendWeights holds relative weights for the five scoring criteria.
type RecommendWeights struct {
	Clarity      float64 `json:"clarity"`
	Thickness    float64 `json:"thickness"`
	Weight       float64 `json:"weight"`
	Cost         float64 `json:"cost"`
	Availability float64 `json:"availability"`
}

func normalizeWeights(w RecommendWeights) RecommendWeights {
	if w.Clarity <= 0 && w.Weight <= 0 && w.Thickness <= 0 && w.Cost <= 0 && w.Availability <= 0 {
		return RecommendWeights{
			Clarity:      DefaultWeightClarity,
			Thickness:    DefaultWeightThickness,
			Weight:       DefaultWeightMass,
			Cost:         DefaultWeightCost,
			Availability: DefaultWeightAvailability,
		}
	}
	if w.Clarity < 0 {
		w.Clarity = 0
	}
	if w.Thickness < 0 {
		w.Thickness = 0
	}
	if w.Weight < 0 {
		w.Weight = 0
	}
	if w.Cost < 0 {
		w.Cost = 0
	}
	if w.Availability < 0 {
		w.Availability = 0
	}
	sum := w.Clarity + w.Thickness + w.Weight + w.Cost + w.Availability
	if sum <= 0 {
		return RecommendWeights{
			Clarity:      DefaultWeightClarity,
			Thickness:    DefaultWeightThickness,
			Weight:       DefaultWeightMass,
			Cost:         DefaultWeightCost,
			Availability: DefaultWeightAvailability,
		}
	}
	return RecommendWeights{
		Clarity:      w.Clarity / sum,
		Thickness:    w.Thickness / sum,
		Weight:       w.Weight / sum,
		Cost:         w.Cost / sum,
		Availability: w.Availability / sum,
	}
}

// RecommendOptions configures lens material recommendation scoring.
// All center-of-rotation distances are expressed in millimeters (mm).
// RecommendOptions configures lens material recommendation scoring. All
// center-of-rotation distances are expressed in millimeters (default 27.0 mm).
type RecommendOptions struct {
	MaxGazeDeg           float64               // Half-angle from optical center (default 30°)
	CenterOfRotationMM   float64               // Shared eye center-of-rotation distance in mm (default 27.0 mm)
	ODCenterOfRotationMM float64               // Optional per-eye OD center-of-rotation distance in mm
	OSCenterOfRotationMM float64               // Optional per-eye OS center-of-rotation distance in mm
	Weights              RecommendWeights      // Scoring weights
	FamilyFilter         optics.MaterialFamily // FamilyAuto = all, FamilyPlastic = plastic only, FamilyGlass = glass only
	Region               optics.MarketRegion   // RegionUS (default) or RegionEU
}

// MaterialRecommendation holds evaluated optical clarity, mass, thickness,
// relative cost, market availability, and weighted scores for one candidate material.
type MaterialRecommendation struct {
	Rank              int                   `json:"rank"`
	Key               string                `json:"key"`
	Name              string                `json:"name"`
	Index             float64               `json:"index"`
	Family            optics.MaterialFamily `json:"family"`
	Abbe              int                   `json:"abbe"`
	SpecificGravity   float64               `json:"specific_gravity"`
	RelativeCost      float64               `json:"relative_cost"`
	MeanDispersion    float64               `json:"mean_dispersion"`
	MaxDispersion     float64               `json:"max_dispersion"`
	ODMeanDispersion  float64               `json:"od_mean_dispersion"`
	OSMeanDispersion  float64               `json:"os_mean_dispersion"`
	PairWeight        float64               `json:"pair_weight"`
	MaxThickness      float64               `json:"max_thickness"`
	MaxEdgeThickness  float64               `json:"max_edge_thickness"`
	ODCenterThickness float64               `json:"od_center_thickness"`
	OSCenterThickness float64               `json:"os_center_thickness"`
	ClarityScore      float64               `json:"clarity_score"`
	ThicknessScore    float64               `json:"thickness_score"`
	WeightScore       float64               `json:"weight_score"`
	CostScore         float64               `json:"cost_score"`
	AvailabilityScore float64               `json:"availability_score"`
	TotalScore        float64               `json:"total_score"`
	OD                EyeProfile            `json:"od"`
	OS                EyeProfile            `json:"os"`
}

// RecommendationReport holds the ranked material recommendations for a given
// prescription, frame shape, and lens design.
type RecommendationReport struct {
	Shape            string                   `json:"shape"`
	Design           string                   `json:"design"`
	Region           optics.MarketRegion      `json:"region"`
	MaxGaze          float64                  `json:"max_gaze"`
	CenterOfRotation float64                  `json:"center_of_rotation"`
	Weights          RecommendWeights         `json:"weights"`
	Recommendations  []MaterialRecommendation `json:"recommendations"`
}

func eyeDispersionWithinAngle(m dsm.DSM, constringence int, c contour.Contour, ocX, ocY, maxGazeDeg float64) (meanPD, maxPD float64) {
	if constringence <= 0 || maxGazeDeg <= 0 || m.S <= 0 {
		return 0, 0
	}
	rMax := m.DegreeDistance(maxGazeDeg)
	if rMax <= 0 {
		return 0, 0
	}

	const (
		angleSteps  = 72
		radialSteps = 15
	)
	dAngle := 360.0 / float64(angleSteps)
	du := 1.0 / float64(radialSteps)

	var sumCA, sumW float64
	var peakCA float64

	sample := func(clipToContour bool) {
		sumCA, sumW, peakCA = 0, 0, 0
		for j := 0; j < radialSteps; j++ {
			u := (float64(j) + 0.5) * du
			r := u * rMax
			for i := 0; i < angleSteps; i++ {
				meridian := float64(i) * dAngle
				rad := meridian * (math.Pi / 180.0)
				if clipToContour {
					px := ocX + r*math.Cos(rad)
					py := ocY + r*math.Sin(rad)
					if !c.Contains(px, py) {
						continue
					}
				}
				ca := dsm.LateralColorSpread(m, r, meridian, constringence)
				sumCA += ca * u
				sumW += u
				if ca > peakCA {
					peakCA = ca
				}
			}
		}
	}

	sample(true)
	if sumW == 0 {
		sample(false)
	}
	if sumW == 0 {
		return 0, 0
	}
	return sumCA / sumW, peakCA
}

// RecommendMaterials evaluates candidate lens materials for the given inputs,
// shape, and optical design, ranking them by clarity, thickness, weight, cost, and availability.
func RecommendMaterials(inputs ReportInputs, shape ShapeOption, design optics.LensDesign, opts RecommendOptions) RecommendationReport {
	maxGaze := opts.MaxGazeDeg
	if maxGaze <= 0 {
		maxGaze = DefaultRecommendMaxGazeDeg
	}
	sharedCorMM := opts.CenterOfRotationMM
	if sharedCorMM <= 0 {
		sharedCorMM = optics.DefaultCenterOfRotationMM
	}
	odCorMM := opts.ODCenterOfRotationMM
	if odCorMM <= 0 {
		odCorMM = sharedCorMM
	}
	osCorMM := opts.OSCenterOfRotationMM
	if osCorMM <= 0 {
		osCorMM = sharedCorMM
	}
	avgCorMM := (odCorMM + osCorMM) / 2.0

	region := opts.Region
	if region == "" {
		region = optics.RegionUS
	}
	weights := normalizeWeights(opts.Weights)

	if inputs.OD.PD <= 0 {
		inputs.OD.PD = (inputs.Frame.A + inputs.Frame.DBL) / 2.0
	}
	if inputs.OS.PD <= 0 {
		inputs.OS.PD = (inputs.Frame.A + inputs.Frame.DBL) / 2.0
	}
	if inputs.OD.OCHeight <= 0 {
		inputs.OD.OCHeight = inputs.Frame.B / 2.0
	}
	if inputs.OS.OCHeight <= 0 {
		inputs.OS.OCHeight = inputs.Frame.B / 2.0
	}
	if inputs.Chamfer < 0 {
		inputs.Chamfer = 0
	}
	if inputs.BevelDepth < 0 {
		inputs.BevelDepth = 0
	}

	frameCenter := (inputs.Frame.A + inputs.Frame.DBL) / 2.0
	odOCX := frameCenter - inputs.OD.PD
	osOCX := -(frameCenter - inputs.OS.PD)
	odOCY := inputs.OD.OCHeight - inputs.Frame.B/2.0
	osOCY := inputs.OS.OCHeight - inputs.Frame.B/2.0

	odGroove, osGroove := ResolveContours(shape, inputs.Frame.A, inputs.Frame.B)
	odContour, osContour := odGroove, osGroove
	if inputs.BevelDepth > 0 {
		odContour = contour.Offset(odGroove, -inputs.BevelDepth)
		osContour = contour.Offset(osGroove, -inputs.BevelDepth)
	}

	catalog := optics.StandardMaterials()
	candidates := make([]optics.MaterialSpec, 0, len(catalog))
	for _, m := range catalog {
		if opts.FamilyFilter == optics.FamilyPlastic && m.Family != optics.FamilyPlastic {
			continue
		}
		if opts.FamilyFilter == optics.FamilyGlass && m.Family != optics.FamilyGlass {
			continue
		}
		candidates = append(candidates, m)
	}

	recs := make([]MaterialRecommendation, 0, len(candidates))
	minDisp, maxDisp := math.Inf(1), math.Inf(-1)
	minWt, maxWt := math.Inf(1), math.Inf(-1)
	minThick, maxThick := math.Inf(1), math.Inf(-1)
	minCost, maxCost := math.Inf(1), math.Inf(-1)

	dsmRX := optics.RX{
		OD: optics.Refraction{
			Sph:  inputs.OD.Sphere,
			Cyl:  inputs.OD.Cyl,
			Axis: inputs.OD.Axis,
		},
		OS: optics.Refraction{
			Sph:  inputs.OS.Sphere,
			Cyl:  inputs.OS.Cyl,
			Axis: inputs.OS.Axis,
		},
	}
	dsmFit := optics.Fit{
		OD: optics.EyeFit{CenterOfRotation: odCorMM},
		OS: optics.EyeFit{CenterOfRotation: osCorMM},
	}

	odBC := inputs.OD.InputBaseCurve
	if odBC <= 0 {
		odBC = inputs.InputBaseCurve
	}
	osBC := inputs.OS.InputBaseCurve
	if osBC <= 0 {
		osBC = inputs.InputBaseCurve
	}
	odCT := inputs.OD.InputCenterThickness
	if odCT <= 0 {
		odCT = inputs.InputCenterThickness
	}
	osCT := inputs.OS.InputCenterThickness
	if osCT <= 0 {
		osCT = inputs.InputCenterThickness
	}

	for _, mat := range candidates {
		odProf := CalcEyeProfile(optics.EyeGeometry{
			IsOD:            true,
			Sphere:          inputs.OD.Sphere,
			Cyl:             inputs.OD.Cyl,
			Axis:            inputs.OD.Axis,
			BaseCurve:       odBC,
			FrameA:          inputs.Frame.A,
			FrameB:          inputs.Frame.B,
			FrameDBL:        inputs.Frame.DBL,
			MonoPD:          inputs.OD.PD,
			OCHeight:        inputs.OD.OCHeight,
			Index:           mat.Index,
			Family:          mat.Family,
			SpecificGravity: mat.SpecificGravity,
			CenterThickness: odCT,
			ChamferMM:       inputs.Chamfer,
			Contour:         &odContour,
			Design:          design,
		})
		osProf := CalcEyeProfile(optics.EyeGeometry{
			IsOD:            false,
			Sphere:          inputs.OS.Sphere,
			Cyl:             inputs.OS.Cyl,
			Axis:            inputs.OS.Axis,
			BaseCurve:       osBC,
			FrameA:          inputs.Frame.A,
			FrameB:          inputs.Frame.B,
			FrameDBL:        inputs.Frame.DBL,
			MonoPD:          inputs.OS.PD,
			OCHeight:        inputs.OS.OCHeight,
			Index:           mat.Index,
			Family:          mat.Family,
			SpecificGravity: mat.SpecificGravity,
			CenterThickness: osCT,
			ChamferMM:       inputs.Chamfer,
			Contour:         &osContour,
			Design:          design,
		})

		odDSM, osDSM := dsm.MakeDSM(optics.Data{
			RX: dsmRX,
			Lenses: optics.LensPair{
				OD: optics.LensParameters{
					Index:           mat.Index,
					CenterThickness: odProf.CenterThickness,
					BaseCurve:       odProf.BaseCurve,
					Abbe:            mat.Abbe,
				},
				OS: optics.LensParameters{
					Index:           mat.Index,
					CenterThickness: osProf.CenterThickness,
					BaseCurve:       osProf.BaseCurve,
					Abbe:            mat.Abbe,
				},
			},
			Fit: dsmFit,
		})

		odMeanCA, odPeakCA := eyeDispersionWithinAngle(odDSM, mat.Abbe, odContour, odOCX, odOCY, maxGaze)
		osMeanCA, osPeakCA := eyeDispersionWithinAngle(osDSM, mat.Abbe, osContour, osOCX, osOCY, maxGaze)
		meanCA := (odMeanCA + osMeanCA) / 2.0
		peakCA := math.Max(odPeakCA, osPeakCA)

		pairWeight := odProf.Weight + osProf.Weight
		maxEdge := math.Max(odProf.MaxCaliperThickness, osProf.MaxCaliperThickness)
		maxOverallThick := math.Max(maxEdge, math.Max(odProf.CenterThickness, osProf.CenterThickness))

		if meanCA < minDisp {
			minDisp = meanCA
		}
		if meanCA > maxDisp {
			maxDisp = meanCA
		}
		if pairWeight < minWt {
			minWt = pairWeight
		}
		if pairWeight > maxWt {
			maxWt = pairWeight
		}
		if maxOverallThick < minThick {
			minThick = maxOverallThick
		}
		if maxOverallThick > maxThick {
			maxThick = maxOverallThick
		}
		if mat.RelativeCost < minCost {
			minCost = mat.RelativeCost
		}
		if mat.RelativeCost > maxCost {
			maxCost = mat.RelativeCost
		}

		avail := mat.AvailabilityUS
		if region == optics.RegionEU {
			avail = mat.AvailabilityEU
		}

		recs = append(recs, MaterialRecommendation{
			Key:               mat.Key,
			Name:              mat.Name,
			Index:             mat.Index,
			Family:            mat.Family,
			Abbe:              mat.Abbe,
			SpecificGravity:   mat.SpecificGravity,
			RelativeCost:      mat.RelativeCost,
			MeanDispersion:    meanCA,
			MaxDispersion:     peakCA,
			ODMeanDispersion:  odMeanCA,
			OSMeanDispersion:  osMeanCA,
			PairWeight:        pairWeight,
			MaxThickness:      maxOverallThick,
			MaxEdgeThickness:  maxEdge,
			ODCenterThickness: odProf.CenterThickness,
			OSCenterThickness: osProf.CenterThickness,
			AvailabilityScore: avail,
			OD:                odProf,
			OS:                osProf,
		})
	}

	scoreLowerBetter := func(val, minVal, maxVal, floor float64) float64 {
		span := maxVal - minVal
		if span < floor {
			span = floor
		}
		if span <= 0 {
			return 100.0
		}
		s := 100.0 * (1.0 - (val-minVal)/span)
		if s < 0 {
			return 0
		}
		if s > 100 {
			return 100
		}
		return s
	}

	for i := range recs {
		r := &recs[i]
		r.ClarityScore = scoreLowerBetter(r.MeanDispersion, minDisp, maxDisp, dispersionFloorPD)
		r.ThicknessScore = scoreLowerBetter(r.MaxThickness, minThick, maxThick, thicknessFloorMM)
		r.WeightScore = scoreLowerBetter(r.PairWeight, minWt, maxWt, weightFloorGrams)
		r.CostScore = scoreLowerBetter(r.RelativeCost, minCost, maxCost, costFloorRel)
		r.TotalScore = weights.Clarity*r.ClarityScore +
			weights.Thickness*r.ThicknessScore +
			weights.Weight*r.WeightScore +
			weights.Cost*r.CostScore +
			weights.Availability*r.AvailabilityScore
	}

	sort.SliceStable(recs, func(i, j int) bool {
		if math.Abs(recs[i].TotalScore-recs[j].TotalScore) > 1e-6 {
			return recs[i].TotalScore > recs[j].TotalScore
		}
		if math.Abs(recs[i].ClarityScore-recs[j].ClarityScore) > 1e-6 {
			return recs[i].ClarityScore > recs[j].ClarityScore
		}
		if math.Abs(recs[i].MaxThickness-recs[j].MaxThickness) > 1e-6 {
			return recs[i].MaxThickness < recs[j].MaxThickness
		}
		if math.Abs(recs[i].PairWeight-recs[j].PairWeight) > 1e-6 {
			return recs[i].PairWeight < recs[j].PairWeight
		}
		if math.Abs(recs[i].RelativeCost-recs[j].RelativeCost) > 1e-6 {
			return recs[i].RelativeCost < recs[j].RelativeCost
		}
		return recs[i].AvailabilityScore > recs[j].AvailabilityScore
	})

	for i := range recs {
		recs[i].Rank = i + 1
	}

	return RecommendationReport{
		Shape:            shape.Name,
		Design:           design.String(),
		Region:           region,
		MaxGaze:          maxGaze,
		CenterOfRotation: avgCorMM,
		Weights:          weights,
		Recommendations:  recs,
	}
}

// WriteRecommendationReport serializes a RecommendationReport as text, JSON, or CSV.
func WriteRecommendationReport(w io.Writer, rec RecommendationReport, format OutputFormat) error {
	switch format {
	case FormatText:
		return writeTextRecommendationReport(w, rec)
	case FormatJSON:
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(rec)
	case FormatCSV:
		return writeCSVRecommendationReport(w, rec)
	default:
		return fmt.Errorf("unsupported output format %q", format)
	}
}

func writeTextRecommendationReport(w io.Writer, rec RecommendationReport) error {
	bw := bufio.NewWriter(w)
	regionLabel := strings.ToUpper(string(rec.Region))
	if regionLabel == "" {
		regionLabel = "US"
	}
	fmt.Fprintf(bw, "Material Recommendation (Shape: %s | Design: %s | Region: %s | Clarity Zone: ±%.0f°)\n",
		rec.Shape, rec.Design, regionLabel, rec.MaxGaze)
	fmt.Fprintf(bw, "Weights: Clarity %.0f%% | Thickness %.0f%% | Weight %.0f%% | Cost %.0f%% | Availability %.0f%%\n\n",
		rec.Weights.Clarity*100, rec.Weights.Thickness*100, rec.Weights.Weight*100, rec.Weights.Cost*100, rec.Weights.Availability*100)
	fmt.Fprintf(bw, "%-4s %-32s %6s %4s %9s %9s %9s %9s %6s %7s %7s %7s %7s %6s %7s\n",
		"Rank", "Material", "Index", "Abbe", "Avg±30°Δ", "Max±30°Δ", "Max Thk", "Pair Wt", "Cost", "Clarity", "Thick", "Weight", "Value", "Avail", "Score")
	fmt.Fprintf(bw, "-------------------------------------------------------------------------------------------------------------------------------------------------\n")
	for _, r := range rec.Recommendations {
		fmt.Fprintf(bw, "%-4d %-32s %6.3f %4d %9.4f %9.4f %6.2f mm %7.1f g %5.1fx %7.1f %7.1f %7.1f %7.1f %6.0f %7.1f\n",
			r.Rank, r.Name, r.Index, r.Abbe, r.MeanDispersion, r.MaxDispersion,
			r.MaxThickness, r.PairWeight, r.RelativeCost,
			r.ClarityScore, r.ThicknessScore, r.WeightScore, r.CostScore, r.AvailabilityScore, r.TotalScore)
	}
	if len(rec.Recommendations) > 0 {
		best := rec.Recommendations[0]
		fmt.Fprintf(bw, "\nRecommended: %s (Score %.1f/100 — Avg ±%.0f° dispersion %.4f Δ, Max Thickness %.2f mm, Pair Weight %.1f g, Rel. Cost %.1fx, Avail %.0f/100)\n",
			best.Name, best.TotalScore, rec.MaxGaze, best.MeanDispersion, best.MaxThickness, best.PairWeight, best.RelativeCost, best.AvailabilityScore)
	}
	return bw.Flush()
}

func writeCSVRecommendationReport(w io.Writer, rec RecommendationReport) error {
	cw := csv.NewWriter(w)
	header := []string{
		"rank", "key", "name", "index", "family", "abbe", "specific_gravity", "relative_cost",
		"mean_dispersion", "max_dispersion",
		"max_thickness", "max_edge_thickness", "pair_weight",
		"od_center_thickness", "os_center_thickness",
		"clarity_score", "thickness_score", "weight_score", "cost_score", "availability_score", "total_score",
	}
	if err := cw.Write(header); err != nil {
		return err
	}
	f0 := func(v float64) string { return strconv.FormatFloat(v, 'f', 0, 64) }
	f1 := func(v float64) string { return strconv.FormatFloat(v, 'f', 1, 64) }
	f2 := func(v float64) string { return strconv.FormatFloat(v, 'f', 2, 64) }
	f4 := func(v float64) string { return strconv.FormatFloat(v, 'f', 4, 64) }
	for _, r := range rec.Recommendations {
		row := []string{
			strconv.Itoa(r.Rank),
			r.Key,
			r.Name,
			strconv.FormatFloat(r.Index, 'f', 3, 64),
			string(r.Family),
			strconv.Itoa(r.Abbe),
			f2(r.SpecificGravity),
			f1(r.RelativeCost),
			f4(r.MeanDispersion),
			f4(r.MaxDispersion),
			f2(r.MaxThickness),
			f2(r.MaxEdgeThickness),
			f2(r.PairWeight),
			f2(r.ODCenterThickness),
			f2(r.OSCenterThickness),
			f2(r.ClarityScore),
			f2(r.ThicknessScore),
			f2(r.WeightScore),
			f2(r.CostScore),
			f0(r.AvailabilityScore),
			f2(r.TotalScore),
		}
		if err := cw.Write(row); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

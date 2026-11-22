// Package lens models physical ophthalmic spectacle lens thickness, 3D volume,
// weight, multi-format thickness reporting, and multi-criteria material
// recommendations.
//
// Surface profiles are evaluated across all 360 rim meridians using conic
// sagitta from package [optics] ([CalcEyeProfile], [BuildReport]) across
// spherical, aspheric, and bi-aspheric lens designs ([optics.LensDesign]).
// Modeling accounts for spherocylindrical prescription power, monocular PD
// decentration, optical center height (OCH), edge safety chamfer, eyewire
// V-bevel / groove depth, and either analytical superellipse presets or
// arbitrary traced rim contours ([contour.Contour]).
//
// For material selection, [RecommendMaterials] ranks the canonical 12-material
// catalog from [optics.StandardMaterials] using DSM ±30° chromatic dispersion
// clarity, maximum thickness, pair weight, relative cost, and regional lab
// availability.
package lens

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"

	"jsouthworth.net/go/optical-analyzer/contour"
	"jsouthworth.net/go/optical-analyzer/optics"
)

// ShapeOption describes a frame shape preset or a traced lens contour pair.
type ShapeOption struct {
	Name        string           // Display label for the shape or loaded trace
	Exp         float64          // Superellipse exponent n when ODContour/OSContour are nil
	ODContour   *contour.Contour // Optional explicit OD rim contour
	OSContour   *contour.Contour // Optional explicit OS rim contour
	Source      string           // Optional trace file path or origin label
	GrooveDepth float64          // Optional eyewire groove depth per side in mm
}

// EyeProfile holds computed edge/center thickness (mm), curvature (D),
// volume (mm³), and weight (g) for one lens.
type EyeProfile struct {
	MaxCaliperThickness  float64 `json:"max_caliper_thickness"`  // Maximum caliper thickness around the 360° rim (mm)
	MeanCaliperThickness float64 `json:"mean_caliper_thickness"` // Mean caliper thickness around the 360° rim (mm)
	TempHorizontal       float64 `json:"temporal_horizontal"`    // Caliper edge thickness at the horizontal temporal rim (mm)
	NasalHorizontal      float64 `json:"nasal_horizontal"`       // Caliper edge thickness at the horizontal nasal rim (mm)
	CenterThickness      float64 `json:"center_thickness"`       // Axial center thickness at the optical center (mm)
	BaseCurve            float64 `json:"base_curve"`             // Front base curve in 1.530 tooling diopters
	Volume               float64 `json:"volume"`                 // Integrated 3D lens volume (mm³)
	Weight               float64 `json:"weight"`                 // Estimated finished lens mass (g)
}

// OutputFormat represents the report serialization format.
type OutputFormat string

const (
	// FormatText formats reports as human-readable aligned text tables.
	FormatText OutputFormat = "text"
	// FormatJSON formats reports as indented JSON documents.
	FormatJSON OutputFormat = "json"
	// FormatCSV formats reports as RFC 4180 comma-separated values.
	FormatCSV OutputFormat = "csv"
)

// EyePrescription holds single-eye Rx, centration, and optional per-eye optical overrides.
type EyePrescription struct {
	Sphere               float64               `json:"sphere"`
	Cyl                  float64               `json:"cyl"`
	Axis                 float64               `json:"axis"`
	PD                   float64               `json:"pd"`
	PDAuto               bool                  `json:"pd_auto"`
	OCHeight             float64               `json:"oc_height"`
	OCHeightAuto         bool                  `json:"oc_height_auto"`
	RefractiveIndex      float64               `json:"refractive_index,omitempty"`
	Family               optics.MaterialFamily `json:"family,omitempty"`
	SpecificGravity      float64               `json:"specific_gravity,omitempty"`
	InputBaseCurve       float64               `json:"input_base_curve,omitempty"`
	InputCenterThickness float64               `json:"input_center_thickness,omitempty"`
}

// FrameSpec holds frame boxing dimensions in millimeters.
type FrameSpec struct {
	A   float64 `json:"a"`   // Horizontal boxing width (mm)
	B   float64 `json:"b"`   // Vertical boxing height (mm)
	DBL float64 `json:"dbl"` // Bridge / distance between lenses (mm)
}

// ReportInputs holds all normalized calculation inputs (linear dimensions in mm).
type ReportInputs struct {
	OD                   EyePrescription       `json:"od"`
	OS                   EyePrescription       `json:"os"`
	RefractiveIndex      float64               `json:"refractive_index"`
	Family               optics.MaterialFamily `json:"family,omitempty"`
	SpecificGravity      float64               `json:"specific_gravity"`
	InputBaseCurve       float64               `json:"input_base_curve"`
	InputCenterThickness float64               `json:"input_center_thickness"`
	Chamfer              float64               `json:"chamfer"`
	BevelDepth           float64               `json:"bevel_depth"`
	Frame                FrameSpec             `json:"frame"`
}

// DesignResult holds computed OD/OS profiles for one optical design.
type DesignResult struct {
	Design     string     `json:"design"`
	PairWeight float64    `json:"pair_weight"`
	OD         EyeProfile `json:"od"`
	OS         EyeProfile `json:"os"`
}

// ShapeResult holds computed design results for one frame shape.
type ShapeResult struct {
	Shape       string         `json:"shape"`
	Exponent    float64        `json:"exponent,omitempty"`
	Source      string         `json:"source,omitempty"`
	GrooveDepth float64        `json:"groove_depth,omitempty"`
	Designs     []DesignResult `json:"designs"`
}

// Report holds inputs and all computed shape/design results.
type Report struct {
	Inputs  ReportInputs  `json:"inputs"`
	Results []ShapeResult `json:"results"`
}

// ReportInputsFromData converts a raw or normalized [optics.Data] struct into
// [ReportInputs], preserving independent per-eye index, substrate family, SG,
// base curve, and center thickness.
func ReportInputsFromData(d optics.Data) ReportInputs {
	odLens := d.Lenses.EffectiveEye(true)
	osLens := d.Lenses.EffectiveEye(false)
	odIdx := odLens.Index
	if odIdx <= 1.0 {
		odIdx = optics.DefaultRefractiveIndex
	}
	osIdx := osLens.Index
	if osIdx <= 1.0 {
		osIdx = odIdx
	}
	return ReportInputs{
		OD: EyePrescription{
			Sphere:               d.RX.OD.Sph,
			Cyl:                  d.RX.OD.Cyl,
			Axis:                 d.RX.OD.Axis,
			PD:                   d.Fit.OD.PD,
			OCHeight:             d.Fit.OD.OCH,
			RefractiveIndex:      odIdx,
			Family:               odLens.Family,
			SpecificGravity:      odLens.SpecificGravity,
			InputBaseCurve:       odLens.BaseCurve,
			InputCenterThickness: odLens.CenterThickness,
		},
		OS: EyePrescription{
			Sphere:               d.RX.OS.Sph,
			Cyl:                  d.RX.OS.Cyl,
			Axis:                 d.RX.OS.Axis,
			PD:                   d.Fit.OS.PD,
			OCHeight:             d.Fit.OS.OCH,
			RefractiveIndex:      osIdx,
			Family:               osLens.Family,
			SpecificGravity:      osLens.SpecificGravity,
			InputBaseCurve:       osLens.BaseCurve,
			InputCenterThickness: osLens.CenterThickness,
		},
		RefractiveIndex:      odIdx,
		Family:               odLens.Family,
		SpecificGravity:      odLens.SpecificGravity,
		InputBaseCurve:       odLens.BaseCurve,
		InputCenterThickness: odLens.CenterThickness,
		Chamfer:              d.Lenses.EffectiveChamfer(),
		BevelDepth:           d.Lenses.EffectiveBevelDepth(),
		Frame: FrameSpec{
			A:   d.Frame.A,
			B:   d.Frame.B,
			DBL: d.Frame.DBL,
		},
	}
}

// CalcEyeProfile computes the full edge/center caliper profile, 3D volume, and
// weight for a single lens described by p. Any zero/unset fields (BaseCurve,
// SpecificGravity, CenterThickness, MonoPD, OCHeight, Index) are resolved
// automatically from p's prescription, substrate family, frame, and design.
func CalcEyeProfile(p optics.EyeGeometry) EyeProfile {
	index := p.Index
	if index <= 1.0 {
		index = optics.DefaultRefractiveIndex
	}
	chamfer := p.ChamferMM
	if chamfer < 0 {
		chamfer = 0
	}
	frameCenter := (p.FrameA + p.FrameDBL) / 2.0
	monoPD := p.MonoPD
	if monoPD <= 0 {
		monoPD = frameCenter
	}
	och := p.OCHeight
	if och <= 0 {
		och = p.FrameB / 2.0
	}
	baseCurve := p.BaseCurve
	if baseCurve <= 0 {
		baseCurve = optics.EstimateBaseCurve(p.Sphere, p.Cyl, p.Design)
	}
	sg := p.SpecificGravity
	if sg <= 0 {
		sg = optics.EstimateSpecificGravity(index, p.Family)
	}

	c := p.EffectiveRimContour()

	centerThick := p.CenterThickness
	if centerThick <= 0 {
		ctParams := p
		ctParams.Index = index
		ctParams.BaseCurve = baseCurve
		ctParams.MonoPD = monoPD
		ctParams.OCHeight = och
		ctParams.ChamferMM = chamfer
		ctParams.BevelDepthMM = 0 // already inset in c
		ctParams.Contour = &c
		centerThick = optics.EstimateCenterThickness(ctParams)
	}

	surf := optics.NewSurfaceModel(p.Sphere, p.Cyl, p.Axis, baseCurve, index, centerThick, p.Design)
	decentration := frameCenter - monoPD
	ocX := decentration
	if !p.IsOD {
		ocX = -decentration
	}
	ocY := och - p.FrameB/2.0

	maxThick := 0.0
	sumThick := 0.0
	var tempHoriz, nasalHoriz float64
	totalVolumeMM3 := 0.0

	const radialSteps = 25
	dTheta := (2.0 * math.Pi) / float64(contour.Steps)

	for i := 0; i < contour.Steps; i++ {
		rimX, rimY := c.PointAt(i)

		dxRim := rimX - ocX
		dyRim := rimY - ocY

		axialRim, _ := surf.AxialThicknessAt(dxRim, dyRim)
		caliperThick := axialRim - chamfer
		if caliperThick < 1.0 {
			caliperThick = 1.0
		}

		if caliperThick > maxThick {
			maxThick = caliperThick
		}
		sumThick += caliperThick

		if i == 0 {
			if p.IsOD {
				nasalHoriz = caliperThick
			} else {
				tempHoriz = caliperThick
			}
		} else if i == contour.Steps/2 {
			if p.IsOD {
				tempHoriz = caliperThick
			} else {
				nasalHoriz = caliperThick
			}
		}

		rRim := c.Radii[i]
		rRimSq := rRim * rRim
		du := 1.0 / float64(radialSteps)

		for j := 0; j < radialSteps; j++ {
			u := (float64(j) + 0.5) * du
			px := u * rimX
			py := u * rimY

			tAxial, _ := surf.AxialThicknessAt(px-ocX, py-ocY)
			dArea := rRimSq * u * du * dTheta
			totalVolumeMM3 += tAxial * dArea
		}
	}

	effectiveVolumeCM3 := (totalVolumeMM3 * 0.98) / 1000.0
	weightGrams := effectiveVolumeCM3 * sg

	return EyeProfile{
		MaxCaliperThickness:  maxThick,
		MeanCaliperThickness: sumThick / float64(contour.Steps),
		TempHorizontal:       tempHoriz,
		NasalHorizontal:      nasalHoriz,
		CenterThickness:      centerThick,
		BaseCurve:            baseCurve,
		Volume:               totalVolumeMM3 * 0.98,
		Weight:               weightGrams,
	}
}

// ResolveShapes resolves a -shape filter string into one or more ShapeOptions.
func ResolveShapes(input string) ([]ShapeOption, error) {
	all := []ShapeOption{
		{Name: "Oval (n=2.0)", Exp: contour.ShapeOval},
		{Name: "Soft Rect (n=3.0)", Exp: contour.ShapeSoftRect},
		{Name: "Squircle (n=4.0)", Exp: contour.ShapeSquircle},
		{Name: "Sharp Box (n=8.0)", Exp: contour.ShapeSharpRectangular},
	}

	norm := strings.ToLower(strings.TrimSpace(input))
	if norm == "" || norm == "all" {
		return all, nil
	}

	switch norm {
	case "oval", "ellipse":
		return []ShapeOption{all[0]}, nil
	case "softrect", "soft-rect", "soft_rect", "p3", "wayfarer":
		return []ShapeOption{all[1]}, nil
	case "squircle", "rect", "rectangle":
		return []ShapeOption{all[2]}, nil
	case "box", "sharp", "sharpbox", "sharp-box":
		return []ShapeOption{all[3]}, nil
	default:
		val, err := strconv.ParseFloat(norm, 64)
		if err != nil || val <= 0 {
			return nil, fmt.Errorf("invalid -shape %q: use oval, softrect, squircle, box, or a positive number", input)
		}
		return []ShapeOption{{Name: fmt.Sprintf("Custom (n=%.2f)", val), Exp: val}}, nil
	}
}

// ResolveDesigns resolves a -design filter string into one or more [optics.LensDesign] values.
func ResolveDesigns(input string) ([]optics.LensDesign, error) {
	all := []optics.LensDesign{
		optics.DesignSpherical,
		optics.DesignAspheric,
		optics.DesignBiAspheric,
	}

	norm := strings.ToLower(strings.TrimSpace(input))
	if norm == "" || norm == "all" {
		return all, nil
	}

	d, err := optics.ResolveLensDesign(norm)
	if err != nil {
		return nil, fmt.Errorf("invalid -design %q: use spherical, aspheric, or biaspheric", input)
	}
	return []optics.LensDesign{d}, nil
}

// ResolveFormat resolves a -format string into an OutputFormat.
func ResolveFormat(input string) (OutputFormat, error) {
	switch strings.ToLower(strings.TrimSpace(input)) {
	case "", "text", "txt", "human":
		return FormatText, nil
	case "json":
		return FormatJSON, nil
	case "csv":
		return FormatCSV, nil
	default:
		return "", fmt.Errorf("invalid -format %q: use text, json, or csv", input)
	}
}

// ResolveContours returns the groove-to-groove OD and OS contours for a ShapeOption and frame size.
func ResolveContours(s ShapeOption, frameA, frameB float64) (od, osC contour.Contour) {
	if s.ODContour != nil {
		od = *s.ODContour
		if s.OSContour != nil {
			osC = *s.OSContour
		} else {
			osC = od.Mirrored()
		}
		return od, osC
	}
	od = contour.Superellipse(frameA, frameB, s.Exp)
	return od, od.Mirrored()
}

func resolveEyeOptics(eye EyePrescription, shared ReportInputs) (index float64, fam optics.MaterialFamily, sg, bc, ct float64) {
	index = eye.RefractiveIndex
	if index <= 1.0 {
		index = shared.RefractiveIndex
	}
	if index <= 1.0 {
		index = optics.DefaultRefractiveIndex
	}
	fam = eye.Family
	if fam == optics.FamilyAuto {
		fam = shared.Family
	}
	if fam == optics.FamilyAuto {
		if optics.IsGlassMaterial(index, fam) {
			fam = optics.FamilyGlass
		} else {
			fam = optics.FamilyPlastic
		}
	}
	sg = eye.SpecificGravity
	if sg <= 0 {
		if eye.RefractiveIndex <= 1.0 || math.Abs(eye.RefractiveIndex-shared.RefractiveIndex) < 1e-6 && eye.Family == shared.Family {
			sg = shared.SpecificGravity
		}
	}
	if sg <= 0 {
		sg = optics.EstimateSpecificGravity(index, fam)
	}
	bc = eye.InputBaseCurve
	if bc <= 0 {
		bc = shared.InputBaseCurve
	}
	ct = eye.InputCenterThickness
	if ct <= 0 {
		ct = shared.InputCenterThickness
	}
	return index, fam, sg, bc, ct
}

// BuildReport normalizes inputs and evaluates all shape/design combinations.
func BuildReport(inputs ReportInputs, shapes []ShapeOption, designs []optics.LensDesign) Report {
	if inputs.OD.PD <= 0 {
		inputs.OD.PD = (inputs.Frame.A + inputs.Frame.DBL) / 2.0
		inputs.OD.PDAuto = true
	}
	if inputs.OS.PD <= 0 {
		inputs.OS.PD = (inputs.Frame.A + inputs.Frame.DBL) / 2.0
		inputs.OS.PDAuto = true
	}
	if inputs.OD.OCHeight <= 0 {
		inputs.OD.OCHeight = inputs.Frame.B / 2.0
		inputs.OD.OCHeightAuto = true
	}
	if inputs.OS.OCHeight <= 0 {
		inputs.OS.OCHeight = inputs.Frame.B / 2.0
		inputs.OS.OCHeightAuto = true
	}
	if inputs.RefractiveIndex <= 1.0 {
		inputs.RefractiveIndex = optics.DefaultRefractiveIndex
	}
	if inputs.Family == optics.FamilyAuto {
		if optics.IsGlassMaterial(inputs.RefractiveIndex, inputs.Family) {
			inputs.Family = optics.FamilyGlass
		} else {
			inputs.Family = optics.FamilyPlastic
		}
	}
	if inputs.SpecificGravity <= 0 {
		inputs.SpecificGravity = optics.EstimateSpecificGravity(inputs.RefractiveIndex, inputs.Family)
	}
	if inputs.Chamfer < 0 {
		inputs.Chamfer = 0
	}
	if inputs.BevelDepth < 0 {
		inputs.BevelDepth = 0
	}

	odIdx, odFam, odSG, odBC, odCT := resolveEyeOptics(inputs.OD, inputs)
	osIdx, osFam, osSG, osBC, osCT := resolveEyeOptics(inputs.OS, inputs)

	results := make([]ShapeResult, 0, len(shapes))
	for _, s := range shapes {
		odContour, osContour := ResolveContours(s, inputs.Frame.A, inputs.Frame.B)
		if inputs.BevelDepth > 0 {
			odContour = contour.Offset(odContour, -inputs.BevelDepth)
			osContour = contour.Offset(osContour, -inputs.BevelDepth)
		}

		designResults := make([]DesignResult, 0, len(designs))
		for _, d := range designs {
			od := CalcEyeProfile(optics.EyeGeometry{
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
				Index:           odIdx,
				Family:          odFam,
				SpecificGravity: odSG,
				CenterThickness: odCT,
				ChamferMM:       inputs.Chamfer,
				Contour:         &odContour,
				Design:          d,
			})
			osP := CalcEyeProfile(optics.EyeGeometry{
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
				Index:           osIdx,
				Family:          osFam,
				SpecificGravity: osSG,
				CenterThickness: osCT,
				ChamferMM:       inputs.Chamfer,
				Contour:         &osContour,
				Design:          d,
			})
			designResults = append(designResults, DesignResult{
				Design:     d.String(),
				PairWeight: od.Weight + osP.Weight,
				OD:         od,
				OS:         osP,
			})
		}
		results = append(results, ShapeResult{
			Shape:       s.Name,
			Exponent:    s.Exp,
			Source:      s.Source,
			GrooveDepth: s.GrooveDepth,
			Designs:     designResults,
		})
	}

	return Report{
		Inputs:  inputs,
		Results: results,
	}
}

// WriteReport serializes report in the requested format.
func WriteReport(w io.Writer, report Report, format OutputFormat) error {
	switch format {
	case FormatText:
		return WriteTextReport(w, report)
	case FormatJSON:
		return WriteJSONReport(w, report)
	case FormatCSV:
		return WriteCSVReport(w, report)
	default:
		return fmt.Errorf("unsupported output format %q", format)
	}
}

// WriteTextReport renders report in human-readable text format.
func WriteTextReport(w io.Writer, report Report) error {
	bw := bufio.NewWriter(w)

	odPDTag := ""
	if report.Inputs.OD.PDAuto {
		odPDTag = " (auto (A+DBL)/2)"
	}
	osPDTag := ""
	if report.Inputs.OS.PDAuto {
		osPDTag = " (auto (A+DBL)/2)"
	}
	odOCHTag := ""
	if report.Inputs.OD.OCHeightAuto {
		odOCHTag = " (auto B/2)"
	}
	osOCHTag := ""
	if report.Inputs.OS.OCHeightAuto {
		osOCHTag = " (auto B/2)"
	}

	ctLabel := "auto"
	if report.Inputs.InputCenterThickness > 0 {
		ctLabel = fmt.Sprintf("%.2f mm", report.Inputs.InputCenterThickness)
	}

	fmt.Fprintf(bw, "OD Sph: %.2f | Cyl: %.2f | Axis: %.0f | PD: %.2f%s | OC Ht: %.2f%s\n",
		report.Inputs.OD.Sphere, report.Inputs.OD.Cyl, report.Inputs.OD.Axis, report.Inputs.OD.PD, odPDTag, report.Inputs.OD.OCHeight, odOCHTag)
	fmt.Fprintf(bw, "OS Sph: %.2f | Cyl: %.2f | Axis: %.0f | PD: %.2f%s | OC Ht: %.2f%s\n",
		report.Inputs.OS.Sphere, report.Inputs.OS.Cyl, report.Inputs.OS.Axis, report.Inputs.OS.PD, osPDTag, report.Inputs.OS.OCHeight, osOCHTag)
	materialTag := ""
	if report.Inputs.Family != "" {
		materialTag = fmt.Sprintf(" (%s)", report.Inputs.Family)
	}
	fmt.Fprintf(bw, "Index: %.2f%s | Specific Gravity: %.2f g/cm^3 | Input BC: %.2f | Center Thick: %s | Chamfer: %.2f mm | Bevel: %.2f mm\n",
		report.Inputs.RefractiveIndex, materialTag, report.Inputs.SpecificGravity, report.Inputs.InputBaseCurve, ctLabel, report.Inputs.Chamfer, report.Inputs.BevelDepth)
	fmt.Fprintf(bw, "Frame: A=%.1f, B=%.1f, DBL=%.1f\n\n",
		report.Inputs.Frame.A, report.Inputs.Frame.B, report.Inputs.Frame.DBL)

	for _, s := range report.Results {
		fmt.Fprintf(bw, "====================================================================================================\n")
		if s.GrooveDepth != 0 {
			fmt.Fprintf(bw, "FRAME SHAPE: %s (groove depth: %.2f mm/side)\n", s.Shape, s.GrooveDepth)
		} else {
			fmt.Fprintf(bw, "FRAME SHAPE: %s\n", s.Shape)
		}
		fmt.Fprintf(bw, "====================================================================================================\n")
		for _, d := range s.Designs {
			fmt.Fprintf(bw, "  [%s] (Pair Weight: %.1f g)\n", d.Design, d.PairWeight)
			fmt.Fprintf(bw, "    OD (BC +%.2f, CT %.2f) -> Max: %.2f mm | Avg: %.2f mm | Temp: %.2f mm | Nasal: %.2f mm | Wt: %.1f g\n",
				d.OD.BaseCurve, d.OD.CenterThickness, d.OD.MaxCaliperThickness, d.OD.MeanCaliperThickness, d.OD.TempHorizontal, d.OD.NasalHorizontal, d.OD.Weight)
			fmt.Fprintf(bw, "    OS (BC +%.2f, CT %.2f) -> Max: %.2f mm | Avg: %.2f mm | Temp: %.2f mm | Nasal: %.2f mm | Wt: %.1f g\n",
				d.OS.BaseCurve, d.OS.CenterThickness, d.OS.MaxCaliperThickness, d.OS.MeanCaliperThickness, d.OS.TempHorizontal, d.OS.NasalHorizontal, d.OS.Weight)
		}
		fmt.Fprintln(bw)
	}

	return bw.Flush()
}

// WriteJSONReport serializes report as pretty-printed JSON.
func WriteJSONReport(w io.Writer, report Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}

// WriteCSVReport serializes report as RFC 4180 CSV with one row per (shape, design, eye).
func WriteCSVReport(w io.Writer, report Report) error {
	cw := csv.NewWriter(w)
	header := []string{
		"shape",
		"shape_exponent",
		"design",
		"eye",
		"sphere",
		"cyl",
		"axis",
		"pd",
		"oc_height",
		"refractive_index",
		"specific_gravity",
		"frame_a",
		"frame_b",
		"frame_dbl",
		"chamfer",
		"bevel_depth",
		"base_curve",
		"center_thickness",
		"max_caliper_thickness",
		"mean_caliper_thickness",
		"temporal_horizontal",
		"nasal_horizontal",
		"volume",
		"weight",
		"pair_weight",
	}
	if err := cw.Write(header); err != nil {
		return err
	}

	fmtRaw := func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
	fmtFixed := func(v float64) string { return strconv.FormatFloat(v, 'f', 2, 64) }

	in := report.Inputs
	for _, s := range report.Results {
		for _, d := range s.Designs {
			eyes := []struct {
				label   string
				rx      EyePrescription
				profile EyeProfile
			}{
				{"OD", in.OD, d.OD},
				{"OS", in.OS, d.OS},
			}
			for _, eye := range eyes {
				expStr := ""
				if s.Exponent > 0 {
					expStr = fmtRaw(s.Exponent)
				}
				idx := eye.rx.RefractiveIndex
				if idx <= 1.0 {
					idx = in.RefractiveIndex
				}
				sg := eye.rx.SpecificGravity
				if sg <= 0 {
					sg = in.SpecificGravity
				}
				row := []string{
					s.Shape,
					expStr,
					d.Design,
					eye.label,
					fmtRaw(eye.rx.Sphere),
					fmtRaw(eye.rx.Cyl),
					fmtRaw(eye.rx.Axis),
					fmtRaw(eye.rx.PD),
					fmtRaw(eye.rx.OCHeight),
					fmtRaw(idx),
					fmtRaw(sg),
					fmtRaw(in.Frame.A),
					fmtRaw(in.Frame.B),
					fmtRaw(in.Frame.DBL),
					fmtRaw(in.Chamfer),
					fmtRaw(in.BevelDepth),
					fmtFixed(eye.profile.BaseCurve),
					fmtFixed(eye.profile.CenterThickness),
					fmtFixed(eye.profile.MaxCaliperThickness),
					fmtFixed(eye.profile.MeanCaliperThickness),
					fmtFixed(eye.profile.TempHorizontal),
					fmtFixed(eye.profile.NasalHorizontal),
					fmtFixed(eye.profile.Volume),
					fmtFixed(eye.profile.Weight),
					fmtFixed(d.PairWeight),
				}
				if err := cw.Write(row); err != nil {
					return err
				}
			}
		}
	}

	cw.Flush()
	return cw.Error()
}

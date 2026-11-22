package optics

import (
	"fmt"
	"math"
	"strings"

	"jsouthworth.net/go/optical-analyzer/contour"
)

// LensDesign identifies the optical lens surface geometry used for automatic
// base curve (F1), conic sagitta, and center thickness calculation.
type LensDesign int

const (
	// DesignBiAspheric uses double-aspheric / atoric front and back curves (default).
	DesignBiAspheric LensDesign = iota
	// DesignAspheric uses front-aspheric curves with a spherical/toric back.
	DesignAspheric
	// DesignSpherical uses traditional spherical front and back curves.
	DesignSpherical
)

// String returns the display name of the lens design.
func (d LensDesign) String() string {
	switch d {
	case DesignAspheric:
		return "Aspheric"
	case DesignSpherical:
		return "Spherical"
	default:
		return "Bi-Aspheric"
	}
}

// Key returns the canonical lowercase identifier for JSON serialization.
func (d LensDesign) Key() string {
	switch d {
	case DesignAspheric:
		return "aspheric"
	case DesignSpherical:
		return "spherical"
	default:
		return "biaspheric"
	}
}

// MarshalText encodes d as its canonical lowercase key for JSON/text serialization.
func (d LensDesign) MarshalText() ([]byte, error) {
	return []byte(d.Key()), nil
}

// UnmarshalText decodes a lens design name using [ResolveLensDesign].
func (d *LensDesign) UnmarshalText(text []byte) error {
	parsed, err := ResolveLensDesign(string(text))
	if err != nil {
		return err
	}
	*d = parsed
	return nil
}

// ResolveLensDesign parses a design flag string into a LensDesign.
// Empty string defaults to DesignBiAspheric.
func ResolveLensDesign(input string) (LensDesign, error) {
	switch strings.ToLower(strings.TrimSpace(input)) {
	case "", "biaspheric", "bi-aspheric", "bi_aspheric", "biaspherical", "bi-aspherical", "biasph", "double-aspheric":
		return DesignBiAspheric, nil
	case "aspheric", "aspherical", "asph", "atoric":
		return DesignAspheric, nil
	case "spherical", "sph", "standard":
		return DesignSpherical, nil
	default:
		return DesignBiAspheric, fmt.Errorf("invalid design %q: use biaspheric, aspheric, or spherical", input)
	}
}

// EstimateBaseCurve estimates an appropriate front base curve (in 1.530 tooling
// diopters) rounded to 0.25 D and clamped to [0.50, 12.00].
func EstimateBaseCurve(sphere, cyl float64, design LensDesign) float64 {
	se := sphere + (cyl / 2.0)
	var bc float64

	switch design {
	case DesignSpherical:
		if se < 0 {
			bc = (0.50 * se) + 6.00
		} else {
			bc = se + 6.00
		}
	case DesignAspheric:
		if se < 0 {
			bc = (0.50 * se) + 5.00
		} else {
			bc = (0.75 * se) + 4.75
		}
	default: // DesignBiAspheric
		if se < 0 {
			bc = (0.40 * se) + 4.00
		} else {
			bc = (0.60 * se) + 4.00
		}
	}

	bc = math.Round(bc*4.0) / 4.0
	if bc < 0.50 {
		bc = 0.50
	}
	if bc > 12.00 {
		bc = 12.00
	}
	return bc
}

// CalcBaseCurve is a convenience wrapper around EstimateBaseCurve for a Refraction.
func CalcBaseCurve(rx Refraction, design LensDesign) float64 {
	return EstimateBaseCurve(rx.Sph, rx.Cyl, design)
}

// ConicParams returns front and back conic shape parameters p = 1 + k for design.
func ConicParams(design LensDesign) (pFront, pBack float64) {
	switch design {
	case DesignAspheric:
		return 0.25, 1.0
	case DesignBiAspheric:
		return 0.0, 0.25
	default: // DesignSpherical
		return 1.0, 1.0
	}
}

// CalcConicSagAndSlope computes the conic sagitta (mm) and surface slope angle
// (radians) at radial distance r (mm) for a surface of power surfacePower (D),
// refractive index, and conic constant parameter p = 1 + k.
func CalcConicSagAndSlope(surfacePower, r, index, p float64) (sagMM, slopeRad float64) {
	absP := math.Abs(surfacePower)
	if absP < 1e-6 || r <= 0 {
		return 0, 0
	}
	radius := ((index - 1.0) / absP) * 1000.0
	ratio := r / radius

	disc := 1.0 - p*ratio*ratio
	if disc < 0.05 {
		disc = 0.05
	}
	sqrtDisc := math.Sqrt(disc)

	sag := (r * r / radius) / (1.0 + sqrtDisc)
	slopeAngle := math.Atan(ratio / sqrtDisc)

	return sag, slopeAngle
}

// SurfaceModel holds precomputed front/back conic surface parameters for fast
// axial thickness evaluation at arbitrary offsets (dx, dy) from a lens's
// optical center.
type SurfaceModel struct {
	Sphere          float64 // Sphere power in diopters
	Cyl             float64 // Cylinder power in diopters
	AxisRad         float64 // Cylinder axis in radians
	FrontPower      float64 // True front surface power F1 in diopters (converted from 1.530 tooling base curve)
	Index           float64 // Refractive index
	CenterThickness float64 // Axial center thickness in mm
	PFront          float64 // Front surface conic parameter p = 1 + k
	PBack           float64 // Back surface conic parameter p = 1 + k
}

// NewSurfaceModel constructs a SurfaceModel for the given prescription, tooling
// base curve (D), refractive index, center thickness (mm), and surface design.
func NewSurfaceModel(sphere, cyl, axisDeg, baseCurve, index, centerThickness float64, design LensDesign) SurfaceModel {
	if index <= 1.0 {
		index = DefaultRefractiveIndex
	}
	pFront, pBack := ConicParams(design)
	return SurfaceModel{
		Sphere:          sphere,
		Cyl:             cyl,
		AxisRad:         axisDeg * (math.Pi / 180.0),
		FrontPower:      baseCurve * (index - 1.0) / 0.530,
		Index:           index,
		CenterThickness: centerThickness,
		PFront:          pFront,
		PBack:           pBack,
	}
}

// AxialThicknessAt returns the axial lens thickness (mm) and back surface slope
// angle (rad) at offset (dx, dy) mm from the optical center.
func (s SurfaceModel) AxialThicknessAt(dx, dy float64) (axialThickness, backSlopeRad float64) {
	r := math.Hypot(dx, dy)
	if r == 0 {
		return s.CenterThickness, 0
	}
	theta := math.Atan2(dy, dx)

	sinDiff := math.Sin(theta - s.AxisRad)
	pTheta := s.Sphere + s.Cyl*(sinDiff*sinDiff)
	f2 := pTheta - s.FrontPower

	sag1, _ := CalcConicSagAndSlope(s.FrontPower, r, s.Index, s.PFront)
	sag2, slope2 := CalcConicSagAndSlope(f2, r, s.Index, s.PBack)

	frontDrop := math.Copysign(sag1, s.FrontPower)
	backDrop := math.Copysign(sag2, -f2)

	axialThickness = s.CenterThickness + backDrop - frontDrop
	if axialThickness < 0.5 {
		axialThickness = 0.5
	}
	return axialThickness, slope2
}

// EyeGeometry bundles the single-eye prescription, centration, material,
// edging, frame contour, and surface design parameters used by
// [EstimateCenterThickness] and lens thickness profiling.
type EyeGeometry struct {
	IsOD            bool             // True for right eye (OD), false for left eye (OS)
	Sphere          float64          // Sphere power in diopters
	Cyl             float64          // Cylinder power in diopters
	Axis            float64          // Cylinder axis in degrees (0..180)
	BaseCurve       float64          // Front base curve in 1.530 tooling D (<= 0 = auto)
	FrameA          float64          // Horizontal boxing width A in mm
	FrameB          float64          // Vertical boxing height B in mm
	FrameDBL        float64          // Bridge / distance between lenses DBL in mm
	MonoPD          float64          // Monocular PD in mm (<= 0 = auto (A+DBL)/2)
	OCHeight        float64          // Optical center height from frame bottom in mm (<= 0 = auto B/2)
	Index           float64          // Refractive index (<= 1.0 = DefaultRefractiveIndex)
	Family          MaterialFamily   // Substrate family (FamilyAuto, FamilyPlastic, FamilyGlass)
	SpecificGravity float64          // Specific gravity in g/cm³ (<= 0 = auto from material)
	CenterThickness float64          // Explicit center thickness in mm (<= 0 = auto-calculated)
	ChamferMM       float64          // Edge pin-bevel / safety chamfer thickness reduction in mm
	BevelDepthMM    float64          // Eyewire V-bevel / groove depth inset per side in mm
	Contour         *contour.Contour // Optional groove-to-groove rim contour (nil = superellipse)
	ShapeExp        float64          // Superellipse exponent when Contour is nil (<= 0 = ShapeOval)
	Design          LensDesign       // Optical surface design
}

// EffectiveRimContour resolves the rim contour for p (using p.Contour when
// non-empty, otherwise a superellipse of size FrameA × FrameB and ShapeExp),
// and applies any positive BevelDepthMM inward offset.
func (p EyeGeometry) EffectiveRimContour() contour.Contour {
	var c contour.Contour
	if p.Contour != nil && p.Contour.AreaMM2() > 0 {
		c = *p.Contour
	} else {
		exp := p.ShapeExp
		if exp <= 0 {
			exp = contour.ShapeOval
		}
		c = contour.Superellipse(p.FrameA, p.FrameB, exp)
	}
	if p.BevelDepthMM > 0 && c.AreaMM2() > 0 {
		w, h := c.BoxingSize()
		if 2.0*p.BevelDepthMM < w && 2.0*p.BevelDepthMM < h {
			c = contour.Offset(c, -p.BevelDepthMM)
		}
	}
	return c
}

// EstimateCenterThickness estimates the required center thickness in millimeters
// for one eye using conic sagitta across all 360 rim meridians, accounting for
// material minimums, edge safety chamfer, and eyewire V-bevel depth.
func EstimateCenterThickness(p EyeGeometry) float64 {
	index := p.Index
	if index <= 1.0 {
		index = DefaultRefractiveIndex
	}
	baseCurve := p.BaseCurve
	if baseCurve <= 0 {
		baseCurve = EstimateBaseCurve(p.Sphere, p.Cyl, p.Design)
	}
	chamfer := p.ChamferMM
	if chamfer < 0 {
		chamfer = 0
	}
	minCT := MinCenterThickness(index, p.Family)
	minET := MinEdgeThickness(index, p.Family)

	c := p.EffectiveRimContour()
	if c.AreaMM2() <= 0 {
		return minCT
	}

	pFront, pBack := ConicParams(p.Design)
	f1 := baseCurve * (index - 1.0) / 0.530
	alphaRad := p.Axis * (math.Pi / 180.0)

	frameCenter := (p.FrameA + p.FrameDBL) / 2.0
	monoPD := p.MonoPD
	if monoPD <= 0 {
		monoPD = frameCenter
	}
	decentration := frameCenter - monoPD
	ocX := decentration
	if !p.IsOD {
		ocX = -decentration
	}
	och := p.OCHeight
	if och <= 0 {
		och = p.FrameB / 2.0
	}
	ocY := och - p.FrameB/2.0

	requiredCT := minCT
	for i := 0; i < contour.Steps; i++ {
		rimX, rimY := c.PointAt(i)
		dx := rimX - ocX
		dy := rimY - ocY
		r := math.Hypot(dx, dy)
		theta := math.Atan2(dy, dx)

		sinDiff := math.Sin(theta - alphaRad)
		pTheta := p.Sphere + p.Cyl*(sinDiff*sinDiff)
		f2 := pTheta - f1

		sag1, _ := CalcConicSagAndSlope(f1, r, index, pFront)
		sag2, _ := CalcConicSagAndSlope(f2, r, index, pBack)

		frontDrop := math.Copysign(sag1, f1)
		backDrop := math.Copysign(sag2, -f2)

		neededCT := minET + chamfer + frontDrop - backDrop
		if neededCT > requiredCT {
			requiredCT = neededCT
		}
	}

	return math.Ceil(requiredCT*10.0) / 10.0
}

// Package optics defines shared ophthalmic spectacle domain types, the
// canonical lens material catalog, normalized JSON prescription/frame data I/O,
// and conic lens surface geometry (base curve, sagitta, and center thickness).
//
// All linear dimensions in package optics (CenterOfRotation, CenterThickness,
// Chamfer, BevelDepth, Frame A/B/DBL, Fit PD/OCH, sagitta, and reading
// distance) are expressed consistently in millimeters (mm). Optical powers are
// in diopters (D).
package optics

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"jsouthworth.net/go/optical-analyzer/contour"
)

// CurrentSchemaVersion is the current JSON configuration schema version.
const CurrentSchemaVersion = 1

// Default optical, edging, and frame constants (all linear dimensions in mm).
const (
	DefaultRefractiveIndex    = 1.50                         // CR-39 standard refractive index
	DefaultAbbeNumber         = 58                           // CR-39 standard Abbe constringence
	DefaultCenterOfRotationMM = 27.0                         // Typical back-vertex-to-center-of-rotation distance (mm)
	DefaultFrameA             = 52.0                         // Default horizontal frame boxing width (mm)
	DefaultFrameB             = 40.0                         // Default vertical frame boxing height (mm)
	DefaultFrameDBL           = 18.0                         // Default bridge / distance between lenses (mm)
	DefaultChamferMM          = 0.25                         // Default edge pin-bevel / safety chamfer reduction (mm)
	DefaultBevelDepthMM       = contour.DefaultGrooveDepthMM // Default eyewire V-bevel / groove depth per side (mm)
	DefaultReadingDistanceMM  = 400.0                        // Standard ophthalmic near working distance (40 cm)
)

// Frame holds frame boxing measurements (in mm) and optional shape/trace metadata.
type Frame struct {
	A           float64 `json:"a,omitempty"`            // Horizontal boxing width (mm)
	B           float64 `json:"b,omitempty"`            // Vertical boxing height (mm)
	DBL         float64 `json:"dbl,omitempty"`          // Distance between lenses / bridge width (mm)
	Shape       string  `json:"shape,omitempty"`        // Frame shape preset ("oval", "softrect", "squircle", "box", "trace", or superellipse exponent)
	Trace       string  `json:"trace,omitempty"`        // Optional path to a frame trace file (.trc/.oma, .json, .svg)
	TraceFormat string  `json:"trace_format,omitempty"` // Optional trace format override ("oma", "json", "svg")
}

// EyeFit holds per-eye fitting centration measurements in millimeters.
type EyeFit struct {
	PD               float64 `json:"pd,omitempty"`                 // Monocular pupillary distance in mm (0 = auto (A+DBL)/2)
	OCH              float64 `json:"och,omitempty"`                // Optical center height from frame bottom in mm (0 = auto B/2)
	CenterOfRotation float64 `json:"center_of_rotation,omitempty"` // Optional per-eye back-vertex-to-center-of-rotation distance in mm (0 = inherit Fit.CenterOfRotation / 27.0 mm)
}

// Fit holds monocular PD, optical center height, and center-of-rotation
// measurements in millimeters.
type Fit struct {
	CenterOfRotation float64 `json:"center_of_rotation,omitempty"` // Shared back-vertex-to-center-of-rotation distance in mm (0 = 27.0 mm)
	OD               EyeFit  `json:"od,omitzero"`                  // Right-eye fitting measurements
	OS               EyeFit  `json:"os,omitzero"`                  // Left-eye fitting measurements
}

// EffectiveODOCH returns the OD optical center height in mm, falling back to B/2 when <= 0.
func (f Fit) EffectiveODOCH(frameB float64) float64 {
	if f.OD.OCH > 0 {
		return f.OD.OCH
	}
	return frameB / 2.0
}

// EffectiveOSOCH returns the OS optical center height in mm, falling back to B/2 when <= 0.
func (f Fit) EffectiveOSOCH(frameB float64) float64 {
	if f.OS.OCH > 0 {
		return f.OS.OCH
	}
	return frameB / 2.0
}

// EffectiveODCenterOfRotation returns the OD back-vertex-to-center-of-rotation
// distance in mm, falling back to Fit.CenterOfRotation and then DefaultCenterOfRotationMM.
func (f Fit) EffectiveODCenterOfRotation() float64 {
	if f.OD.CenterOfRotation > 0 {
		return f.OD.CenterOfRotation
	}
	if f.CenterOfRotation > 0 {
		return f.CenterOfRotation
	}
	return DefaultCenterOfRotationMM
}

// EffectiveOSCenterOfRotation returns the OS back-vertex-to-center-of-rotation
// distance in mm, falling back to Fit.CenterOfRotation and then DefaultCenterOfRotationMM.
func (f Fit) EffectiveOSCenterOfRotation() float64 {
	if f.OS.CenterOfRotation > 0 {
		return f.OS.CenterOfRotation
	}
	if f.CenterOfRotation > 0 {
		return f.CenterOfRotation
	}
	return DefaultCenterOfRotationMM
}

// LensParameters holds per-eye lens material and geometry parameters.
// CenterThickness is axial center thickness in millimeters (0 = auto-calculated).
type LensParameters struct {
	Index           float64        `json:"index,omitempty"`            // Refractive index (0 = inherit shared material / default CR-39)
	Family          MaterialFamily `json:"family,omitempty"`           // Substrate family ("plastic", "glass", or "" for auto)
	SpecificGravity float64        `json:"specific_gravity,omitempty"` // Material density in g/cm³ (0 = auto from material)
	CenterThickness float64        `json:"center_thickness,omitempty"` // Center thickness in mm (0 = auto-calculated)
	BaseCurve       float64        `json:"base_curve,omitempty"`       // Front base curve in 1.530 tooling D (0 = auto)
	Abbe            int            `json:"abbe,omitempty"`             // Abbe number (0 = auto from material)
}

// LensPair holds shared lens material, surface design, and edging settings
// along with optional per-eye OD/OS overrides.
type LensPair struct {
	Material        string         `json:"material,omitempty"`         // Standard catalog preset key (e.g. "1.60-plastic", "trivex", or "auto"/empty)
	Design          LensDesign     `json:"design,omitempty"`           // Surface design ("biaspheric", "aspheric", "spherical"; omitted when biaspheric)
	Index           float64        `json:"index,omitempty"`            // Shared custom refractive index (0 = from Material preset / default)
	Family          MaterialFamily `json:"family,omitempty"`           // Shared substrate family ("plastic", "glass", or "" for auto)
	SpecificGravity float64        `json:"specific_gravity,omitempty"` // Shared density in g/cm³ (0 = auto from material)
	Abbe            int            `json:"abbe,omitempty"`             // Shared Abbe number (0 = auto from material)
	Chamfer         *float64       `json:"chamfer,omitempty"`          // Edge safety chamfer thickness reduction in mm (nil = DefaultChamferMM)
	BevelDepth      *float64       `json:"bevel_depth,omitempty"`      // Eyewire V-bevel / groove depth per side in mm (nil = DefaultBevelDepthMM)
	OD              LensParameters `json:"od,omitzero"`                // Right-eye lens parameters / overrides
	OS              LensParameters `json:"os,omitzero"`                // Left-eye lens parameters / overrides
}

// IsAutoMaterial reports whether lp leaves material selection automatic
// (no catalog preset, shared index/family, or per-eye index/family specified).
func (lp LensPair) IsAutoMaterial() bool {
	mat := strings.ToLower(strings.TrimSpace(lp.Material))
	if mat != "" && mat != "auto" {
		return false
	}
	return lp.Index <= 0 && lp.Family == "" &&
		lp.OD.Index <= 0 && lp.OS.Index <= 0 &&
		lp.OD.Family == "" && lp.OS.Family == ""
}

// EffectiveEye resolves shared Material preset and LensPair-level overrides
// merged with the specified eye's (OD or OS) overrides. Unset BaseCurve and
// CenterThickness remain 0 (auto), and unset Index remains 0 if no material
// was specified anywhere in lp.
func (lp LensPair) EffectiveEye(isOD bool) LensParameters {
	var base LensParameters
	if spec, ok := LookupMaterialByKey(lp.Material); ok {
		base.Index = spec.Index
		base.Family = spec.Family
		base.Abbe = spec.Abbe
		base.SpecificGravity = spec.SpecificGravity
	}
	if lp.Index > 0 {
		if base.Index > 0 && math.Abs(lp.Index-base.Index) >= 0.002 {
			base.Abbe = 0
			base.SpecificGravity = 0
		}
		base.Index = lp.Index
	}
	if lp.Family != "" {
		if base.Family != "" && lp.Family != base.Family {
			base.Abbe = 0
			base.SpecificGravity = 0
		}
		base.Family = lp.Family
	}
	if lp.Abbe > 0 {
		base.Abbe = lp.Abbe
	}
	if lp.SpecificGravity > 0 {
		base.SpecificGravity = lp.SpecificGravity
	}

	eye := lp.OS
	if isOD {
		eye = lp.OD
	}

	out := base
	eyeChangedMaterial := false
	if eye.Index > 0 {
		if out.Index > 0 && math.Abs(eye.Index-out.Index) >= 0.002 {
			eyeChangedMaterial = true
		}
		out.Index = eye.Index
	}
	if eye.Family != "" {
		if out.Family != "" && eye.Family != out.Family {
			eyeChangedMaterial = true
		}
		out.Family = eye.Family
	}
	if eyeChangedMaterial {
		out.Abbe = 0
		out.SpecificGravity = 0
	}
	if eye.Abbe > 0 {
		out.Abbe = eye.Abbe
	}
	if eye.SpecificGravity > 0 {
		out.SpecificGravity = eye.SpecificGravity
	}
	out.BaseCurve = eye.BaseCurve
	out.CenterThickness = eye.CenterThickness
	return out
}

// EffectiveChamfer returns the edge pin-bevel / safety chamfer thickness reduction in mm,
// defaulting to DefaultChamferMM (0.25 mm) when Chamfer is nil.
func (lp LensPair) EffectiveChamfer() float64 {
	if lp.Chamfer == nil {
		return DefaultChamferMM
	}
	if *lp.Chamfer < 0 {
		return 0
	}
	return *lp.Chamfer
}

// EffectiveBevelDepth returns the eyewire V-bevel / groove depth per side in mm,
// defaulting to DefaultBevelDepthMM when BevelDepth is nil.
func (lp LensPair) EffectiveBevelDepth() float64 {
	if lp.BevelDepth == nil {
		return DefaultBevelDepthMM
	}
	if *lp.BevelDepth < 0 {
		return 0
	}
	return *lp.BevelDepth
}

// Refraction holds single-eye prescription powers in diopters and cylinder axis in degrees.
type Refraction struct {
	Sph  float64 `json:"sph"`            // Sphere power in diopters
	Cyl  float64 `json:"cyl,omitempty"`  // Cylinder power in diopters
	Axis float64 `json:"axis,omitempty"` // Cylinder axis in degrees (0..180)
}

// RX holds OD and OS prescriptions.
type RX struct {
	OD Refraction `json:"od"` // Right-eye prescription
	OS Refraction `json:"os"` // Left-eye prescription
}

// Data represents the optical specification stored in .json / .dat files.
// All linear dimensions (CenterOfRotation, CenterThickness, Chamfer,
// BevelDepth, Frame A/B/DBL, Fit PD/OCH) are expressed consistently in
// millimeters (mm).
type Data struct {
	Version int      `json:"version,omitempty"` // Optional config schema version (0 or CurrentSchemaVersion)
	RX      RX       `json:"rx"`                // OD and OS prescriptions
	Lenses  LensPair `json:"lenses,omitzero"`   // Lens material, design, and edging settings
	Frame   Frame    `json:"frame,omitzero"`    // Frame boxing dimensions and shape/trace metadata
	Fit     Fit      `json:"fit,omitzero"`      // Fitting centration measurements (PD, OCH, center of rotation)
}

// Validate verifies that all populated fields in d have valid domains and
// recognized enum values. Zero/omitted fields are allowed and resolved by
// [NormalizeData].
func (d Data) Validate() error {
	if d.Version != 0 && d.Version != CurrentSchemaVersion {
		return fmt.Errorf("unsupported config schema version %d (supported: %d)", d.Version, CurrentSchemaVersion)
	}

	validateEyeRX := func(label string, rx Refraction) error {
		if rx.Axis < 0 || rx.Axis > 180 {
			return fmt.Errorf("%s axis %.2f out of range [0, 180]", label, rx.Axis)
		}
		return nil
	}
	if err := validateEyeRX("od", d.RX.OD); err != nil {
		return err
	}
	if err := validateEyeRX("os", d.RX.OS); err != nil {
		return err
	}

	if d.Lenses.Design < DesignBiAspheric || d.Lenses.Design > DesignSpherical {
		return fmt.Errorf("invalid lens design %d", d.Lenses.Design)
	}

	matKey := strings.ToLower(strings.TrimSpace(d.Lenses.Material))
	if matKey == "auto" {
		if d.Lenses.Index > 0 || d.Lenses.Family != "" ||
			d.Lenses.OD.Index > 0 || d.Lenses.OS.Index > 0 ||
			d.Lenses.OD.Family != "" || d.Lenses.OS.Family != "" {
			return fmt.Errorf("lenses.material %q cannot be combined with explicit index or family", d.Lenses.Material)
		}
	} else if matKey != "" {
		if _, ok := LookupMaterialByKey(matKey); !ok {
			return fmt.Errorf("unknown lens material preset %q (use a catalog key such as \"1.60-plastic\", \"trivex\", or \"auto\")", d.Lenses.Material)
		}
	}

	validateFamily := func(label string, fam MaterialFamily) error {
		if fam == "" {
			return nil
		}
		if _, err := ResolveMaterialFamily(string(fam)); err != nil {
			return fmt.Errorf("%s: %w", label, err)
		}
		return nil
	}
	if err := validateFamily("lenses.family", d.Lenses.Family); err != nil {
		return err
	}
	if err := validateFamily("lenses.od.family", d.Lenses.OD.Family); err != nil {
		return err
	}
	if err := validateFamily("lenses.os.family", d.Lenses.OS.Family); err != nil {
		return err
	}

	validateIndex := func(label string, idx float64) error {
		if idx != 0 && (idx <= 1.0 || idx > 3.0) {
			return fmt.Errorf("%s %.4f must be in (1.0, 3.0]", label, idx)
		}
		return nil
	}
	if err := validateIndex("lenses.index", d.Lenses.Index); err != nil {
		return err
	}
	if err := validateIndex("lenses.od.index", d.Lenses.OD.Index); err != nil {
		return err
	}
	if err := validateIndex("lenses.os.index", d.Lenses.OS.Index); err != nil {
		return err
	}

	if d.Lenses.SpecificGravity < 0 || d.Lenses.OD.SpecificGravity < 0 || d.Lenses.OS.SpecificGravity < 0 {
		return fmt.Errorf("specific_gravity must be >= 0")
	}
	if d.Lenses.Abbe < 0 || d.Lenses.OD.Abbe < 0 || d.Lenses.OS.Abbe < 0 {
		return fmt.Errorf("abbe must be >= 0")
	}
	if d.Lenses.OD.BaseCurve < 0 || d.Lenses.OS.BaseCurve < 0 {
		return fmt.Errorf("base_curve must be >= 0")
	}
	if d.Lenses.OD.CenterThickness < 0 || d.Lenses.OS.CenterThickness < 0 {
		return fmt.Errorf("center_thickness must be >= 0")
	}
	if d.Lenses.Chamfer != nil && *d.Lenses.Chamfer < 0 {
		return fmt.Errorf("lenses.chamfer must be >= 0")
	}
	if d.Lenses.BevelDepth != nil && *d.Lenses.BevelDepth < 0 {
		return fmt.Errorf("lenses.bevel_depth must be >= 0")
	}

	if d.Frame.A < 0 || d.Frame.B < 0 || d.Frame.DBL < 0 {
		return fmt.Errorf("frame dimensions a, b, and dbl must be >= 0")
	}
	if err := validateFrameShape(d.Frame.Shape); err != nil {
		return err
	}
	if d.Frame.TraceFormat != "" {
		if _, err := contour.ResolveFormat(d.Frame.TraceFormat, ""); err != nil {
			return fmt.Errorf("frame.trace_format: %w", err)
		}
	}

	if d.Fit.CenterOfRotation < 0 || d.Fit.OD.CenterOfRotation < 0 || d.Fit.OS.CenterOfRotation < 0 {
		return fmt.Errorf("fit center_of_rotation must be >= 0")
	}
	if d.Fit.OD.PD < 0 || d.Fit.OS.PD < 0 || d.Fit.OD.OCH < 0 || d.Fit.OS.OCH < 0 {
		return fmt.Errorf("fit pd and och must be >= 0")
	}

	return nil
}

func validateFrameShape(shape string) error {
	s := strings.ToLower(strings.TrimSpace(shape))
	if s == "" {
		return nil
	}
	switch s {
	case "oval", "round", "ellipse",
		"softrect", "soft-rect", "soft_rect", "rect", "rectangle", "rectangular",
		"squircle", "aviator", "wayfarer",
		"box", "sharp", "sharpbox", "sharp-box", "sharp_box", "square",
		"trace":
		return nil
	}
	if exp, err := strconv.ParseFloat(s, 64); err == nil && exp > 0 {
		return nil
	}
	return fmt.Errorf("invalid frame.shape %q (use oval, softrect, squircle, box, trace, or a positive superellipse exponent)", shape)
}

// DecodeData parses and validates a JSON Data document from r, rejecting
// unknown fields and trailing data.
func DecodeData(r io.Reader) (Data, error) {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	var data Data
	if err := dec.Decode(&data); err != nil {
		return Data{}, err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return Data{}, fmt.Errorf("unexpected trailing data in JSON input")
	}
	if err := data.Validate(); err != nil {
		return Data{}, err
	}
	return data, nil
}

// LoadData reads and validates a JSON optical config file from disk.
func LoadData(path string) (Data, error) {
	return LoadDataFromPath(path, nil)
}

// LoadDataFromPath reads and validates a JSON Data file from path, or from stdin when path is "-".
func LoadDataFromPath(path string, stdin io.Reader) (Data, error) {
	var r io.Reader
	if path == "-" {
		r = stdin
	} else {
		f, err := os.Open(path)
		if err != nil {
			return Data{}, err
		}
		defer f.Close()
		r = f
	}
	return DecodeData(r)
}

// SaveData writes a Data struct as formatted JSON to disk.
func SaveData(path string, data Data) error {
	if err := data.Validate(); err != nil {
		return err
	}
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(path, b, 0644)
}

// ResolveTracePath resolves tracePath relative to the directory containing
// configPath when tracePath is a non-empty relative path and configPath is a
// real file path (not empty or "-").
func ResolveTracePath(configPath, tracePath string) string {
	if tracePath == "" || tracePath == "-" || filepath.IsAbs(tracePath) || configPath == "" || configPath == "-" {
		return tracePath
	}
	return filepath.Join(filepath.Dir(configPath), tracePath)
}

// RelativeTracePath converts tracePath into a slash-separated path relative to
// configPath's directory when both refer to local files within a nearby
// directory tree, improving config portability across machines.
func RelativeTracePath(configPath, tracePath string) string {
	if tracePath == "" || tracePath == "-" || configPath == "" || configPath == "-" {
		return tracePath
	}
	absCfgDir, err1 := filepath.Abs(filepath.Dir(configPath))
	absTrace, err2 := filepath.Abs(tracePath)
	if err1 != nil || err2 != nil {
		return tracePath
	}
	rel, err := filepath.Rel(absCfgDir, absTrace)
	if err != nil {
		return tracePath
	}
	slashRel := filepath.ToSlash(rel)
	if !strings.HasPrefix(slashRel, "../../") {
		return slashRel
	}
	return filepath.Clean(tracePath)
}

// LoadTraceForData loads the frame trace referenced by d.Frame.Trace (if any),
// resolving relative paths against configPath's directory and honoring
// d.Frame.TraceFormat. Returns (nil, nil) when d.Frame.Trace is empty.
func LoadTraceForData(configPath string, d Data, stdin io.Reader) (*contour.Trace, error) {
	if d.Frame.Trace == "" {
		return nil, nil
	}
	p := ResolveTracePath(configPath, d.Frame.Trace)
	return contour.ReadFromPath(p, d.Frame.TraceFormat, stdin, d.Frame.A, d.Frame.B, d.Frame.DBL)
}

// DefaultData returns default parameters (plano Rx, CR-39 1.50 plastic, Bi-Aspheric,
// 52x40 DBL 18, 27 mm rotation center, 0.25 mm chamfer, 0.6 mm bevel depth, auto PD/OCH/BC/CT).
func DefaultData() Data {
	bc := EstimateBaseCurve(0, 0, DesignBiAspheric)
	ctMM := MinCenterThickness(DefaultRefractiveIndex, FamilyPlastic)
	sg := EstimateSpecificGravity(DefaultRefractiveIndex, FamilyPlastic)
	autoPD := (DefaultFrameA + DefaultFrameDBL) / 2.0
	autoOCH := DefaultFrameB / 2.0
	chamfer := DefaultChamferMM
	bevel := DefaultBevelDepthMM
	return Data{
		Version: CurrentSchemaVersion,
		RX: RX{
			OD: Refraction{Sph: 0, Cyl: 0, Axis: 0},
			OS: Refraction{Sph: 0, Cyl: 0, Axis: 0},
		},
		Lenses: LensPair{
			Material:   "1.50-plastic",
			Design:     DesignBiAspheric,
			Chamfer:    &chamfer,
			BevelDepth: &bevel,
			OD: LensParameters{
				Index:           DefaultRefractiveIndex,
				Family:          FamilyPlastic,
				SpecificGravity: sg,
				CenterThickness: ctMM,
				BaseCurve:       bc,
				Abbe:            DefaultAbbeNumber,
			},
			OS: LensParameters{
				Index:           DefaultRefractiveIndex,
				Family:          FamilyPlastic,
				SpecificGravity: sg,
				CenterThickness: ctMM,
				BaseCurve:       bc,
				Abbe:            DefaultAbbeNumber,
			},
		},
		Frame: Frame{
			A:   DefaultFrameA,
			B:   DefaultFrameB,
			DBL: DefaultFrameDBL,
		},
		Fit: Fit{
			CenterOfRotation: DefaultCenterOfRotationMM,
			OD:               EyeFit{PD: autoPD, OCH: autoOCH, CenterOfRotation: DefaultCenterOfRotationMM},
			OS:               EyeFit{PD: autoPD, OCH: autoOCH, CenterOfRotation: DefaultCenterOfRotationMM},
		},
	}
}

// FrameFromTrace returns a Frame whose A, B, and DBL dimensions are populated
// from tr (falling back to fallback for any dimension not present in tr).
func FrameFromTrace(tr *contour.Trace, fallback Frame) Frame {
	if tr == nil {
		return fallback
	}
	out := fallback
	out.A, out.B, out.DBL = tr.EffectiveDimensions(fallback.A, fallback.B, fallback.DBL)
	return out
}

// NormalizeOptions controls how zero/omitted fields in Data are resolved.
type NormalizeOptions struct {
	Design         LensDesign     // Fallback optical design when Data.Lenses.Design is DesignBiAspheric (or always when OverrideDesign is true)
	OverrideDesign bool           // Forces Design to take precedence over Data.Lenses.Design
	ODFamily       MaterialFamily // Fallback OD substrate family when not set on the lens
	OSFamily       MaterialFamily // Fallback OS substrate family when not set on the lens
	Trace          *contour.Trace // Optional frame trace used for contour-aware center thickness estimation
}

func scaledTraceContour(tr *contour.Trace, isOD bool, frame Frame) *contour.Contour {
	if tr == nil {
		return nil
	}
	c := tr.OD
	if !isOD {
		c = tr.OS
	}
	if c.AreaMM2() <= 0 {
		return nil
	}
	w, h := c.BoxingSize()
	if w > 0 && h > 0 && frame.A > 0 && frame.B > 0 &&
		(math.Abs(frame.A-w) > 1e-4 || math.Abs(frame.B-h) > 1e-4) {
		c = contour.Scale(c, frame.A/w, frame.B/h)
	}
	return &c
}

// NormalizeData resolves all zero/omitted fields in d using unified defaults:
//   - Version 0 -> CurrentSchemaVersion (1)
//   - Fit CenterOfRotation <= 0 -> 27.0 mm (propagated into OD and OS)
//   - Frame A <= 0 -> 52 mm, B <= 0 -> 40 mm, DBL <= 0 -> 18 mm
//   - Fit OD/OS PD <= 0 -> (A + DBL) / 2
//   - Fit OD/OS OCH <= 0 -> B / 2
//   - Lenses.Design -> opts.Design when OverrideDesign or unset
//   - Lenses.Chamfer nil -> 0.25 mm
//   - Lenses.BevelDepth nil -> 0.6 mm
//   - Lenses.Material preset & shared Index/Family/Abbe/SG inherited into OD and OS
//   - Lens Index <= 1.0 -> 1.50 (CR-39)
//   - Lens Family empty -> inferred plastic/glass from index
//   - Lens SpecificGravity <= 0 -> EstimateSpecificGravity(index, family)
//   - Lens Abbe <= 0 -> EstimateAbbe(index, family)
//   - Lens BaseCurve <= 0 -> EstimateBaseCurve(rx.Sph, rx.Cyl, design)
//   - Lens CenterThickness <= 0 -> EstimateCenterThickness(...) (in mm)
func NormalizeData(d Data, opts NormalizeOptions) Data {
	if d.Version == 0 {
		d.Version = CurrentSchemaVersion
	}

	odCor := d.Fit.EffectiveODCenterOfRotation()
	osCor := d.Fit.EffectiveOSCenterOfRotation()
	d.Fit.OD.CenterOfRotation = odCor
	d.Fit.OS.CenterOfRotation = osCor
	if d.Fit.CenterOfRotation <= 0 && math.Abs(odCor-osCor) < 1e-9 {
		d.Fit.CenterOfRotation = odCor
	}

	if d.Frame.A <= 0 {
		d.Frame.A = DefaultFrameA
	}
	if d.Frame.B <= 0 {
		d.Frame.B = DefaultFrameB
	}
	if d.Frame.DBL <= 0 {
		d.Frame.DBL = DefaultFrameDBL
	}

	autoPD := (d.Frame.A + d.Frame.DBL) / 2.0
	if d.Fit.OD.PD <= 0 {
		d.Fit.OD.PD = autoPD
	}
	if d.Fit.OS.PD <= 0 {
		d.Fit.OS.PD = autoPD
	}
	if d.Fit.OD.OCH <= 0 {
		d.Fit.OD.OCH = d.Frame.B / 2.0
	}
	if d.Fit.OS.OCH <= 0 {
		d.Fit.OS.OCH = d.Frame.B / 2.0
	}

	design := d.Lenses.Design
	if opts.OverrideDesign || design == DesignBiAspheric {
		if opts.OverrideDesign || opts.Design != DesignBiAspheric {
			design = opts.Design
		}
	}
	d.Lenses.Design = design

	chamfer := d.Lenses.EffectiveChamfer()
	d.Lenses.Chamfer = &chamfer
	bevel := d.Lenses.EffectiveBevelDepth()
	d.Lenses.BevelDepth = &bevel

	odEff := d.Lenses.EffectiveEye(true)
	osEff := d.Lenses.EffectiveEye(false)

	normalizeLens := func(isOD bool, rx Refraction, lp LensParameters, pd, och float64, optFam MaterialFamily) LensParameters {
		if lp.Index <= 1.0 {
			lp.Index = DefaultRefractiveIndex
		}
		fam := optFam
		if fam == FamilyAuto {
			fam = lp.Family
		}
		if fam == FamilyAuto {
			if IsGlassMaterial(lp.Index, fam) {
				fam = FamilyGlass
			} else {
				fam = FamilyPlastic
			}
		}
		lp.Family = fam

		if lp.SpecificGravity <= 0 {
			lp.SpecificGravity = EstimateSpecificGravity(lp.Index, fam)
		}
		if lp.Abbe <= 0 {
			lp.Abbe = EstimateAbbe(lp.Index, fam)
		}
		if lp.BaseCurve <= 0 {
			lp.BaseCurve = EstimateBaseCurve(rx.Sph, rx.Cyl, design)
		}
		if lp.CenterThickness <= 0 {
			lp.CenterThickness = EstimateCenterThickness(EyeGeometry{
				IsOD:         isOD,
				Sphere:       rx.Sph,
				Cyl:          rx.Cyl,
				Axis:         rx.Axis,
				BaseCurve:    lp.BaseCurve,
				FrameA:       d.Frame.A,
				FrameB:       d.Frame.B,
				FrameDBL:     d.Frame.DBL,
				MonoPD:       pd,
				OCHeight:     och,
				Index:        lp.Index,
				Family:       fam,
				ChamferMM:    chamfer,
				BevelDepthMM: bevel,
				Contour:      scaledTraceContour(opts.Trace, isOD, d.Frame),
				Design:       design,
			})
		}
		return lp
	}

	d.Lenses.OD = normalizeLens(true, d.RX.OD, odEff, d.Fit.OD.PD, d.Fit.OD.OCH, opts.ODFamily)
	d.Lenses.OS = normalizeLens(false, d.RX.OS, osEff, d.Fit.OS.PD, d.Fit.OS.OCH, opts.OSFamily)

	if spec, ok := LookupMaterialByKey(d.Lenses.Material); ok {
		d.Lenses.Material = spec.Key
	} else if d.Lenses.Material == "" || strings.EqualFold(d.Lenses.Material, "auto") {
		if math.Abs(d.Lenses.OD.Index-d.Lenses.OS.Index) < 1e-6 && d.Lenses.OD.Family == d.Lenses.OS.Family {
			if spec, ok := LookupMaterial(d.Lenses.OD.Index, d.Lenses.OD.Family); ok {
				d.Lenses.Material = spec.Key
			}
		}
	}

	return d
}

// Package dsm implements Dynamic Spectacle Magnification (DSM) ophthalmic
// optics modeling and plot rendering based on the framework of Arnulf Remole
// (School of Optometry, University of Waterloo).
//
// Rather than relying on thin-lens approximations such as Prentice's rule,
// [DSM] incorporates front base curve (F1), center thickness (T), refractive
// index (N), meridional back vertex power, and back-vertex-to-center-of-rotation
// distance (S) to evaluate off-axis induced prism, lateral chromatic aberration
// ([LateralColorSpread]), binocular prismatic difference ([PrismaticDifference],
// [ReadingPrismaticDifference]), and meridional magnification ellipses
// ([RenderMagnificationEllipse]).
//
// Prescription, lens material, and frame data types are defined in package
// [optics] (in millimeters); [MakeDSM] converts thickness and center-of-rotation
// distances to meters internally to match dioptric powers (1/m).
package dsm

import (
	"math"

	"jsouthworth.net/go/optical-analyzer/optics"
)

// DSM holds the Dynamic Spectacle Magnification model parameters for one eye.
// Internal physics quantities T (center thickness) and S (center of rotation distance)
// are stored in meters to match dioptric powers (1/m).
type DSM struct {
	T         float64 // Center thickness in meters
	N         float64 // Refractive index
	F1        float64 // Front base curve power in diopters
	S         float64 // Back-vertex-to-center-of-rotation distance in meters
	FvMajor   float64 // Back vertex power along AxisMajor (Sphere) in diopters
	FvMinor   float64 // Back vertex power along AxisMinor (Sphere + Cyl) in diopters
	AxisMajor float64 // Major principal meridian angle in degrees (0..180)
	AxisMinor float64 // Minor principal meridian angle in degrees (perpendicular to AxisMajor)
}

func minorAxis(axis float64) float64 {
	m := math.Mod(axis+90.0, 180.0)
	if m < 0 {
		m += 180.0
	}
	return m
}

// MakeDSM constructs OD and OS DSM optical models from data (converting
// CenterThickness and Fit center-of-rotation distances from mm to meters).
func MakeDSM(data optics.Data) (OD, OS DSM) {
	odLens := data.Lenses.EffectiveEye(true)
	osLens := data.Lenses.EffectiveEye(false)
	OD = DSM{
		T:         odLens.CenterThickness / 1000.0,
		N:         odLens.Index,
		F1:        odLens.BaseCurve,
		S:         data.Fit.EffectiveODCenterOfRotation() / 1000.0,
		FvMajor:   data.RX.OD.Sph,
		FvMinor:   data.RX.OD.Sph + data.RX.OD.Cyl,
		AxisMajor: data.RX.OD.Axis,
		AxisMinor: minorAxis(data.RX.OD.Axis),
	}
	OS = DSM{
		T:         osLens.CenterThickness / 1000.0,
		N:         osLens.Index,
		F1:        osLens.BaseCurve,
		S:         data.Fit.EffectiveOSCenterOfRotation() / 1000.0,
		FvMajor:   data.RX.OS.Sph,
		FvMinor:   data.RX.OS.Sph + data.RX.OS.Cyl,
		AxisMajor: data.RX.OS.Axis,
		AxisMinor: minorAxis(data.RX.OS.Axis),
	}
	return
}

func radians(degrees float64) float64 {
	return degrees * math.Pi / 180
}

func (m DSM) ω(meridian float64) float64 {
	mer := math.Mod(meridian, 180.0)
	if mer < 0 {
		mer += 180.0
	}
	return math.Abs(90.0 - math.Abs(m.AxisMajor-mer))
}

// Magnification returns the dynamic spectacle magnification factor for back
// vertex power Fv (in diopters), combining the lens shape factor
// 1 / (1 - (T/N)*F1) and power factor 1 / (1 - S*Fv).
func (m DSM) Magnification(Fv float64) float64 {
	return (1 / (1 - ((m.T / m.N) * m.F1))) * (1 / (1 - (m.S * Fv)))
}

// Ratio returns the meridional magnification ratio between the major and minor
// principal meridians: Magnification(FvMajor) / Magnification(FvMinor).
func (m DSM) Ratio() float64 {
	return m.Magnification(m.FvMajor) / m.Magnification(m.FvMinor)
}

// EllipseIncrement returns the fractional meridional magnification increment
// sin²(ω) * (Ratio() - 1) for angle ω (in degrees) from the minor principal
// meridian.
func (m DSM) EllipseIncrement(ω float64) float64 {
	return math.Pow(math.Sin(radians(ω)), 2) * (m.Ratio() - 1)
}

// ImageLocation returns the apparent radial image position (in mm) on the lens
// plane for an object point at radial distance orig (mm) along meridian
// (degrees).
func (m DSM) ImageLocation(orig, meridian float64) float64 {
	return orig * m.Magnification(m.FvMinor) *
		(1 + m.EllipseIncrement(m.ω(meridian)))
}

// Prism returns the magnitude of induced prism in prism diopters (Δ) at radial
// distance orig (mm) along meridian (degrees) relative to the eye's center of
// rotation S.
func (m DSM) Prism(orig, meridian float64) float64 {
	return (math.Abs(orig-m.ImageLocation(orig, meridian)) * 100) /
		(1000 * m.S)
}

// DegreeDistance converts an eye rotation angle degree (in degrees) into the
// corresponding radial distance (in mm) on the lens plane from the optical
// center, computed as tan(degree) * S * 1000.
func (m DSM) DegreeDistance(degree float64) float64 {
	return math.Tan(radians(degree)) * (m.S * 1000)
}

// ReadingInsetMM returns the monocular nasal convergence inset (mm) on the lens plane
// for a near target at readingDistMM (default 400 mm), accounting for the eye's
// center-of-rotation distance and DSM horizontal magnification (prismatic effect).
func (m DSM) ReadingInsetMM(monoPDMM, readingDistMM float64) float64 {
	if monoPDMM <= 0 || m.S <= 0 {
		return 0
	}
	if readingDistMM <= 0 {
		readingDistMM = optics.DefaultReadingDistanceMM
	}
	sMM := m.S * 1000.0
	geoInset := monoPDMM * (sMM / (readingDistMM + sMM))
	horizMag := m.ImageLocation(1.0, 0.0)
	if math.IsNaN(horizMag) || math.IsInf(horizMag, 0) || horizMag <= 0 {
		horizMag = 1.0
	}
	if horizMag < 0.5 {
		horizMag = 0.5
	} else if horizMag > 2.0 {
		horizMag = 2.0
	}
	return geoInset * horizMag
}

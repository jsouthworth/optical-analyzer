// Package contour represents 2D ophthalmic spectacle lens rim perimeters and
// handles frame trace serialization.
//
// A [Contour] stores a closed rim perimeter as [Steps] (360) uniform polar
// radii in millimeters centered on the lens boxing center, sampled every 1°
// counterclockwise from +X (0 rad) in front-view orientation (+X right, +Y up).
// In front view, OD (right eye) lies on the viewer's left (+X is nasal) and OS
// (left eye) lies on the viewer's right (-X is nasal).
//
// The package provides analytical superellipse generators ([Superellipse]),
// geometric rim operations ([Offset], [Scale], [Contour.Mirrored],
// [Contour.Contains], [Contour.AreaMM2], [Contour.BoxingSize]), and trace I/O
// for OMA/VCA (.trc/.oma), JSON, and SVG files ([Read], [Write]).
package contour

import (
	"fmt"
	"math"
)

// Steps is the uniform polar sample count (1 degree per step) used by Contour.
const Steps = 360

// DefaultGrooveDepthMM is the typical full-rim eyewire V-bevel / groove depth per side (mm).
const DefaultGrooveDepthMM = 0.6

// Common frame shape exponents (superellipse parameter n).
const (
	ShapeOval             = 2.0 // Standard ellipse/oval
	ShapeSoftRect         = 3.0 // P3 / Wayfarer / soft rounded corners
	ShapeSquircle         = 4.0 // Standard rectangular frame with rounded corners
	ShapeSharpRectangular = 8.0 // Almost sharp rectangular corners
)

// Point2D represents a 2D point in millimeters.
type Point2D struct {
	X, Y float64
}

// Contour represents a closed lens rim perimeter sampled at Steps (360) uniform
// polar angles (counterclockwise starting from +X / 0 radians) in millimeters
// relative to the lens boxing center, in front-view orientation (+Y = up).
type Contour struct {
	Radii [Steps]float64
}

// PointAt returns the (x, y) rim coordinate in millimeters for polar sample index i.
func (c Contour) PointAt(i int) (x, y float64) {
	theta := float64(i) * (2.0 * math.Pi / float64(Steps))
	r := c.Radii[i]
	return r * math.Cos(theta), r * math.Sin(theta)
}

// Mirrored returns a horizontally reflected contour (x -> -x), mapping between
// OD (viewer's left, +X nasal) and OS (viewer's right, -X nasal) orientations.
func (c Contour) Mirrored() Contour {
	var out Contour
	for i := 0; i < Steps; i++ {
		mirrorIdx := (Steps/2 - i + Steps) % Steps
		out.Radii[i] = c.Radii[mirrorIdx]
	}
	return out
}

// AreaMM2 returns the enclosed 2D area of the contour in mm^2 using exact polar integration.
func (c Contour) AreaMM2() float64 {
	dTheta := (2.0 * math.Pi) / float64(Steps)
	area := 0.0
	for _, r := range c.Radii {
		area += 0.5 * r * r * dTheta
	}
	return area
}

// Contains reports whether point (x, y) in mm (relative to the boxing center)
// lies inside or on the contour boundary.
func (c Contour) Contains(x, y float64) bool {
	r := math.Hypot(x, y)
	if r == 0 {
		return true
	}
	deg := math.Atan2(y, x) * (180.0 / math.Pi)
	if deg < 0 {
		deg += 360.0
	}
	pos := deg * (float64(Steps) / 360.0)
	i0 := int(math.Floor(pos)) % Steps
	i1 := (i0 + 1) % Steps
	frac := pos - math.Floor(pos)
	rBound := c.Radii[i0] + frac*(c.Radii[i1]-c.Radii[i0])
	return r <= rBound+1e-9
}

// BoxingSize returns the horizontal (A) and vertical (B) boxing dimensions of c in mm.
func (c Contour) BoxingSize() (width, height float64) {
	minX, maxX := math.Inf(1), math.Inf(-1)
	minY, maxY := math.Inf(1), math.Inf(-1)
	for i := 0; i < Steps; i++ {
		x, y := c.PointAt(i)
		if x < minX {
			minX = x
		}
		if x > maxX {
			maxX = x
		}
		if y < minY {
			minY = y
		}
		if y > maxY {
			maxY = y
		}
	}
	if math.IsInf(minX, 0) {
		return 0, 0
	}
	return maxX - minX, maxY - minY
}

// SuperellipseCoord computes sgn(t) * |t|^(2/n).
func SuperellipseCoord(t, n float64) float64 {
	if t == 0 {
		return 0
	}
	return math.Copysign(math.Pow(math.Abs(t), 2.0/n), t)
}

// Superellipse generates a Contour for a superellipse of boxing dimensions A x B
// and exponent n, sampled at exact polar angles so polar integration is exact.
func Superellipse(A, B, n float64) Contour {
	var c Contour
	if A <= 0 || B <= 0 {
		return c
	}
	if n <= 0 {
		n = ShapeOval
	}
	aHalf := A / 2.0
	bHalf := B / 2.0
	dTheta := (2.0 * math.Pi) / float64(Steps)
	for i := 0; i < Steps; i++ {
		theta := float64(i) * dTheta
		cx := math.Abs(math.Cos(theta)) / aHalf
		sy := math.Abs(math.Sin(theta)) / bHalf
		m := math.Max(cx, sy)
		if m > 0 {
			c.Radii[i] = 1.0 / (m * math.Pow(math.Pow(cx/m, n)+math.Pow(sy/m, n), 1.0/n))
		}
	}
	return c
}

// Offset shifts a counterclockwise Contour along its outward rim normal by offsetMM
// (positive expands into a frame eyewire groove; negative contracts inward).
func Offset(c Contour, offsetMM float64) Contour {
	if math.Abs(offsetMM) < 1e-9 {
		return c
	}
	shifted := make([]Point2D, Steps)
	for i := 0; i < Steps; i++ {
		px, py := c.PointAt(i)
		prevX, prevY := c.PointAt((i - 1 + Steps) % Steps)
		nextX, nextY := c.PointAt((i + 1) % Steps)

		// Outward unit normal is 90 deg clockwise from CCW tangent (next - prev).
		tx := nextX - prevX
		ty := nextY - prevY
		nx, ny := ty, -tx
		normLen := math.Hypot(nx, ny)
		if normLen > 1e-12 {
			px += offsetMM * (nx / normLen)
			py += offsetMM * (ny / normLen)
		}
		shifted[i] = Point2D{X: px, Y: py}
	}
	return ResamplePolygon(shifted)
}

// Scale rescales c horizontally by scaleX and vertically by scaleY.
func Scale(c Contour, scaleX, scaleY float64) Contour {
	if math.Abs(scaleX-scaleY) < 1e-9 {
		var out Contour
		for i, r := range c.Radii {
			out.Radii[i] = r * scaleX
		}
		return out
	}
	pts := make([]Point2D, Steps)
	for i := 0; i < Steps; i++ {
		x, y := c.PointAt(i)
		pts[i] = Point2D{X: x * scaleX, Y: y * scaleY}
	}
	return ResamplePolygon(pts)
}

// ResamplePolygon casts Steps polar rays from (0,0) and intersects them with
// the closed polygon edges to produce a Contour.
func ResamplePolygon(poly []Point2D) Contour {
	var c Contour
	n := len(poly)
	if n < 3 {
		return c
	}

	cross := func(ax, ay, bx, by float64) float64 {
		return ax*by - ay*bx
	}

	dTheta := (2.0 * math.Pi) / float64(Steps)
	for i := 0; i < Steps; i++ {
		theta := float64(i) * dTheta
		dx, dy := math.Cos(theta), math.Sin(theta)

		bestT := 0.0
		for j := 0; j < n; j++ {
			a := poly[j]
			b := poly[(j+1)%n]
			ex, ey := b.X-a.X, b.Y-a.Y
			denom := cross(dx, dy, ex, ey)
			if math.Abs(denom) < 1e-12 {
				continue
			}
			t := cross(a.X, a.Y, ex, ey) / denom
			u := cross(a.X, a.Y, dx, dy) / denom
			if t > bestT && u >= -1e-9 && u <= 1.0+1e-9 {
				bestT = t
			}
		}
		c.Radii[i] = bestT
	}
	return c
}

// ResampleEquiangular resamples an arbitrary-length (>= 8) equiangular polar
// radius slice onto a Steps-sample Contour via circular linear interpolation.
func ResampleEquiangular(samples []float64) (Contour, error) {
	var out Contour
	n := len(samples)
	if n < 8 {
		return out, fmt.Errorf("need at least 8 samples, got %d", n)
	}
	for i, r := range samples {
		if math.IsNaN(r) || math.IsInf(r, 0) || r <= 0 {
			return out, fmt.Errorf("sample[%d] = %v must be positive and finite", i, r)
		}
	}
	if n == Steps {
		copy(out.Radii[:], samples)
		return out, nil
	}
	for i := 0; i < Steps; i++ {
		pos := float64(i) * float64(n) / float64(Steps)
		i0 := int(math.Floor(pos)) % n
		i1 := (i0 + 1) % n
		frac := pos - math.Floor(pos)
		out.Radii[i] = samples[i0] + frac*(samples[i1]-samples[i0])
	}
	return out, nil
}

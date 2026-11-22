// Package tracer extracts 360-step ophthalmic lens rim contours from front-view
// photographs of eyeglass frames.
//
// [TraceFile] and [TraceImage] segment the left and right lens apertures from a
// PNG or JPEG photo using Otsu color-distance thresholding, Sobel edge sealing
// for thin metallic rims, rivet/temple-overlap filtering, convex-hull and
// Hermite occlusion bridging across temples and nosepad arms behind the lenses,
// raised-cosine Fourier smoothing, and bilateral OD/OS symmetrization.
//
// Extracted contours are scaled to physical millimeters using the stamped
// groove-to-groove frame boxing width A (and optional height B and eyewire
// groove depth) and can be exported to [contour.Trace] via
// [TracedFrame.ToContourTrace] or visually verified with [RenderDebugOverlay],
// [WriteDebugOverlay], and [WriteDebugOverlayFile].
package tracer

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/jpeg"
	"image/png"
	"io"
	"math"
	"os"
	"sort"

	"jsouthworth.net/go/optical-analyzer/contour"
)

// DefaultGrooveDepthMM is the typical eyewire groove depth per side (mm) hidden inside
// a full-rim frame front (stamped groove-to-groove A = visible aperture + 2*grooveDepth).
const DefaultGrooveDepthMM = contour.DefaultGrooveDepthMM

// TracedFrame holds lens contours and frame measurements extracted from a front-view photo.
type TracedFrame struct {
	ODContour     contour.Contour // Traced right-eye (viewer's left) lens rim contour in mm
	OSContour     contour.Contour // Traced left-eye (viewer's right) lens rim contour in mm
	InferredB     float64         // Vertical boxing height (B) in mm inferred from photo aspect ratio
	InferredDBL   float64         // Bridge / distance between lenses (DBL) in mm inferred from photo spacing
	GrooveDepthMM float64         // Eyewire groove depth per side (mm) applied when expanding visible apertures
	TiltRad       float64         // Detected frame horizontal roll angle in radians corrected during tracing

	srcImg      image.Image
	pivotX      float64
	pivotY      float64
	odBoxLX     float64
	odBoxLY     float64
	osBoxLX     float64
	osBoxLY     float64
	mmPerPxX    float64
	mmPerPxY    float64
	boxWidthMM  float64
	boxHeightMM float64
}

// ToContourTrace converts the traced frame into a contour.Trace using the
// provided frame dimensions (falling back to inferred B and DBL when <= 0).
func (tf *TracedFrame) ToContourTrace(name, source string, frameA, frameB, frameDBL float64) contour.Trace {
	effA := frameA
	if effA <= 0 {
		effA = tf.boxWidthMM
	}
	effB := frameB
	if effB <= 0 {
		effB = tf.InferredB
	}
	effDBL := frameDBL
	if effDBL <= 0 {
		effDBL = tf.InferredDBL
	}
	return contour.Trace{
		Name:          name,
		Source:        source,
		FrameA:        effA,
		FrameB:        effB,
		FrameDBL:      effDBL,
		GrooveDepthMM: tf.GrooveDepthMM,
		OD:            tf.ODContour,
		OS:            tf.OSContour,
	}
}

// OverlayParams configures boxing dimensions and optical center crosshair placement on debug overlays.
type OverlayParams struct {
	FrameA   float64 // Horizontal boxing width (A) in mm
	FrameB   float64 // Vertical boxing height (B) in mm (<= 0 falls back to InferredB)
	FrameDBL float64 // Bridge / distance between lenses (DBL) in mm (<= 0 falls back to InferredDBL)
	ODPD     float64 // Optional OD monocular PD in mm (<= 0 centers horizontally)
	OSPD     float64 // Optional OS monocular PD in mm (<= 0 centers horizontally)
	ODOCH    float64 // Optional OD optical center height from frame bottom in mm (<= 0 uses B/2)
	OSOCH    float64 // Optional OS optical center height from frame bottom in mm (<= 0 uses B/2)
}

type pixelPt struct {
	x, y int
}

type floatPt struct {
	x, y float64
}

type holeComponent struct {
	pixels []pixelPt
	area   int
	minX   int
	maxX   int
	minY   int
	maxY   int
	cx     float64
	cy     float64
}

// TraceFile loads a PNG or JPEG front-view frame photo (or reads from stdin when
// path is "-") and traces its OD/OS lens contours.
func TraceFile(path string, stdin io.Reader, frameA, frameB, grooveDepth float64) (*TracedFrame, error) {
	var r io.Reader
	if path == "-" {
		r = stdin
	} else {
		f, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("open frame image %q: %w", path, err)
		}
		defer f.Close()
		r = f
	}

	img, _, err := image.Decode(r)
	if err != nil {
		return nil, fmt.Errorf("decode frame image %q: %w", path, err)
	}

	return TraceImage(img, frameA, frameB, grooveDepth)
}

// TraceImage extracts OD (viewer's left) and OS (viewer's right) lens contours from a
// clean front-view frame photo. frameA (mm) is the stamped groove-to-groove boxing width;
// frameB (mm) is optional and inferred from the photo aspect ratio when <= 0.
func TraceImage(img image.Image, frameA, frameB, grooveDepth float64) (*TracedFrame, error) {
	if frameA <= 0 {
		return nil, fmt.Errorf("tracing a frame image requires frame A (boxing width in mm) > 0 to calibrate scale")
	}
	visibleA := frameA - 2.0*grooveDepth
	if visibleA <= 0 {
		return nil, fmt.Errorf("invalid groove-depth (%.2f mm/side): 2*groove-depth must be less than A (%.2f mm)", grooveDepth, frameA)
	}
	if frameB > 0 && frameB-2.0*grooveDepth <= 0 {
		return nil, fmt.Errorf("invalid groove-depth (%.2f mm/side): 2*groove-depth must be less than B (%.2f mm)", grooveDepth, frameB)
	}

	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w < 10 || h < 10 {
		return nil, fmt.Errorf("frame image too small (%dx%d)", w, h)
	}

	rgba := toNRGBA(img)
	mask, leftHole, rightHole, err := segmentLensHoles(rgba, w, h)
	if err != nil {
		return nil, err
	}

	odRawBoundary := extractHoleBoundary(leftHole, mask, w, h)
	osRawBoundary := extractHoleBoundary(rightHole, mask, w, h)

	// Compute tilt from the occlusion-resistant convex hull centers rather than raw pixel centroids
	// (an inward temple or nosepad intrusion shifts the raw area centroid slightly).
	odCenter := hullBoxCenter(odRawBoundary, leftHole.cx, leftHole.cy)
	osCenter := hullBoxCenter(osRawBoundary, rightHole.cx, rightHole.cy)

	tilt := math.Atan2(osCenter.y-odCenter.y, osCenter.x-odCenter.x)
	if math.Abs(tilt) > 30.0*(math.Pi/180.0) {
		return nil, fmt.Errorf("detected lens pair tilt (%.1f deg) exceeds 30 deg; check image orientation", tilt*180.0/math.Pi)
	}

	pivotX := (odCenter.x + osCenter.x) / 2.0
	pivotY := (odCenter.y + osCenter.y) / 2.0
	cosT := math.Cos(-tilt)
	sinT := math.Sin(-tilt)

	toLeveled := func(pt pixelPt) (float64, float64) {
		dx := float64(pt.x) - pivotX
		dy := float64(pt.y) - pivotY
		return dx*cosT - dy*sinT, dx*sinT + dy*cosT
	}

	odLeveled, odMinX, odMaxX, odMinY, odMaxY := transformPoints(odRawBoundary, toLeveled)
	osLeveled, osMinX, osMaxX, osMinY, osMaxY := transformPoints(osRawBoundary, toLeveled)

	odWidthPx := (odMaxX - odMinX) + 1.0
	osWidthPx := (osMaxX - osMinX) + 1.0
	odHeightPx := (odMaxY - odMinY) + 1.0
	osHeightPx := (osMaxY - osMinY) + 1.0

	avgWidthPx := (odWidthPx + osWidthPx) / 2.0
	avgHeightPx := (odHeightPx + osHeightPx) / 2.0
	if avgWidthPx <= 1 || avgHeightPx <= 1 {
		return nil, fmt.Errorf("traced lens apertures are degenerate (%.1fx%.1f px)", avgWidthPx, avgHeightPx)
	}

	mmPerPxX := visibleA / avgWidthPx
	inferredB := avgHeightPx*mmPerPxX + 2.0*grooveDepth

	mmPerPxY := mmPerPxX
	effectiveB := inferredB
	if frameB > 0 {
		visibleB := frameB - 2.0*grooveDepth
		mmPerPxY = visibleB / avgHeightPx
		effectiveB = frameB
	}

	dblPx := (osMinX - odMaxX) - 1.0
	inferredDBL := 0.0
	if dblPx > 0 {
		inferredDBL = dblPx*mmPerPxX - 2.0*grooveDepth
		if inferredDBL < 0 {
			inferredDBL = 0
		}
	}

	odBoxLX := (odMinX + odMaxX) / 2.0
	odBoxLY := (odMinY + odMaxY) / 2.0
	osBoxLX := (osMinX + osMaxX) / 2.0
	osBoxLY := (osMinY + osMaxY) / 2.0

	halfPxMM := 0.5 * (mmPerPxX + mmPerPxY) / 2.0
	odVisible := contour.Offset(resampleContour(odLeveled, odBoxLX, odBoxLY, mmPerPxX, mmPerPxY), halfPxMM)
	osVisible := contour.Offset(resampleContour(osLeveled, osBoxLX, osBoxLY, mmPerPxX, mmPerPxY), halfPxMM)
	odVisible, osVisible = symmetrizeContours(odVisible, osVisible)
	odContour := contour.Offset(odVisible, grooveDepth)
	osContour := contour.Offset(osVisible, grooveDepth)

	return &TracedFrame{
		ODContour:     odContour,
		OSContour:     osContour,
		InferredB:     inferredB,
		InferredDBL:   inferredDBL,
		GrooveDepthMM: grooveDepth,
		TiltRad:       tilt,
		srcImg:        img,
		pivotX:        pivotX,
		pivotY:        pivotY,
		odBoxLX:       odBoxLX,
		odBoxLY:       odBoxLY,
		osBoxLX:       osBoxLX,
		osBoxLY:       osBoxLY,
		mmPerPxX:      mmPerPxX,
		mmPerPxY:      mmPerPxY,
		boxWidthMM:    frameA,
		boxHeightMM:   effectiveB,
	}, nil
}

func toNRGBA(img image.Image) *image.NRGBA {
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), img, bounds.Min, draw.Src)
	return dst
}

func segmentLensHoles(img *image.NRGBA, w, h int) ([]bool, *holeComponent, *holeComponent, error) {
	candidates := buildCandidateMasks(img, w, h)
	for _, baseMask := range candidates {
		if left, right, ok := findLensHoles(baseMask, w, h); ok {
			return baseMask, left, right, nil
		}
		for _, radius := range []int{1, 2, 3} {
			closed := morphClose(baseMask, w, h, radius)
			if left, right, ok := findLensHoles(closed, w, h); ok {
				return closed, left, right, nil
			}
		}
	}
	return nil, nil, nil, fmt.Errorf("could not detect two enclosed lens openings in frame image")
}

func buildCandidateMasks(img *image.NRGBA, w, h int) [][]bool {
	var masks [][]bool

	transparentBorder := 0
	totalBorder := 0
	for x := 0; x < w; x++ {
		for _, y := range []int{0, h - 1} {
			totalBorder++
			if img.NRGBAAt(x, y).A < 128 {
				transparentBorder++
			}
		}
	}
	for y := 1; y < h-1; y++ {
		for _, x := range []int{0, w - 1} {
			totalBorder++
			if img.NRGBAAt(x, y).A < 128 {
				transparentBorder++
			}
		}
	}

	if transparentBorder > totalBorder/2 {
		alphaMask := make([]bool, w*h)
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				alphaMask[y*w+x] = img.NRGBAAt(x, y).A >= 128
			}
		}
		masks = append(masks, alphaMask)
		masks = append(masks, colorDistanceMask(img, w, h, 255, 255, 255, true))
		return masks
	}

	bgR, bgG, bgB := borderMedianRGB(img, w, h)
	otsuMask := colorDistanceMask(img, w, h, bgR, bgG, bgB, false)
	masks = append(masks, otsuMask)

	// Edge-enhanced + low-contrast rim masks capture thin metallic/pale wire rims (such as
	// browline/clubmaster frames) whose brightness is too close to the background for global
	// Otsu alone, while also sealing specular highlights along the wire.
	grad, borderGradP95 := sobelGradientMap(img, w, h)
	dists, borderDistP95 := colorDistanceMap(img, w, h, bgR, bgG, bgB)
	for _, cfg := range []struct {
		edgeFloor float64
		edgeScale float64
		distFloor float64
		distScale float64
	}{
		{20.0, 3.5, 42.0, 2.8},
		{13.0, 2.5, 30.0, 2.2},
	} {
		edgeThresh := math.Max(cfg.edgeFloor, borderGradP95*cfg.edgeScale)
		distThresh := math.Max(cfg.distFloor, borderDistP95*cfg.distScale)
		combined := make([]bool, w*h)
		for i := range combined {
			combined[i] = otsuMask[i] || grad[i] >= edgeThresh || dists[i] >= distThresh
		}
		masks = append(masks, combined)
	}

	return masks
}

func colorDistanceMap(img *image.NRGBA, w, h int, bgR, bgG, bgB uint8) ([]float64, float64) {
	dists := make([]float64, w*h)
	borderDists := make([]float64, 0, 2*(w+h))
	for y := 0; y < h; y++ {
		row := y * w
		for x := 0; x < w; x++ {
			c := img.NRGBAAt(x, y)
			dr := float64(c.R) - float64(bgR)
			dg := float64(c.G) - float64(bgG)
			db := float64(c.B) - float64(bgB)
			d := math.Sqrt(dr*dr + dg*dg + db*db)
			dists[row+x] = d
			if y == 0 || y == h-1 || x == 0 || x == w-1 {
				borderDists = append(borderDists, d)
			}
		}
	}
	p95 := 0.0
	if len(borderDists) > 0 {
		sort.Float64s(borderDists)
		p95 = borderDists[int(float64(len(borderDists)-1)*0.95)]
	}
	return dists, p95
}

func sobelGradientMap(img *image.NRGBA, w, h int) ([]float64, float64) {
	lum := make([]float64, w*h)
	for y := 0; y < h; y++ {
		row := y * w
		for x := 0; x < w; x++ {
			c := img.NRGBAAt(x, y)
			lum[row+x] = 0.299*float64(c.R) + 0.587*float64(c.G) + 0.114*float64(c.B)
		}
	}

	grad := make([]float64, w*h)
	borderGrads := make([]float64, 0, 2*(w+h))
	for y := 1; y < h-1; y++ {
		r0 := (y - 1) * w
		r1 := y * w
		r2 := (y + 1) * w
		for x := 1; x < w-1; x++ {
			gx := -lum[r0+x-1] + lum[r0+x+1] - 2*lum[r1+x-1] + 2*lum[r1+x+1] - lum[r2+x-1] + lum[r2+x+1]
			gy := -lum[r0+x-1] - 2*lum[r0+x] - lum[r0+x+1] + lum[r2+x-1] + 2*lum[r2+x] + lum[r2+x+1]
			g := math.Hypot(gx, gy) / 4.0
			grad[r1+x] = g
			if y == 1 || y == h-2 || x == 1 || x == w-2 {
				borderGrads = append(borderGrads, g)
			}
		}
	}

	p95 := 0.0
	if len(borderGrads) > 0 {
		sort.Float64s(borderGrads)
		idx := int(float64(len(borderGrads)-1) * 0.95)
		p95 = borderGrads[idx]
	}
	return grad, p95
}

func borderMedianRGB(img *image.NRGBA, w, h int) (uint8, uint8, uint8) {
	n := 2*w + 2*max(0, h-2)
	rs := make([]int, 0, n)
	gs := make([]int, 0, n)
	bs := make([]int, 0, n)

	add := func(x, y int) {
		c := img.NRGBAAt(x, y)
		rs = append(rs, int(c.R))
		gs = append(gs, int(c.G))
		bs = append(bs, int(c.B))
	}
	for x := 0; x < w; x++ {
		add(x, 0)
		add(x, h-1)
	}
	for y := 1; y < h-1; y++ {
		add(0, y)
		add(w-1, y)
	}

	sort.Ints(rs)
	sort.Ints(gs)
	sort.Ints(bs)
	mid := len(rs) / 2
	return uint8(rs[mid]), uint8(gs[mid]), uint8(bs[mid])
}

func colorDistanceMask(img *image.NRGBA, w, h int, bgR, bgG, bgB uint8, requireOpaque bool) []bool {
	dists := make([]float64, w*h)
	var hist [256]int
	total := 0

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := img.NRGBAAt(x, y)
			if requireOpaque && c.A < 128 {
				continue
			}
			dr := float64(c.R) - float64(bgR)
			dg := float64(c.G) - float64(bgG)
			db := float64(c.B) - float64(bgB)
			d := math.Sqrt(dr*dr + dg*dg + db*db)
			dists[y*w+x] = d
			bin := int(math.Round(d * 255.0 / 442.0))
			if bin > 255 {
				bin = 255
			}
			hist[bin]++
			total++
		}
	}

	threshBin := otsuThreshold(hist, total)
	thresh := float64(threshBin) * 442.0 / 255.0
	if thresh < 20.0 {
		thresh = 20.0
	}

	mask := make([]bool, w*h)
	for i, d := range dists {
		mask[i] = d >= thresh
	}
	return mask
}

func otsuThreshold(hist [256]int, total int) int {
	if total == 0 {
		return 0
	}
	sumAll := 0.0
	for i, count := range hist {
		sumAll += float64(i * count)
	}

	sumB := 0.0
	wB := 0
	maxVar := -1.0
	bestT := 0

	for t := 0; t < 256; t++ {
		wB += hist[t]
		if wB == 0 {
			continue
		}
		wF := total - wB
		if wF == 0 {
			break
		}
		sumB += float64(t * hist[t])
		mB := sumB / float64(wB)
		mF := (sumAll - sumB) / float64(wF)
		diff := mB - mF
		betweenVar := float64(wB) * float64(wF) * diff * diff
		if betweenVar > maxVar {
			maxVar = betweenVar
			bestT = t
		}
	}
	return bestT
}

// morphClose performs separable square morphological closing (dilate then erode),
// running in O(w*h) time independent of radius.
func morphClose(mask []bool, w, h, radius int) []bool {
	// Separable horizontal dilation
	hDil := make([]bool, w*h)
	for y := 0; y < h; y++ {
		row := y * w
		run := -radius - 1
		for x := 0; x < w; x++ {
			if mask[row+x] {
				run = x
			}
			if x-run <= radius {
				hDil[row+x] = true
			}
		}
		run = w + radius + 1
		for x := w - 1; x >= 0; x-- {
			if mask[row+x] {
				run = x
			}
			if run-x <= radius {
				hDil[row+x] = true
			}
		}
	}
	// Separable vertical dilation
	dil := make([]bool, w*h)
	for x := 0; x < w; x++ {
		run := -radius - 1
		for y := 0; y < h; y++ {
			if hDil[y*w+x] {
				run = y
			}
			if y-run <= radius {
				dil[y*w+x] = true
			}
		}
		run = h + radius + 1
		for y := h - 1; y >= 0; y-- {
			if hDil[y*w+x] {
				run = y
			}
			if run-y <= radius {
				dil[y*w+x] = true
			}
		}
	}
	// Separable horizontal erosion
	hEro := make([]bool, w*h)
	for y := 0; y < h; y++ {
		row := y * w
		run := -radius - 1
		for x := 0; x < w; x++ {
			if !dil[row+x] {
				run = x
			}
			if x-run > radius {
				hEro[row+x] = true
			}
		}
		run = w + radius + 1
		for x := w - 1; x >= 0; x-- {
			if !dil[row+x] {
				run = x
			}
			if run-x <= radius {
				hEro[row+x] = false
			}
		}
	}
	// Separable vertical erosion
	ero := make([]bool, w*h)
	for x := 0; x < w; x++ {
		run := -radius - 1
		for y := 0; y < h; y++ {
			if !hEro[y*w+x] {
				run = y
			}
			if y-run > radius {
				ero[y*w+x] = true
			}
		}
		run = h + radius + 1
		for y := h - 1; y >= 0; y-- {
			if !hEro[y*w+x] {
				run = y
			}
			if run-y <= radius {
				ero[y*w+x] = false
			}
		}
	}
	return ero
}

func findLensHoles(mask []bool, w, h int) (*holeComponent, *holeComponent, bool) {
	exterior := make([]bool, w*h)
	queue := make([]int, 0, 2*(w+h))

	enqueue := func(x, y int) {
		idx := y*w + x
		if !mask[idx] && !exterior[idx] {
			exterior[idx] = true
			queue = append(queue, idx)
		}
	}

	for x := 0; x < w; x++ {
		enqueue(x, 0)
		enqueue(x, h-1)
	}
	for y := 1; y < h-1; y++ {
		enqueue(0, y)
		enqueue(w-1, y)
	}

	for head := 0; head < len(queue); head++ {
		idx := queue[head]
		x, y := idx%w, idx/w
		if x > 0 {
			enqueue(x-1, y)
		}
		if x+1 < w {
			enqueue(x+1, y)
		}
		if y > 0 {
			enqueue(x, y-1)
		}
		if y+1 < h {
			enqueue(x, y+1)
		}
	}

	visited := make([]bool, w*h)
	var holes []*holeComponent

	for y := 1; y < h-1; y++ {
		for x := 1; x < w-1; x++ {
			idx := y*w + x
			if mask[idx] || exterior[idx] || visited[idx] {
				continue
			}

			comp := &holeComponent{
				minX: x, maxX: x,
				minY: y, maxY: y,
			}
			bfs := []int{idx}
			visited[idx] = true
			sumX, sumY := 0.0, 0.0

			for head := 0; head < len(bfs); head++ {
				cur := bfs[head]
				cx, cy := cur%w, cur/w
				comp.pixels = append(comp.pixels, pixelPt{cx, cy})
				sumX += float64(cx)
				sumY += float64(cy)
				if cx < comp.minX {
					comp.minX = cx
				}
				if cx > comp.maxX {
					comp.maxX = cx
				}
				if cy < comp.minY {
					comp.minY = cy
				}
				if cy > comp.maxY {
					comp.maxY = cy
				}

				for _, nb := range [4][2]int{{cx - 1, cy}, {cx + 1, cy}, {cx, cy - 1}, {cx, cy + 1}} {
					nx, ny := nb[0], nb[1]
					if nx < 0 || nx >= w || ny < 0 || ny >= h {
						continue
					}
					nIdx := ny*w + nx
					if !mask[nIdx] && !exterior[nIdx] && !visited[nIdx] {
						visited[nIdx] = true
						bfs = append(bfs, nIdx)
					}
				}
			}

			comp.area = len(comp.pixels)
			if comp.area < 25 {
				continue
			}
			comp.cx = sumX / float64(comp.area)
			comp.cy = sumY / float64(comp.area)
			holes = append(holes, comp)
		}
	}

	if len(holes) < 2 {
		return nil, nil, false
	}

	sort.Slice(holes, func(i, j int) bool {
		return holes[i].area > holes[j].area
	})

	minLensArea := max(25, (w*h)/500)
	limit := min(len(holes), 12)
	var bestLeft, bestRight *holeComponent
	bestScore := -1

	for i := 0; i < limit; i++ {
		for j := i + 1; j < limit; j++ {
			a, b := holes[i], holes[j]
			if a.area < minLensArea || b.area < minLensArea {
				continue
			}
			if a.cx > b.cx {
				a, b = b, a
			}
			minArea := min(a.area, b.area)
			maxArea := max(a.area, b.area)
			if float64(minArea) < 0.45*float64(maxArea) {
				continue
			}

			wA := a.maxX - a.minX + 1
			wB := b.maxX - b.minX + 1
			hA := a.maxY - a.minY + 1
			hB := b.maxY - b.minY + 1
			if wA < 5 || wB < 5 || hA < 5 || hB < 5 {
				continue
			}

			// Reject extreme aspect ratios (such as narrow endpiece hinge rivets).
			aspectA := float64(wA) / float64(hA)
			aspectB := float64(wB) / float64(hB)
			if aspectA < 0.40 || aspectA > 2.40 || aspectB < 0.40 || aspectB > 2.40 {
				continue
			}
			if float64(min(wA, wB)) < 0.65*float64(max(wA, wB)) ||
				float64(min(hA, hB)) < 0.65*float64(max(hA, hB)) {
				continue
			}

			// Reject overlapping holes or pairs too far apart (like opposite endpiece rivets).
			if float64(a.maxX) > float64(b.minX)+0.15*float64(min(wA, wB)) {
				continue
			}
			gap := float64(b.minX - a.maxX)
			if gap > 1.6*float64(max(wA, wB)) {
				continue
			}
			if math.Abs(a.cy-b.cy) > 0.65*float64(max(hA, hB)) {
				continue
			}

			score := a.area + b.area
			if score > bestScore {
				bestScore = score
				bestLeft, bestRight = a, b
			}
		}
	}

	if bestLeft == nil || bestRight == nil {
		return nil, nil, false
	}

	// Merge any enclosed corner sub-holes sliced off by a crossing temple arm on the temporal side.
	mergeTemporalSubHoles(bestLeft, holes, true)
	mergeTemporalSubHoles(bestRight, holes, false)

	return bestLeft, bestRight, true
}

func mergeTemporalSubHoles(mainHole *holeComponent, allHoles []*holeComponent, isOD bool) {
	mainW := float64(mainHole.maxX - mainHole.minX + 1)
	mainH := float64(mainHole.maxY - mainHole.minY + 1)
	minDim := math.Min(mainW, mainH)
	padX := 0.08 * mainW
	padY := 0.05 * mainH
	maxGapPx := math.Max(4.0, 0.10*mainW)
	minSubArea := max(40, int(0.006*float64(mainHole.area)))

	// Build fast lookup for mainHole pixels to check proximity.
	inMain := make(map[pixelPt]bool, len(mainHole.pixels))
	for _, p := range mainHole.pixels {
		inMain[p] = true
	}

	nearMain := func(sub *holeComponent) bool {
		// Sample up to ~64 pixels of sub and verify proximity to mainHole.
		step := max(1, len(sub.pixels)/64)
		r := int(math.Ceil(maxGapPx))
		for i := 0; i < len(sub.pixels); i += step {
			sp := sub.pixels[i]
			for dy := -r; dy <= r; dy += max(1, r/4) {
				for dx := -r; dx <= r; dx += max(1, r/4) {
					if inMain[pixelPt{sp.x + dx, sp.y + dy}] {
						return true
					}
				}
			}
		}
		return false
	}

	for _, sub := range allHoles {
		if sub == mainHole || sub.area < minSubArea || float64(sub.area) >= 0.35*float64(mainHole.area) {
			continue
		}
		// Only consider pockets on the temporal (outer) half so we never absorb bridge holes.
		if isOD && sub.cx > mainHole.cx {
			continue
		}
		if !isOD && sub.cx < mainHole.cx {
			continue
		}
		if float64(sub.minY) < float64(mainHole.minY)-padY || float64(sub.maxY) > float64(mainHole.maxY)+padY {
			continue
		}
		if isOD && float64(sub.minX) < float64(mainHole.minX)-padX {
			continue
		}
		if !isOD && float64(sub.maxX) > float64(mainHole.maxX)+padX {
			continue
		}
		subW := float64(sub.maxX - sub.minX + 1)
		subH := float64(sub.maxY - sub.minY + 1)
		// Reject small endpiece rivets / screws (both round pins and wide decorative bars).
		if math.Max(subW, subH) < 0.10*minDim {
			continue
		}
		if subW/subH > 3.0 && (float64(sub.maxX) < float64(mainHole.minX) || float64(sub.minX) > float64(mainHole.maxX)) {
			continue
		}
		if !nearMain(sub) {
			continue
		}
		mainHole.pixels = append(mainHole.pixels, sub.pixels...)
		mainHole.area += sub.area
		if sub.minX < mainHole.minX {
			mainHole.minX = sub.minX
		}
		if sub.maxX > mainHole.maxX {
			mainHole.maxX = sub.maxX
		}
		if sub.minY < mainHole.minY {
			mainHole.minY = sub.minY
		}
		if sub.maxY > mainHole.maxY {
			mainHole.maxY = sub.maxY
		}
	}
}

func extractHoleBoundary(hole *holeComponent, mask []bool, w, h int) []pixelPt {
	inHole := make([]bool, w*h)
	for _, p := range hole.pixels {
		inHole[p.y*w+p.x] = true
	}

	boundary := make([]pixelPt, 0, len(hole.pixels)/4)
	for _, p := range hole.pixels {
		isEdge := p.x == 0 || p.x == w-1 || p.y == 0 || p.y == h-1 ||
			!inHole[p.y*w+(p.x-1)] || !inHole[p.y*w+(p.x+1)] ||
			!inHole[(p.y-1)*w+p.x] || !inHole[(p.y+1)*w+p.x]
		if isEdge {
			boundary = append(boundary, p)
		}
	}
	return boundary
}

func transformPoints(pts []pixelPt, fn func(pixelPt) (float64, float64)) ([]floatPt, float64, float64, float64, float64) {
	out := make([]floatPt, len(pts))
	minX, maxX := math.Inf(1), math.Inf(-1)
	minY, maxY := math.Inf(1), math.Inf(-1)

	for i, p := range pts {
		x, y := fn(p)
		out[i] = floatPt{x, y}
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
	return out, minX, maxX, minY, maxY
}

func hullBoxCenter(pts []pixelPt, fallbackX, fallbackY float64) floatPt {
	if len(pts) == 0 {
		return floatPt{fallbackX, fallbackY}
	}
	minX, maxX := pts[0].x, pts[0].x
	minY, maxY := pts[0].y, pts[0].y
	for _, p := range pts[1:] {
		if p.x < minX {
			minX = p.x
		}
		if p.x > maxX {
			maxX = p.x
		}
		if p.y < minY {
			minY = p.y
		}
		if p.y > maxY {
			maxY = p.y
		}
	}
	return floatPt{float64(minX+maxX) / 2.0, float64(minY+maxY) / 2.0}
}

// convexHull2D computes the counterclockwise 2D convex hull of pts via Andrew's monotone chain.
func convexHull2D(pts []contour.Point2D) []contour.Point2D {
	if len(pts) <= 3 {
		out := make([]contour.Point2D, len(pts))
		copy(out, pts)
		return out
	}
	sorted := make([]contour.Point2D, len(pts))
	copy(sorted, pts)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].X != sorted[j].X {
			return sorted[i].X < sorted[j].X
		}
		return sorted[i].Y < sorted[j].Y
	})

	cross := func(o, a, b contour.Point2D) float64 {
		return (a.X-o.X)*(b.Y-o.Y) - (a.Y-o.Y)*(b.X-o.X)
	}

	lower := make([]contour.Point2D, 0, len(sorted))
	for _, p := range sorted {
		for len(lower) >= 2 && cross(lower[len(lower)-2], lower[len(lower)-1], p) <= 0 {
			lower = lower[:len(lower)-1]
		}
		lower = append(lower, p)
	}

	upper := make([]contour.Point2D, 0, len(sorted))
	for i := len(sorted) - 1; i >= 0; i-- {
		p := sorted[i]
		for len(upper) >= 2 && cross(upper[len(upper)-2], upper[len(upper)-1], p) <= 0 {
			upper = upper[:len(upper)-1]
		}
		upper = append(upper, p)
	}

	hull := make([]contour.Point2D, 0, len(lower)+len(upper)-2)
	hull = append(hull, lower[:len(lower)-1]...)
	hull = append(hull, upper[:len(upper)-1]...)
	return hull
}

func resampleContour(leveledPts []floatPt, boxLX, boxLY, mmPerPxX, mmPerPxY float64) contour.Contour {
	var maxR [contour.Steps]float64
	var hasVal [contour.Steps]bool

	for _, p := range leveledPts {
		mmX := (p.x - boxLX) * mmPerPxX
		mmY := -(p.y - boxLY) * mmPerPxY

		r := math.Hypot(mmX, mmY)
		angle := math.Atan2(mmY, mmX)
		if angle < 0 {
			angle += 2.0 * math.Pi
		}
		bin := int(math.Round(angle/(2.0*math.Pi)*float64(contour.Steps))) % contour.Steps
		// Take the outermost boundary radius per angular bin so inward peninsulas
		// (temple tips, nosepad loops) with visible rim behind them are skipped.
		if !hasVal[bin] || r > maxR[bin] {
			maxR[bin] = r
			hasVal[bin] = true
		}
	}

	var raw [contour.Steps]float64
	populated := make([]int, 0, contour.Steps)
	for i := 0; i < contour.Steps; i++ {
		if hasVal[i] {
			raw[i] = maxR[i]
			populated = append(populated, i)
		}
	}

	if len(populated) > 0 && len(populated) < contour.Steps {
		for k := 0; k < len(populated); k++ {
			i0 := populated[k]
			i1 := populated[(k+1)%len(populated)]
			span := (i1 - i0 + contour.Steps) % contour.Steps
			if span <= 1 {
				continue
			}
			r0, r1 := raw[i0], raw[i1]
			for s := 1; s < span; s++ {
				t := float64(s) / float64(span)
				raw[(i0+s)%contour.Steps] = r0 + t*(r1-r0)
			}
		}
	}

	// Suppress narrow outward leaks (e.g. 2-4 px specular reflection glints on thin metallic
	// bottom rims) before computing the convex hull so outward noise does not stretch the hull.
	meanRaw := 0.0
	for _, r := range raw {
		meanRaw += r
	}
	meanRaw /= float64(contour.Steps)
	if meanRaw > 0 {
		raw = despikeOutward(raw, 6, 0.008*meanRaw)
	}

	// Build convex hull from the despiked outer-envelope polar points so inward occlusions
	// (nosepad arms and crossing temples) can be detected and bridged cleanly.
	hullInput := make([]contour.Point2D, contour.Steps)
	dTheta := (2.0 * math.Pi) / float64(contour.Steps)
	for i := 0; i < contour.Steps; i++ {
		th := float64(i) * dTheta
		hullInput[i] = contour.Point2D{X: raw[i] * math.Cos(th), Y: raw[i] * math.Sin(th)}
	}
	hullPoly := convexHull2D(hullInput)
	if len(hullPoly) >= 3 {
		hullC := contour.ResamplePolygon(hullPoly)
		meanR := 0.0
		for _, r := range hullC.Radii {
			meanR += r
		}
		meanR /= float64(contour.Steps)

		if meanR > 0 {
			shoulderTol := 0.004 * meanR
			defectTol := 0.012 * meanR

			var inDefect [contour.Steps]bool
			for i := 0; i < contour.Steps; i++ {
				inDefect[i] = (hullC.Radii[i] - raw[i]) > shoulderTol
			}

			// Find a starting index outside any defect run so circular wrapping is handled cleanly.
			anchor := -1
			for i := 0; i < contour.Steps; i++ {
				if !inDefect[i] {
					anchor = i
					break
				}
			}

			if anchor >= 0 {
				repaired := raw
				estimateSlope := func(idx, dir int) float64 {
					// Estimate dr/dbin over up to 8 clean bins stepping away from the defect.
					const win = 8
					var sumK, sumR, sumKK, sumKR float64
					n := 0
					for k := 0; k <= win; k++ {
						b := (idx + dir*k + contour.Steps) % contour.Steps
						if k > 0 && inDefect[b] {
							break
						}
						fk := float64(dir * k)
						fr := raw[b]
						sumK += fk
						sumR += fr
						sumKK += fk * fk
						sumKR += fk * fr
						n++
					}
					if n < 2 {
						return 0
					}
					denom := float64(n)*sumKK - sumK*sumK
					if math.Abs(denom) < 1e-9 {
						return 0
					}
					return (float64(n)*sumKR - sumK*sumR) / denom
				}

				for offset := 0; offset < contour.Steps; {
					cur := (anchor + offset) % contour.Steps
					if !inDefect[cur] {
						offset++
						continue
					}
					runStart := offset
					maxDef := 0.0
					for offset < contour.Steps && inDefect[(anchor+offset)%contour.Steps] {
						b := (anchor + offset) % contour.Steps
						if d := hullC.Radii[b] - raw[b]; d > maxDef {
							maxDef = d
						}
						offset++
					}
					span := offset - runStart
					if maxDef >= defectTol && span <= 110 {
						i0 := (anchor + runStart - 1 + contour.Steps) % contour.Steps
						i1 := (anchor + offset) % contour.Steps
						r0, r1 := raw[i0], raw[i1]
						maxSlope := 0.02 * meanR
						clampSlope := func(s float64) float64 {
							if s < -maxSlope {
								return -maxSlope
							}
							if s > maxSlope {
								return maxSlope
							}
							return s
						}
						m0 := clampSlope(estimateSlope(i0, -1))
						m1 := clampSlope(estimateSlope(i1, +1))
						L := float64(span + 1)
						arcFactor := 1.0 / math.Cos(float64(span)*math.Pi/360.0)
						if arcFactor > 1.04 {
							arcFactor = 1.04
						}

						for k := 1; k <= span; k++ {
							b := (i0 + k) % contour.Steps
							t := float64(k) / L
							t2 := t * t
							t3 := t2 * t
							h00 := 2*t3 - 3*t2 + 1
							h10 := t3 - 2*t2 + t
							h01 := -2*t3 + 3*t2
							h11 := t3 - t2
							rInterp := h00*r0 + h10*(m0*L) + h01*r1 + h11*(m1*L)
							lo := hullC.Radii[b] * 0.995
							hi := hullC.Radii[b] * arcFactor
							if rInterp < lo {
								rInterp = lo
							}
							if rInterp > hi {
								rInterp = hi
							}
							repaired[b] = rInterp
						}
					}
				}
				raw = repaired
			}
		}
	}

	// Raised-cosine Fourier low-pass smoothing eliminates high-frequency pixel/specular
	// rim jaggedness while preserving smooth curves and frame corners without shrinkage.
	return fourierSmoothContour(raw, 10, 18)
}

// symmetrizeContours combines OD and mirrored OS contours into a bilaterally symmetric pair.
// Where both eyes closely agree, averaging cancels pixel noise and slight camera yaw; where
// one side dips inward (asymmetric brow/temple/nosepad occlusion), the blend smoothly favors
// the unoccluded outer rim so both eyes share a clean, symmetric lens contour.
func symmetrizeContours(od, osC contour.Contour) (contour.Contour, contour.Contour) {
	osMir := osC.Mirrored()
	var merged [contour.Steps]float64
	for i := 0; i < contour.Steps; i++ {
		lo := math.Min(od.Radii[i], osMir.Radii[i])
		hi := math.Max(od.Radii[i], osMir.Radii[i])
		diff := hi - lo
		// Smoothstep weight from 0.5 (average when close) to 1.0 (unoccluded outer rim when diff >= 0.5 mm).
		t := math.Min(1.0, diff/0.5)
		w := 0.5 + 0.5*t*t*(3.0-2.0*t)
		merged[i] = w*hi + (1.0-w)*lo
	}
	symOD := fourierSmoothContour(merged, 10, 18)
	return symOD, symOD.Mirrored()
}

// fourierSmoothContour filters a 360-step periodic polar radius profile by keeping
// harmonics 0..passK at full strength, applying a raised-cosine roll-off over
// passK..cutK (to avoid Gibbs ringing), and removing higher-frequency rim jitter.
func fourierSmoothContour(raw [contour.Steps]float64, passK, cutK int) contour.Contour {
	const N = contour.Steps
	twoPiOverN := 2.0 * math.Pi / float64(N)

	a0 := 0.0
	for i := 0; i < N; i++ {
		a0 += raw[i]
	}
	a0 /= float64(N)

	cosCoeff := make([]float64, cutK+1)
	sinCoeff := make([]float64, cutK+1)
	for k := 1; k <= cutK; k++ {
		w := 1.0
		if k > passK {
			t := float64(k-passK) / float64(cutK-passK)
			w = 0.5 * (1.0 + math.Cos(math.Pi*t))
		}
		var ak, bk float64
		fk := float64(k) * twoPiOverN
		for i := 0; i < N; i++ {
			ang := fk * float64(i)
			ak += raw[i] * math.Cos(ang)
			bk += raw[i] * math.Sin(ang)
		}
		cosCoeff[k] = (2.0 / float64(N)) * ak * w
		sinCoeff[k] = (2.0 / float64(N)) * bk * w
	}

	var c contour.Contour
	for i := 0; i < N; i++ {
		val := a0
		for k := 1; k <= cutK; k++ {
			ang := float64(k) * twoPiOverN * float64(i)
			val += cosCoeff[k]*math.Cos(ang) + sinCoeff[k]*math.Sin(ang)
		}
		if val < 0 {
			val = 0
		}
		c.Radii[i] = val
	}
	return c
}

// despikeOutward clamps narrow outward bumps exceeding tol above the local circular median.
func despikeOutward(raw [contour.Steps]float64, halfWin int, tol float64) [contour.Steps]float64 {
	out := raw
	buf := make([]float64, 2*halfWin+1)
	for i := 0; i < contour.Steps; i++ {
		for k := -halfWin; k <= halfWin; k++ {
			buf[k+halfWin] = raw[(i+k+contour.Steps)%contour.Steps]
		}
		sort.Float64s(buf)
		med := buf[halfWin]
		if raw[i] > med+tol {
			out[i] = med
		}
	}
	return out
}

// RenderDebugOverlay renders the traced contours, boxing rectangles, and optical centers
// over the original frame photo and returns the annotated RGBA image.
func RenderDebugOverlay(traced *TracedFrame, params OverlayParams) (*image.RGBA, error) {
	if traced == nil || traced.srcImg == nil {
		return nil, fmt.Errorf("no traced image available for overlay")
	}

	bounds := traced.srcImg.Bounds()
	canvas := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(canvas, canvas.Bounds(), traced.srcImg, bounds.Min, draw.Src)

	cosInv := math.Cos(traced.TiltRad)
	sinInv := math.Sin(traced.TiltRad)

	toImagePx := func(boxLX, boxLY, mmX, mmY float64) (int, int) {
		lx := boxLX + mmX/traced.mmPerPxX
		ly := boxLY - mmY/traced.mmPerPxY
		px := traced.pivotX + lx*cosInv - ly*sinInv
		py := traced.pivotY + lx*sinInv + ly*cosInv
		return int(math.Round(px)), int(math.Round(py))
	}

	green := color.RGBA{0, 220, 0, 255}
	cyan := color.RGBA{0, 180, 255, 255}
	yellow := color.RGBA{255, 215, 0, 255}
	red := color.RGBA{255, 40, 40, 255}

	halfW := traced.boxWidthMM / 2.0
	halfH := traced.boxHeightMM / 2.0

	drawBox := func(boxLX, boxLY float64) {
		corners := [4][2]float64{
			{-halfW, halfH},
			{halfW, halfH},
			{halfW, -halfH},
			{-halfW, -halfH},
		}
		for i := 0; i < 4; i++ {
			x0, y0 := toImagePx(boxLX, boxLY, corners[i][0], corners[i][1])
			x1, y1 := toImagePx(boxLX, boxLY, corners[(i+1)%4][0], corners[(i+1)%4][1])
			drawLine(canvas, x0, y0, x1, y1, yellow)
		}
	}

	drawContour := func(boxLX, boxLY float64, c contour.Contour, col color.RGBA) {
		for i := 0; i < contour.Steps; i++ {
			ax, ay := c.PointAt(i)
			bx, by := c.PointAt((i + 1) % contour.Steps)
			x0, y0 := toImagePx(boxLX, boxLY, ax, ay)
			x1, y1 := toImagePx(boxLX, boxLY, bx, by)
			drawLine(canvas, x0, y0, x1, y1, col)
		}
	}

	drawBox(traced.odBoxLX, traced.odBoxLY)
	drawBox(traced.osBoxLX, traced.osBoxLY)
	drawContour(traced.odBoxLX, traced.odBoxLY, traced.ODContour, green)
	drawContour(traced.osBoxLX, traced.osBoxLY, traced.OSContour, cyan)

	frameCenter := (params.FrameA + params.FrameDBL) / 2.0
	odPD := params.ODPD
	if odPD <= 0 {
		odPD = frameCenter
	}
	osPD := params.OSPD
	if osPD <= 0 {
		osPD = frameCenter
	}

	odOCX := frameCenter - odPD
	osOCX := -(frameCenter - osPD)

	odOCY := 0.0
	if params.ODOCH > 0 && params.FrameB > 0 {
		odOCY = params.ODOCH - params.FrameB/2.0
	}
	osOCY := 0.0
	if params.OSOCH > 0 && params.FrameB > 0 {
		osOCY = params.OSOCH - params.FrameB/2.0
	}

	odX, odY := toImagePx(traced.odBoxLX, traced.odBoxLY, odOCX, odOCY)
	osX, osY := toImagePx(traced.osBoxLX, traced.osBoxLY, osOCX, osOCY)
	drawCrosshair(canvas, odX, odY, 6, red)
	drawCrosshair(canvas, osX, osY, 6, red)

	return canvas, nil
}

// WriteDebugOverlayFile renders the traced contours, boxing rectangles, and optical centers
// over the original frame photo and writes it as a PNG file.
func WriteDebugOverlayFile(path string, traced *TracedFrame, params OverlayParams) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create debug overlay %q: %w", path, err)
	}
	defer f.Close()

	if err := WriteDebugOverlay(f, traced, params); err != nil {
		return fmt.Errorf("write debug overlay %q: %w", path, err)
	}
	return nil
}

// WriteDebugOverlay writes the annotated debug PNG to w.
func WriteDebugOverlay(w io.Writer, traced *TracedFrame, params OverlayParams) error {
	canvas, err := RenderDebugOverlay(traced, params)
	if err != nil {
		return err
	}
	return png.Encode(w, canvas)
}

func drawLine(img *image.RGBA, x0, y0, x1, y1 int, col color.RGBA) {
	dx := x1 - x0
	if dx < 0 {
		dx = -dx
	}
	dy := y1 - y0
	if dy < 0 {
		dy = -dy
	}
	sx := -1
	if x0 < x1 {
		sx = 1
	}
	sy := -1
	if y0 < y1 {
		sy = 1
	}
	err := dx - dy
	bounds := img.Bounds()

	for {
		if image.Pt(x0, y0).In(bounds) {
			img.SetRGBA(x0, y0, col)
		}
		if x0 == x1 && y0 == y1 {
			break
		}
		e2 := 2 * err
		if e2 > -dy {
			err -= dy
			x0 += sx
		}
		if e2 < dx {
			err += dx
			y0 += sy
		}
	}
}

func drawCrosshair(img *image.RGBA, cx, cy, arm int, col color.RGBA) {
	drawLine(img, cx-arm, cy, cx+arm, cy, col)
	drawLine(img, cx, cy-arm, cx, cy+arm, col)
}

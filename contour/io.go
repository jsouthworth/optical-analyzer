package contour

import (
	"bufio"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
)

// CurrentTraceSchemaVersion is the current JSON trace schema version.
const CurrentTraceSchemaVersion = 1

// Format identifies a supported frame trace serialization format.
type Format string

// Supported trace serialization formats.
const (
	FormatOMA  Format = "oma"  // OMA/VCA Data Communication Standard (.trc / .oma)
	FormatJSON Format = "json" // JSON polar radii representation (.json)
	FormatSVG  Format = "svg"  // Scalable Vector Graphics front-view paths (.svg)
)

// Trace represents a reusable lens contour pair and its boxing dimensions.
type Trace struct {
	Name          string  // Display name or frame model identifier
	Source        string  // Source file path or origin label (e.g., "stdin")
	Job           string  // Optional OMA JOB identifier header
	Model         string  // Optional OMA MODEL header
	FrameA        float64 // Horizontal boxing width (A) in mm
	FrameB        float64 // Vertical boxing height (B) in mm
	FrameDBL      float64 // Distance between lenses / bridge width (DBL) in mm
	GrooveDepthMM float64 // Eyewire V-bevel / groove depth per side in mm
	OD            Contour // Right-eye (viewer's left) lens contour
	OS            Contour // Left-eye (viewer's right) lens contour
	ODMirrored    bool    // True if OD was synthesized by mirroring OS
	OSMirrored    bool    // True if OS was synthesized by mirroring OD
}

type traceJSON struct {
	Version     int       `json:"version,omitempty"`
	Name        string    `json:"name,omitempty"`
	Source      string    `json:"source,omitempty"`
	Job         string    `json:"job,omitempty"`
	Model       string    `json:"model,omitempty"`
	FrameA      float64   `json:"frame_a"`
	FrameB      float64   `json:"frame_b"`
	FrameDBL    float64   `json:"frame_dbl,omitempty"`
	GrooveDepth float64   `json:"groove_depth,omitempty"`
	ODRadii     []float64 `json:"od_radii"`
	OSRadii     []float64 `json:"os_radii,omitempty"`
}

// ResolveFormat determines the trace format from an explicit format flag or a file path.
// When formatFlag is empty and path is "" or "-" (stdin/stdout) or has no extension,
// FormatOMA is returned as the default.
func ResolveFormat(formatFlag, path string) (Format, error) {
	if norm := strings.ToLower(strings.TrimSpace(formatFlag)); norm != "" {
		switch norm {
		case "oma", "trc":
			return FormatOMA, nil
		case "json":
			return FormatJSON, nil
		case "svg":
			return FormatSVG, nil
		default:
			return "", fmt.Errorf("unsupported trace format %q (use oma, json, or svg)", formatFlag)
		}
	}

	if path == "" || path == "-" {
		return FormatOMA, nil
	}

	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case "", ".trc", ".oma":
		return FormatOMA, nil
	case ".json":
		return FormatJSON, nil
	case ".svg":
		return FormatSVG, nil
	default:
		return "", fmt.Errorf("unsupported trace file extension %q (use .trc, .oma, .json, or .svg)", ext)
	}
}

// WriteToPath writes tr to path, or to stdout when path is "" or "-".
func WriteToPath(path, formatFlag string, stdout io.Writer, tr Trace) error {
	format, err := ResolveFormat(formatFlag, path)
	if err != nil {
		return err
	}
	if path == "" || path == "-" {
		return Write(stdout, format, tr)
	}

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create trace file %q: %w", path, err)
	}
	defer f.Close()

	if err := Write(f, format, tr); err != nil {
		return fmt.Errorf("write trace file %q: %w", path, err)
	}
	return nil
}

// Write serializes tr in the requested format (FormatOMA, FormatJSON, or FormatSVG).
func Write(w io.Writer, format Format, tr Trace) error {
	switch Format(strings.ToLower(strings.TrimSpace(string(format)))) {
	case FormatOMA, "trc":
		return writeOMA(w, tr)
	case FormatJSON:
		return writeJSON(w, tr)
	case FormatSVG:
		return writeSVG(w, tr)
	default:
		return fmt.Errorf("unsupported trace format %q (use oma, json, or svg)", format)
	}
}

// ReadFromPath loads a trace from path, or from stdin when path is "-".
func ReadFromPath(path, formatFlag string, stdin io.Reader, overrideA, overrideB, overrideDBL float64) (*Trace, error) {
	format, err := ResolveFormat(formatFlag, path)
	if err != nil {
		return nil, err
	}

	var r io.Reader
	if path == "-" {
		r = stdin
	} else {
		f, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("open trace file %q: %w", path, err)
		}
		defer f.Close()
		r = f
	}

	tr, err := Read(r, format, overrideA, overrideB, overrideDBL)
	if err != nil {
		if path == "-" {
			return nil, fmt.Errorf("read trace from stdin: %w", err)
		}
		return nil, fmt.Errorf("read trace file %q: %w", path, err)
	}
	if tr.Source == "" {
		if path == "-" {
			tr.Source = "stdin"
		} else {
			tr.Source = path
		}
	}
	return tr, nil
}

// Read parses a trace in the given format (FormatOMA, FormatJSON, or FormatSVG) and applies optional
// dimension overrides (rescaling contours when overrideA or overrideB differ from stored sizes).
func Read(r io.Reader, format Format, overrideA, overrideB, overrideDBL float64) (*Trace, error) {
	var (
		tr  *Trace
		err error
	)
	switch Format(strings.ToLower(strings.TrimSpace(string(format)))) {
	case FormatOMA, "trc":
		tr, err = readOMA(r)
	case FormatJSON:
		tr, err = readJSON(r)
	case FormatSVG:
		return readSVG(r, overrideA, overrideB, overrideDBL)
	default:
		return nil, fmt.Errorf("unsupported trace format %q (use oma, json, or svg)", format)
	}
	if err != nil {
		return nil, err
	}

	boxA, boxB := tr.OD.BoxingSize()
	if tr.FrameA <= 0 {
		tr.FrameA = boxA
	}
	if tr.FrameB <= 0 {
		tr.FrameB = boxB
	}

	scaleX, scaleY := 1.0, 1.0
	if overrideA > 0 && tr.FrameA > 0 {
		scaleX = overrideA / tr.FrameA
		tr.FrameA = overrideA
	}
	if overrideB > 0 && tr.FrameB > 0 {
		scaleY = overrideB / tr.FrameB
		tr.FrameB = overrideB
	}
	if overrideDBL > 0 {
		tr.FrameDBL = overrideDBL
	}

	if math.Abs(scaleX-1.0) > 1e-6 || math.Abs(scaleY-1.0) > 1e-6 {
		tr.OD = Scale(tr.OD, scaleX, scaleY)
		tr.OS = Scale(tr.OS, scaleX, scaleY)
	}
	return tr, nil
}

func writeJSON(w io.Writer, tr Trace) error {
	payload := traceJSON{
		Version:     CurrentTraceSchemaVersion,
		Name:        tr.Name,
		Source:      tr.Source,
		Job:         tr.Job,
		Model:       tr.Model,
		FrameA:      tr.FrameA,
		FrameB:      tr.FrameB,
		FrameDBL:    tr.FrameDBL,
		GrooveDepth: tr.GrooveDepthMM,
		ODRadii:     append([]float64(nil), tr.OD.Radii[:]...),
		OSRadii:     append([]float64(nil), tr.OS.Radii[:]...),
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func readJSON(r io.Reader) (*Trace, error) {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	var payload traceJSON
	if err := dec.Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode json trace: %w", err)
	}
	if payload.Version != 0 && payload.Version != CurrentTraceSchemaVersion {
		return nil, fmt.Errorf("unsupported json trace version %d (supported: %d)", payload.Version, CurrentTraceSchemaVersion)
	}
	if len(payload.ODRadii) < 8 {
		return nil, fmt.Errorf("json trace requires at least 8 od_radii samples, got %d", len(payload.ODRadii))
	}
	od, err := ResampleEquiangular(payload.ODRadii)
	if err != nil {
		return nil, fmt.Errorf("invalid od_radii: %w", err)
	}

	var osC Contour
	if len(payload.OSRadii) > 0 {
		if len(payload.OSRadii) < 8 {
			return nil, fmt.Errorf("json trace os_radii requires at least 8 samples, got %d", len(payload.OSRadii))
		}
		osC, err = ResampleEquiangular(payload.OSRadii)
		if err != nil {
			return nil, fmt.Errorf("invalid os_radii: %w", err)
		}
	} else {
		osC = od.Mirrored()
	}

	return &Trace{
		Name:          payload.Name,
		Source:        payload.Source,
		Job:           payload.Job,
		Model:         payload.Model,
		FrameA:        payload.FrameA,
		FrameB:        payload.FrameB,
		FrameDBL:      payload.FrameDBL,
		GrooveDepthMM: payload.GrooveDepth,
		OD:            od,
		OS:            osC,
	}, nil
}

// writeOMA writes a standard OMA/VCA ASCII frame trace (.trc/.oma) with
// 360 equiangular radii in hundredths of a millimeter (0.01 mm).
func writeOMA(w io.Writer, tr Trace) error {
	bw := bufio.NewWriter(w)
	fmt.Fprintln(bw, "JOB=1")
	if tr.FrameA > 0 {
		fmt.Fprintf(bw, "HBOX=%.2f\n", tr.FrameA)
	}
	if tr.FrameB > 0 {
		fmt.Fprintf(bw, "VBOX=%.2f\n", tr.FrameB)
	}
	if tr.FrameDBL > 0 {
		fmt.Fprintf(bw, "DBL=%.2f\n", tr.FrameDBL)
	}

	writeEye := func(eyeCode string, c Contour) {
		fmt.Fprintf(bw, "TRCFMT=1;%d;E;%s;F\n", Steps, eyeCode)
		const perLine = 12
		for i := 0; i < Steps; i += perLine {
			end := min(i+perLine, Steps)
			parts := make([]string, 0, end-i)
			for j := i; j < end; j++ {
				parts = append(parts, strconv.Itoa(int(math.Round(c.Radii[j]*100.0))))
			}
			fmt.Fprintf(bw, "R=%s\n", strings.Join(parts, ";"))
		}
	}

	writeEye("R", tr.OD)
	writeEye("L", tr.OS)
	return bw.Flush()
}

// writeSVG exports a standalone millimeter-scaled SVG showing both OD and OS contours.
func writeSVG(w io.Writer, tr Trace) error {
	frameA, frameB := tr.FrameA, tr.FrameB
	if frameA <= 0 || frameB <= 0 {
		boxA, boxB := tr.OD.BoxingSize()
		if frameA <= 0 {
			frameA = boxA
		}
		if frameB <= 0 {
			frameB = boxB
		}
	}
	frameDBL := tr.FrameDBL
	if frameDBL <= 0 {
		frameDBL = 18.0
	}

	const pad = 5.0
	totalW := 2*frameA + frameDBL + 2*pad
	totalH := frameB + 2*pad
	odCenterX := pad + frameA/2.0
	osCenterX := pad + frameA + frameDBL + frameA/2.0
	centerY := pad + frameB/2.0

	formatPoints := func(cx float64, c Contour) string {
		var b strings.Builder
		for i := 0; i < Steps; i++ {
			if i > 0 {
				b.WriteByte(' ')
			}
			x, y := c.PointAt(i)
			fmt.Fprintf(&b, "%.3f,%.3f", cx+x, centerY-y)
		}
		return b.String()
	}

	bw := bufio.NewWriter(w)
	fmt.Fprintf(bw, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %.2f %.2f" width="%.2fmm" height="%.2fmm" data-frame-a="%.4f" data-frame-b="%.4f" data-frame-dbl="%.4f">`+"\n",
		totalW, totalH, totalW, totalH, frameA, frameB, tr.FrameDBL)
	fmt.Fprintf(bw, `  <polygon id="od" fill="none" stroke="#111" stroke-width="0.5" points="%s"/>`+"\n", formatPoints(odCenterX, tr.OD))
	fmt.Fprintf(bw, `  <polygon id="os" fill="none" stroke="#111" stroke-width="0.5" points="%s"/>`+"\n", formatPoints(osCenterX, tr.OS))
	fmt.Fprintln(bw, "</svg>")
	return bw.Flush()
}

// readSVG parses closed shapes from SVG <polygon>, <polyline>, and <path> elements.
// Supports either a single lens outline (mirrored for OS) or a side-by-side OD/OS pair.
func readSVG(r io.Reader, overrideA, overrideB, overrideDBL float64) (*Trace, error) {
	dec := xml.NewDecoder(r)
	var (
		svgA, svgB, svgDBL float64
		polygons           [][]Point2D
	)

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parse svg xml: %w", err)
		}

		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}

		switch strings.ToLower(se.Name.Local) {
		case "svg":
			for _, attr := range se.Attr {
				switch strings.ToLower(attr.Name.Local) {
				case "data-frame-a":
					if v, err := strconv.ParseFloat(strings.TrimSpace(attr.Value), 64); err == nil && v > 0 {
						svgA = v
					}
				case "data-frame-b":
					if v, err := strconv.ParseFloat(strings.TrimSpace(attr.Value), 64); err == nil && v > 0 {
						svgB = v
					}
				case "data-frame-dbl":
					if v, err := strconv.ParseFloat(strings.TrimSpace(attr.Value), 64); err == nil && v > 0 {
						svgDBL = v
					}
				}
			}
		case "polygon", "polyline":
			for _, attr := range se.Attr {
				if strings.ToLower(attr.Name.Local) == "points" {
					pts, err := parseSVGPoints(attr.Value)
					if err != nil {
						return nil, fmt.Errorf("parse svg %s points: %w", se.Name.Local, err)
					}
					if len(pts) >= 3 {
						polygons = append(polygons, pts)
					}
				}
			}
		case "path":
			for _, attr := range se.Attr {
				if strings.ToLower(attr.Name.Local) == "d" {
					subpaths, err := parseSVGPath(attr.Value)
					if err != nil {
						return nil, fmt.Errorf("parse svg path: %w", err)
					}
					for _, sp := range subpaths {
						if len(sp) >= 3 {
							polygons = append(polygons, sp)
						}
					}
				}
			}
		}
	}

	if len(polygons) == 0 {
		return nil, fmt.Errorf("svg contains no polygon, polyline, or path outlines")
	}

	effectiveA := overrideA
	if effectiveA <= 0 {
		effectiveA = svgA
	}
	if effectiveA <= 0 {
		return nil, fmt.Errorf("svg trace requires -a (frame boxing width in mm) > 0 to calibrate scale")
	}

	type polyBox struct {
		pts                    []Point2D
		minX, maxX, minY, maxY float64
		w, h, cx               float64
	}
	boxes := make([]polyBox, 0, len(polygons))
	for _, pts := range polygons {
		minX, maxX := math.Inf(1), math.Inf(-1)
		minY, maxY := math.Inf(1), math.Inf(-1)
		for _, p := range pts {
			if p.X < minX {
				minX = p.X
			}
			if p.X > maxX {
				maxX = p.X
			}
			if p.Y < minY {
				minY = p.Y
			}
			if p.Y > maxY {
				maxY = p.Y
			}
		}
		w, h := maxX-minX, maxY-minY
		if w > 1e-6 && h > 1e-6 {
			boxes = append(boxes, polyBox{
				pts: pts, minX: minX, maxX: maxX, minY: minY, maxY: maxY,
				w: w, h: h, cx: (minX + maxX) / 2.0,
			})
		}
	}
	if len(boxes) == 0 {
		return nil, fmt.Errorf("svg contains only degenerate outlines")
	}

	toContour := func(pb polyBox, scaleX, scaleY float64) Contour {
		cx := (pb.minX + pb.maxX) / 2.0
		cy := (pb.minY + pb.maxY) / 2.0
		centered := make([]Point2D, len(pb.pts))
		for i, p := range pb.pts {
			centered[i] = Point2D{
				X: (p.X - cx) * scaleX,
				Y: -(p.Y - cy) * scaleY, // SVG Y goes down; optical Y goes up
			}
		}
		return ResamplePolygon(centered)
	}

	var (
		odC, osC     Contour
		effectiveB   = overrideB
		effectiveDBL = overrideDBL
	)
	if effectiveB <= 0 {
		effectiveB = svgB
	}
	if effectiveDBL <= 0 {
		effectiveDBL = svgDBL
	}

	if len(boxes) >= 2 {
		bestI, bestJ := 0, 1
		if boxes[bestI].cx > boxes[bestJ].cx {
			bestI, bestJ = bestJ, bestI
		}
		left, right := boxes[bestI], boxes[bestJ]
		avgW := (left.w + right.w) / 2.0
		avgH := (left.h + right.h) / 2.0
		scaleX := effectiveA / avgW
		scaleY := scaleX
		if effectiveB > 0 {
			scaleY = effectiveB / avgH
		} else {
			effectiveB = avgH * scaleX
		}
		if effectiveDBL <= 0 {
			gap := right.minX - left.maxX
			if gap > 0 {
				effectiveDBL = gap * scaleX
			}
		}
		odC = toContour(left, scaleX, scaleY)
		osC = toContour(right, scaleX, scaleY)
	} else {
		single := boxes[0]
		scaleX := effectiveA / single.w
		scaleY := scaleX
		if effectiveB > 0 {
			scaleY = effectiveB / single.h
		} else {
			effectiveB = single.h * scaleX
		}
		odC = toContour(single, scaleX, scaleY)
		osC = odC.Mirrored()
	}

	return &Trace{
		FrameA:   effectiveA,
		FrameB:   effectiveB,
		FrameDBL: effectiveDBL,
		OD:       odC,
		OS:       osC,
	}, nil
}

func parseSVGPoints(raw string) ([]Point2D, error) {
	nums, err := extractNumbers(raw)
	if err != nil {
		return nil, err
	}
	if len(nums)%2 != 0 {
		return nil, fmt.Errorf("odd number of coordinates (%d)", len(nums))
	}
	pts := make([]Point2D, 0, len(nums)/2)
	for i := 0; i < len(nums); i += 2 {
		pts = append(pts, Point2D{X: nums[i], Y: nums[i+1]})
	}
	return pts, nil
}

func parseSVGPath(d string) ([][]Point2D, error) {
	tokens := tokenizeSVGPath(d)
	var (
		subpaths  [][]Point2D
		current   []Point2D
		cx, cy    float64
		sx, sy    float64
		cmd       byte
		idx       int
		hasOrigin bool
	)

	nextNum := func() (float64, error) {
		if idx >= len(tokens) {
			return 0, fmt.Errorf("unexpected end of path data after %c", cmd)
		}
		v, err := strconv.ParseFloat(tokens[idx], 64)
		if err != nil {
			return 0, fmt.Errorf("invalid path number %q: %w", tokens[idx], err)
		}
		idx++
		return v, nil
	}

	isCommand := func(tok string) bool {
		return len(tok) == 1 && unicode.IsLetter(rune(tok[0]))
	}

	flush := func() {
		if len(current) >= 3 {
			subpaths = append(subpaths, current)
		}
		current = nil
	}

	for idx < len(tokens) {
		if isCommand(tokens[idx]) {
			cmd = tokens[idx][0]
			idx++
		} else if cmd == 0 {
			return nil, fmt.Errorf("path data must start with a command, got %q", tokens[idx])
		}

		switch cmd {
		case 'M', 'm':
			x, err := nextNum()
			if err != nil {
				return nil, err
			}
			y, err := nextNum()
			if err != nil {
				return nil, err
			}
			flush()
			if cmd == 'm' && hasOrigin {
				cx += x
				cy += y
			} else {
				cx, cy = x, y
			}
			sx, sy = cx, cy
			hasOrigin = true
			current = append(current, Point2D{X: cx, Y: cy})
			if cmd == 'M' {
				cmd = 'L'
			} else {
				cmd = 'l'
			}
		case 'L', 'l':
			x, err := nextNum()
			if err != nil {
				return nil, err
			}
			y, err := nextNum()
			if err != nil {
				return nil, err
			}
			if cmd == 'l' {
				cx += x
				cy += y
			} else {
				cx, cy = x, y
			}
			current = append(current, Point2D{X: cx, Y: cy})
		case 'H', 'h':
			x, err := nextNum()
			if err != nil {
				return nil, err
			}
			if cmd == 'h' {
				cx += x
			} else {
				cx = x
			}
			current = append(current, Point2D{X: cx, Y: cy})
		case 'V', 'v':
			y, err := nextNum()
			if err != nil {
				return nil, err
			}
			if cmd == 'v' {
				cy += y
			} else {
				cy = y
			}
			current = append(current, Point2D{X: cx, Y: cy})
		case 'C', 'c':
			x1, err := nextNum()
			if err != nil {
				return nil, err
			}
			y1, err := nextNum()
			if err != nil {
				return nil, err
			}
			x2, err := nextNum()
			if err != nil {
				return nil, err
			}
			y2, err := nextNum()
			if err != nil {
				return nil, err
			}
			x3, err := nextNum()
			if err != nil {
				return nil, err
			}
			y3, err := nextNum()
			if err != nil {
				return nil, err
			}
			if cmd == 'c' {
				x1 += cx
				y1 += cy
				x2 += cx
				y2 += cy
				x3 += cx
				y3 += cy
			}
			const segs = 16
			p0x, p0y := cx, cy
			for s := 1; s <= segs; s++ {
				t := float64(s) / float64(segs)
				u := 1.0 - t
				px := u*u*u*p0x + 3*u*u*t*x1 + 3*u*t*t*x2 + t*t*t*x3
				py := u*u*u*p0y + 3*u*u*t*y1 + 3*u*t*t*y2 + t*t*t*y3
				current = append(current, Point2D{X: px, Y: py})
			}
			cx, cy = x3, y3
		case 'Q', 'q':
			x1, err := nextNum()
			if err != nil {
				return nil, err
			}
			y1, err := nextNum()
			if err != nil {
				return nil, err
			}
			x2, err := nextNum()
			if err != nil {
				return nil, err
			}
			y2, err := nextNum()
			if err != nil {
				return nil, err
			}
			if cmd == 'q' {
				x1 += cx
				y1 += cy
				x2 += cx
				y2 += cy
			}
			const segs = 12
			p0x, p0y := cx, cy
			for s := 1; s <= segs; s++ {
				t := float64(s) / float64(segs)
				u := 1.0 - t
				px := u*u*p0x + 2*u*t*x1 + t*t*x2
				py := u*u*p0y + 2*u*t*y1 + t*t*y2
				current = append(current, Point2D{X: px, Y: py})
			}
			cx, cy = x2, y2
		case 'Z', 'z':
			cx, cy = sx, sy
			flush()
			cmd = 0
		default:
			return nil, fmt.Errorf("unsupported SVG path command %q", string(cmd))
		}
	}
	flush()
	return subpaths, nil
}

func tokenizeSVGPath(d string) []string {
	var tokens []string
	i := 0
	n := len(d)
	for i < n {
		ch := d[i]
		if ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' || ch == ',' {
			i++
			continue
		}
		if (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') {
			tokens = append(tokens, d[i:i+1])
			i++
			continue
		}
		start := i
		if ch == '+' || ch == '-' {
			i++
		}
		hasDot := false
		for i < n {
			c := d[i]
			if c >= '0' && c <= '9' {
				i++
			} else if c == '.' && !hasDot {
				hasDot = true
				i++
			} else if (c == 'e' || c == 'E') && i+1 < n {
				i++
				if d[i] == '+' || d[i] == '-' {
					i++
				}
			} else {
				break
			}
		}
		if i > start {
			tokens = append(tokens, d[start:i])
		} else {
			i++
		}
	}
	return tokens
}

func extractNumbers(raw string) ([]float64, error) {
	tokens := tokenizeSVGPath(raw)
	nums := make([]float64, 0, len(tokens))
	for _, tok := range tokens {
		v, err := strconv.ParseFloat(tok, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid coordinate %q: %w", tok, err)
		}
		nums = append(nums, v)
	}
	return nums, nil
}

package contour

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"path/filepath"
	"strconv"
	"strings"
)

type rawOMABlock struct {
	format      int
	count       int
	spacing     string // "E" or "U"/"A"
	side        string // "R", "L", "B"
	asciiRadii  []float64
	asciiAngles []float64
	binRadii    []byte
	binAngles   []byte
	hasRadii    bool
}

// EffectiveDimensions returns the frame boxing dimensions (A, B, DBL) in mm
// derived from t, preferring explicit header/stored values, then measured
// contour dimensions, and finally the provided fallback values.
func (t Trace) EffectiveDimensions(fallbackA, fallbackB, fallbackDBL float64) (a, b, dbl float64) {
	a, b, dbl = fallbackA, fallbackB, fallbackDBL
	boxA, boxB := t.OD.BoxingSize()
	if boxA <= 0 || boxB <= 0 {
		boxA, boxB = t.OS.BoxingSize()
	}
	if t.FrameA > 0 {
		a = t.FrameA
	} else if boxA > 0 {
		a = boxA
	}
	if t.FrameB > 0 {
		b = t.FrameB
	} else if boxB > 0 {
		b = boxB
	}
	if t.FrameDBL > 0 {
		dbl = t.FrameDBL
	}
	return a, b, dbl
}

// Summary returns a concise human-readable description of the trace.
func (t Trace) Summary() string {
	label := t.Name
	if label == "" {
		label = t.Model
	}
	if label == "" && t.Source != "" && t.Source != "stdin" {
		label = filepath.Base(t.Source)
	}
	if label == "" {
		label = "Frame Trace"
	}
	a, b, dbl := t.EffectiveDimensions(0, 0, 0)
	parts := []string{label}
	if a > 0 && b > 0 {
		parts = append(parts, fmt.Sprintf("%.1f×%.1f mm", a, b))
	}
	if dbl > 0 {
		parts = append(parts, fmt.Sprintf("DBL %.1f", dbl))
	}
	if t.OSMirrored {
		parts = append(parts, "OD mirrored to OS")
	} else if t.ODMirrored {
		parts = append(parts, "OS mirrored to OD")
	}
	return strings.Join(parts, " • ")
}

func readOMA(r io.Reader) (*Trace, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	return ParseOMA(raw)
}

// ParseOMA parses raw OMA/VCA Data Communication Standard trace data (.trc / .oma),
// supporting ASCII (Format 1 absolute and Format 5 differential) and binary
// (Format 2 absolute, Format 3 differential, and Format 4 packed binary with
// ESC 0x1B transport unescaping), as well as equiangular (E) and unequiangular
// (U/A with A= records) sampling. Single-eye traces are automatically mirrored
// horizontally for the opposite eye.
func ParseOMA(raw []byte) (*Trace, error) {
	tr := &Trace{}
	var (
		cur          *rawOMABlock
		defaultSide  = "R"
		hasOD, hasOS bool
		odW, odH     float64
		osW, osH     float64
		hboxR, hboxL float64
		vboxR, vboxL float64
	)

	flush := func() error {
		if cur == nil || !cur.hasRadii {
			cur = nil
			return nil
		}
		c, w, h, err := decodeOMABlock(cur)
		if err != nil {
			return err
		}
		switch cur.side {
		case "L":
			tr.OS = c
			osW, osH = w, h
			hasOS = true
		case "R", "B", "":
			tr.OD = c
			odW, odH = w, h
			hasOD = true
		default:
			if !hasOD {
				tr.OD = c
				odW, odH = w, h
				hasOD = true
			} else {
				tr.OS = c
				osW, osH = w, h
				hasOS = true
			}
		}
		cur = nil
		return nil
	}

	for _, line := range splitOMARecords(raw) {
		line = trimOMAFrameBytes(line)
		if len(line) == 0 || line[0] == '#' || line[0] == ';' {
			continue
		}

		eqIdx := bytes.IndexByte(line, '=')
		if eqIdx <= 0 {
			continue
		}
		key := strings.ToUpper(strings.TrimSpace(string(line[:eqIdx])))
		valBytes := line[eqIdx+1:]

		switch key {
		case "JOB":
			tr.Job = strings.TrimSpace(string(valBytes))
		case "MODEL":
			tr.Model = strings.TrimSpace(string(valBytes))
		case "MNAME", "FRAM":
			if tr.Name == "" {
				tr.Name = strings.TrimSpace(string(valBytes))
			}
		case "DBL":
			if v, ok := parseOMAFirstFloat(string(valBytes)); ok && v > 0 {
				tr.FrameDBL = v
			}
		case "HBOX":
			r, l := parseOMAPairFloats(string(valBytes))
			if r > 0 {
				hboxR = r
			}
			if l > 0 {
				hboxL = l
			}
		case "VBOX":
			r, l := parseOMAPairFloats(string(valBytes))
			if r > 0 {
				vboxR = r
			}
			if l > 0 {
				vboxL = l
			}
		case "SIDE":
			s := strings.ToUpper(strings.TrimSpace(string(valBytes)))
			if strings.HasPrefix(s, "L") {
				defaultSide = "L"
				if cur != nil {
					cur.side = "L"
				}
			} else if strings.HasPrefix(s, "R") {
				defaultSide = "R"
				if cur != nil {
					cur.side = "R"
				}
			}
		case "TRCFMT":
			if err := flush(); err != nil {
				return nil, err
			}
			blk, err := parseOMATRCFMT(string(valBytes), defaultSide)
			if err != nil {
				return nil, err
			}
			cur = blk
		case "R":
			if cur == nil {
				cur = &rawOMABlock{format: 1, spacing: "E", side: defaultSide}
			}
			cur.hasRadii = true
			if cur.format == 1 || cur.format == 5 {
				vals, err := parseOMAASCIINumbers(string(valBytes))
				if err != nil {
					return nil, fmt.Errorf("parsing R= record: %w", err)
				}
				cur.asciiRadii = append(cur.asciiRadii, vals...)
			} else {
				cur.binRadii = append(cur.binRadii, unescapeOMABinary(valBytes)...)
			}
		case "A":
			if cur == nil {
				continue
			}
			if cur.format == 1 || cur.format == 5 {
				vals, err := parseOMAASCIINumbers(string(valBytes))
				if err != nil {
					return nil, fmt.Errorf("parsing A= record: %w", err)
				}
				cur.asciiAngles = append(cur.asciiAngles, vals...)
			} else {
				cur.binAngles = append(cur.binAngles, unescapeOMABinary(valBytes)...)
			}
		}
	}

	if err := flush(); err != nil {
		return nil, err
	}

	if !hasOD && !hasOS {
		return nil, fmt.Errorf("no valid OMA trace data (TRCFMT / R=) found")
	}

	// If only one eye was traced, mirror it horizontally for the other eye.
	if hasOD && !hasOS {
		tr.OS = tr.OD.Mirrored()
		tr.OSMirrored = true
	} else if hasOS && !hasOD {
		tr.OD = tr.OS.Mirrored()
		tr.ODMirrored = true
	}

	if hboxR > 0 {
		tr.FrameA = hboxR
	} else if hboxL > 0 {
		tr.FrameA = hboxL
	}
	if vboxR > 0 {
		tr.FrameB = vboxR
	} else if vboxL > 0 {
		tr.FrameB = vboxL
	}

	if tr.FrameA <= 0 {
		if odW > 0 {
			tr.FrameA = odW
		} else if osW > 0 {
			tr.FrameA = osW
		}
	}
	if tr.FrameB <= 0 {
		if odH > 0 {
			tr.FrameB = odH
		} else if osH > 0 {
			tr.FrameB = osH
		}
	}

	return tr, nil
}

func splitOMARecords(raw []byte) [][]byte {
	var records [][]byte
	start := 0
	for i := 0; i < len(raw); i++ {
		if raw[i] == '\r' || raw[i] == '\n' {
			if i > start {
				records = append(records, raw[start:i])
			}
			if raw[i] == '\r' && i+1 < len(raw) && raw[i+1] == '\n' {
				i++
			}
			start = i + 1
		}
	}
	if start < len(raw) {
		records = append(records, raw[start:])
	}
	return records
}

func trimOMAFrameBytes(b []byte) []byte {
	for len(b) > 0 {
		c := b[0]
		if c == 0x00 || c == 0x11 || c == 0x12 || c == 0x13 || c == 0x14 ||
			c == 0x1C || c == 0x1D || c == 0x1E || c == ' ' || c == '\t' {
			b = b[1:]
		} else {
			break
		}
	}
	for len(b) > 0 {
		c := b[len(b)-1]
		if c == 0x00 || c == 0x1C || c == 0x1D || c == 0x1E {
			b = b[:len(b)-1]
		} else {
			break
		}
	}
	return b
}

func parseOMATRCFMT(val, defaultSide string) (*rawOMABlock, error) {
	parts := strings.Split(strings.TrimSpace(val), ";")
	if len(parts) < 1 {
		return nil, fmt.Errorf("empty TRCFMT record")
	}
	fmtCode, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return nil, fmt.Errorf("invalid TRCFMT format %q: %w", parts[0], err)
	}
	blk := &rawOMABlock{
		format:  fmtCode,
		spacing: "E",
		side:    defaultSide,
	}
	if len(parts) >= 2 && strings.TrimSpace(parts[1]) != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(parts[1])); err == nil && n > 0 {
			blk.count = n
		}
	}
	if len(parts) >= 3 && strings.TrimSpace(parts[2]) != "" {
		blk.spacing = strings.ToUpper(strings.TrimSpace(parts[2]))
	}
	if len(parts) >= 4 && strings.TrimSpace(parts[3]) != "" {
		blk.side = strings.ToUpper(strings.TrimSpace(parts[3]))
	}
	return blk, nil
}

func parseOMAFirstFloat(val string) (float64, bool) {
	for _, tok := range strings.Split(strings.TrimSpace(val), ";") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		if f, err := strconv.ParseFloat(tok, 64); err == nil {
			return f, true
		}
	}
	return 0, false
}

func parseOMAPairFloats(val string) (r, l float64) {
	parts := strings.Split(strings.TrimSpace(val), ";")
	if len(parts) >= 1 {
		if f, err := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64); err == nil {
			r = f
		}
	}
	if len(parts) >= 2 {
		if f, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64); err == nil {
			l = f
		}
	}
	if l == 0 && r > 0 {
		l = r
	} else if r == 0 && l > 0 {
		r = l
	}
	return r, l
}

func parseOMAASCIINumbers(val string) ([]float64, error) {
	fields := strings.FieldsFunc(val, func(r rune) bool {
		return r == ';' || r == ',' || r == ' ' || r == '\t'
	})
	out := make([]float64, 0, len(fields))
	for _, f := range fields {
		if f == "" {
			continue
		}
		v, err := strconv.ParseFloat(f, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid number %q: %w", f, err)
		}
		out = append(out, v)
	}
	return out, nil
}

func unescapeOMABinary(in []byte) []byte {
	out := make([]byte, 0, len(in))
	for i := 0; i < len(in); i++ {
		if in[i] == 0x1B && i+1 < len(in) {
			i++
			out = append(out, in[i]&0x7F)
		} else {
			out = append(out, in[i])
		}
	}
	return out
}

func decodeOMABlock(blk *rawOMABlock) (Contour, float64, float64, error) {
	var rawRadii []float64
	var rawAngles []float64
	var err error

	switch blk.format {
	case 1:
		rawRadii = blk.asciiRadii
		rawAngles = blk.asciiAngles
	case 5:
		rawRadii = make([]float64, len(blk.asciiRadii))
		acc := 0.0
		for i, v := range blk.asciiRadii {
			if i == 0 {
				acc = v
			} else {
				acc += v
			}
			rawRadii[i] = acc
		}
		if len(blk.asciiAngles) > 0 {
			rawAngles = make([]float64, len(blk.asciiAngles))
			accA := 0.0
			for i, v := range blk.asciiAngles {
				if i == 0 {
					accA = v
				} else {
					accA += v
				}
				rawAngles[i] = accA
			}
		}
	case 2:
		rawRadii, err = decodeOMABinaryAbsolute(blk.binRadii, blk.count)
		if err != nil {
			return Contour{}, 0, 0, fmt.Errorf("decoding format 2 radii: %w", err)
		}
		if len(blk.binAngles) > 0 {
			rawAngles, err = decodeOMABinaryAbsolute(blk.binAngles, blk.count)
			if err != nil {
				return Contour{}, 0, 0, fmt.Errorf("decoding format 2 angles: %w", err)
			}
		}
	case 3:
		rawRadii, err = decodeOMABinaryDifferential(blk.binRadii, blk.count)
		if err != nil {
			return Contour{}, 0, 0, fmt.Errorf("decoding format 3 radii: %w", err)
		}
		if len(blk.binAngles) > 0 {
			rawAngles, err = decodeOMABinaryDifferential(blk.binAngles, blk.count)
			if err != nil {
				return Contour{}, 0, 0, fmt.Errorf("decoding format 3 angles: %w", err)
			}
		}
	case 4:
		rawRadii, err = decodeOMAPackedBinary(blk.binRadii, blk.count)
		if err != nil {
			return Contour{}, 0, 0, fmt.Errorf("decoding format 4 packed radii: %w", err)
		}
		if len(blk.binAngles) > 0 {
			rawAngles, err = decodeOMAPackedBinary(blk.binAngles, blk.count)
			if err != nil {
				return Contour{}, 0, 0, fmt.Errorf("decoding format 4 packed angles: %w", err)
			}
		}
	default:
		return Contour{}, 0, 0, fmt.Errorf("unsupported OMA TRCFMT format %d", blk.format)
	}

	if len(rawRadii) < 3 {
		return Contour{}, 0, 0, fmt.Errorf("trace has too few points (%d)", len(rawRadii))
	}

	scale := 0.01
	maxRaw := 0.0
	for _, r := range rawRadii {
		if math.Abs(r) > maxRaw {
			maxRaw = math.Abs(r)
		}
	}
	if maxRaw > 0 && maxRaw < 120 {
		scale = 1.0
	}

	n := len(rawRadii)
	anglesDeg := make([]float64, n)
	if (blk.spacing == "U" || blk.spacing == "A") && len(rawAngles) == n {
		angScale := 0.01
		maxAng := 0.0
		for _, a := range rawAngles {
			if math.Abs(a) > maxAng {
				maxAng = math.Abs(a)
			}
		}
		if maxAng > 0 && maxAng <= 360.0 {
			angScale = 1.0
		}
		for i := 0; i < n; i++ {
			anglesDeg[i] = rawAngles[i] * angScale
		}
	} else {
		step := 360.0 / float64(n)
		for i := 0; i < n; i++ {
			anglesDeg[i] = float64(i) * step
		}
	}

	pts := make([]Point2D, n)
	minX, maxX := math.Inf(1), math.Inf(-1)
	minY, maxY := math.Inf(1), math.Inf(-1)

	for i := 0; i < n; i++ {
		rMM := rawRadii[i] * scale
		rad := anglesDeg[i] * math.Pi / 180.0
		x := rMM * math.Cos(rad)
		y := rMM * math.Sin(rad)
		pts[i] = Point2D{X: x, Y: y}
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

	cx := (minX + maxX) / 2.0
	cy := (minY + maxY) / 2.0
	for i := range pts {
		pts[i].X -= cx
		pts[i].Y -= cy
	}

	return ResamplePolygon(pts), maxX - minX, maxY - minY, nil
}

func decodeOMABinaryAbsolute(data []byte, expected int) ([]float64, error) {
	n := len(data) / 2
	if expected > 0 && expected < n {
		n = expected
	}
	if n == 0 {
		return nil, fmt.Errorf("empty binary data")
	}
	out := make([]float64, n)
	for i := 0; i < n; i++ {
		out[i] = float64(binary.LittleEndian.Uint16(data[i*2 : i*2+2]))
	}
	return out, nil
}

func decodeOMABinaryDifferential(data []byte, expected int) ([]float64, error) {
	if len(data) < 2 {
		return nil, fmt.Errorf("binary differential data too short")
	}
	var out []float64
	cur := int32(binary.LittleEndian.Uint16(data[0:2]))
	out = append(out, float64(cur))
	pos := 2

	for pos < len(data) && (expected <= 0 || len(out) < expected) {
		b := data[pos]
		pos++
		if b == 0x80 {
			if pos+2 > len(data) {
				break
			}
			cur = int32(binary.LittleEndian.Uint16(data[pos : pos+2]))
			pos += 2
			out = append(out, float64(cur))
		} else {
			cur += int32(int8(b))
			out = append(out, float64(cur))
		}
	}
	return out, nil
}

func decodeOMAPackedBinary(data []byte, expected int) ([]float64, error) {
	const (
		modeAbsolute = iota
		modeDiff
		modeIncr
	)

	var out []float64
	mode := modeAbsolute
	var curVal int32
	var curDiff int32
	pos := 0

	for pos < len(data) && (expected <= 0 || len(out) < expected) {
		switch mode {
		case modeAbsolute:
			if pos+2 > len(data) {
				return out, nil
			}
			word := binary.LittleEndian.Uint16(data[pos : pos+2])
			pos += 2
			if word == 0x8000 {
				mode = modeDiff
				continue
			}
			curVal = int32(word)
			curDiff = 0
			out = append(out, float64(curVal))

		case modeDiff:
			b := data[pos]
			pos++
			switch b {
			case 0x80:
				mode = modeIncr
			case 0x81:
				mode = modeAbsolute
			default:
				curDiff = int32(int8(b))
				curVal += curDiff
				out = append(out, float64(curVal))
			}

		case modeIncr:
			b := data[pos]
			pos++
			nibbles := [2]byte{b & 0x0F, (b >> 4) & 0x0F}
			for _, nib := range nibbles {
				if expected > 0 && len(out) >= expected {
					break
				}
				if nib == 0x08 {
					mode = modeDiff
					break
				}
				var delta int32
				if nib&0x08 != 0 {
					delta = int32(nib) - 16
				} else {
					delta = int32(nib)
				}
				curDiff += delta
				curVal += curDiff
				out = append(out, float64(curVal))
			}
		}
	}
	return out, nil
}

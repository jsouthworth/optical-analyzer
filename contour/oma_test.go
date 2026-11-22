package contour

import (
	"bytes"
	"encoding/binary"
	"math"
	"strconv"
	"strings"
	"testing"
)

func TestParseOMAASCIIFormat1(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteString("ANS=TRC\r\n")
	buf.WriteString("JOB=TEST123\r\n")
	buf.WriteString("MODEL=PANTO\r\n")
	buf.WriteString("HBOX=50.00;50.00\r\n")
	buf.WriteString("VBOX=36.00;36.00\r\n")
	buf.WriteString("DBL=20.00\r\n")
	buf.WriteString("TRCFMT=1;72;E;R;F\r\n")

	for i := 0; i < 72; i++ {
		if i%8 == 0 {
			if i > 0 {
				buf.WriteString("\r\n")
			}
			buf.WriteString("R=")
		} else {
			buf.WriteString(";")
		}
		rad := float64(i) * (2 * math.Pi / 72.0)
		a, b := 25.0, 18.0
		r := (a * b) / math.Hypot(b*math.Cos(rad), a*math.Sin(rad))
		if math.Cos(rad) > 0 {
			r -= 1.0 * math.Cos(rad)
		}
		buf.WriteString(strconv.Itoa(int(math.Round(r * 100.0))))
	}
	buf.WriteString("\r\n")

	tr, err := ParseOMA(buf.Bytes())
	if err != nil {
		t.Fatalf("ParseOMA failed: %v", err)
	}
	if tr.Job != "TEST123" || tr.Model != "PANTO" || tr.FrameDBL != 20.0 || tr.FrameA != 50.0 || tr.FrameB != 36.0 {
		t.Fatalf("unexpected header metadata: %+v", tr)
	}
	if tr.ODMirrored || !tr.OSMirrored {
		t.Fatalf("unexpected mirror flags: OD=%v OS=%v", tr.ODMirrored, tr.OSMirrored)
	}
	w, h := tr.OD.BoxingSize()
	if math.Abs(w-49.0) > 0.2 || math.Abs(h-36.0) > 0.2 {
		t.Fatalf("unexpected OD box dims: %.2f x %.2f", w, h)
	}
	odX0, _ := tr.OD.PointAt(0)
	osX180, _ := tr.OS.PointAt(180)
	if math.Abs(odX0+osX180) > 1e-6 {
		t.Fatalf("OS mirror X mismatch: OD[0].X=%.4f OS[180].X=%.4f", odX0, osX180)
	}
	if sum := tr.Summary(); !strings.Contains(sum, "PANTO") || !strings.Contains(sum, "50.0×36.0 mm") || !strings.Contains(sum, "OD mirrored to OS") {
		t.Fatalf("unexpected Summary: %q", sum)
	}
}

func TestParseOMAASCIIFormat5Differential(t *testing.T) {
	raw := []byte(
		"TRCFMT=5;8;E;R;F\n" +
			"R=2000;0;-100;100;0;0;-100;100\n",
	)
	tr, err := ParseOMA(raw)
	if err != nil {
		t.Fatalf("ParseOMA(Format 5): %v", err)
	}
	w, h := tr.OD.BoxingSize()
	if math.Abs(w-40.0) > 0.2 || math.Abs(h-38.0) > 0.2 {
		t.Fatalf("Format 5 unexpected dims: %.2f x %.2f", w, h)
	}
}

func TestParseOMAUnequiangular(t *testing.T) {
	raw := []byte(
		"TRCFMT=1;4;U;R;F\n" +
			"R=2000;1500;2000;1500\n" +
			"A=0;9000;18000;27000\n",
	)
	tr, err := ParseOMA(raw)
	if err != nil {
		t.Fatalf("ParseOMA(U): %v", err)
	}
	w, h := tr.OD.BoxingSize()
	if math.Abs(w-40.0) > 1e-6 || math.Abs(h-30.0) > 1e-6 {
		t.Fatalf("unexpected diamond dims: %.2f x %.2f", w, h)
	}
}

func TestParseOMABinaryFormats(t *testing.T) {
	radii := []uint16{2000, 2010, 2005, 1990}
	var payload2 bytes.Buffer
	for _, r := range radii {
		var b [2]byte
		binary.LittleEndian.PutUint16(b[:], r)
		for _, ch := range b {
			if ch < 0x20 {
				payload2.WriteByte(0x1B)
				payload2.WriteByte(ch | 0x80)
			} else {
				payload2.WriteByte(ch)
			}
		}
	}
	raw2 := append([]byte("TRCFMT=2;4;E;R;F\nR="), payload2.Bytes()...)
	raw2 = append(raw2, '\n')

	tr2, err := ParseOMA(raw2)
	if err != nil {
		t.Fatalf("Format 2 parse failed: %v", err)
	}
	if math.Abs(tr2.FrameA-40.05) > 1e-6 || math.Abs(tr2.FrameB-40.0) > 1e-6 {
		t.Fatalf("Format 2 unexpected frame dims: %.2f x %.2f", tr2.FrameA, tr2.FrameB)
	}
	w2, h2 := tr2.OD.BoxingSize()
	if math.Abs(w2-40.05) > 0.25 || math.Abs(h2-40.0) > 0.25 {
		t.Fatalf("Format 2 unexpected contour dims: %.2f x %.2f", w2, h2)
	}

	var payload3 bytes.Buffer
	writeEscaped := func(b []byte) {
		for _, ch := range b {
			if ch < 0x20 {
				payload3.WriteByte(0x1B)
				payload3.WriteByte(ch | 0x80)
			} else {
				payload3.WriteByte(ch)
			}
		}
	}
	var start [2]byte
	binary.LittleEndian.PutUint16(start[:], 2000)
	writeEscaped(start[:])
	d1, d2 := int8(-5), int8(-15)
	writeEscaped([]byte{10, byte(d1), byte(d2)})
	raw3 := append([]byte("TRCFMT=3;4;E;R;F\nR="), payload3.Bytes()...)
	raw3 = append(raw3, '\n')

	tr3, err := ParseOMA(raw3)
	if err != nil {
		t.Fatalf("Format 3 parse failed: %v", err)
	}
	w3, _ := tr3.OD.BoxingSize()
	if math.Abs(w3-w2) > 1e-6 {
		t.Fatalf("Format 3 mismatch vs Format 2: %.3f vs %.3f", w3, w2)
	}

	var payload4 bytes.Buffer
	writeEsc4 := func(b []byte) {
		for _, ch := range b {
			if ch < 0x20 {
				payload4.WriteByte(0x1B)
				payload4.WriteByte(ch | 0x80)
			} else {
				payload4.WriteByte(ch)
			}
		}
	}
	writeEsc4(start[:])
	writeEsc4([]byte{0x00, 0x80})
	writeEsc4([]byte{10})
	writeEsc4([]byte{0x80})
	writeEsc4([]byte{0x00 | (0x0E << 4)})
	raw4 := append([]byte("TRCFMT=4;4;E;R;F\nR="), payload4.Bytes()...)
	raw4 = append(raw4, '\n')

	tr4, err := ParseOMA(raw4)
	if err != nil {
		t.Fatalf("Format 4 parse failed: %v", err)
	}
	if tr4.OD.AreaMM2() <= 0 {
		t.Fatalf("Format 4 produced empty contour")
	}

	// Verify Read(FormatOMA) delegates to the full binary parser too
	trViaRead, err := Read(bytes.NewReader(raw2), FormatOMA, 0, 0, 0)
	if err != nil {
		t.Fatalf("Read(FormatOMA) binary failed: %v", err)
	}
	wR, _ := trViaRead.OD.BoxingSize()
	if math.Abs(wR-w2) > 1e-6 {
		t.Fatalf("Read(FormatOMA) width %.3f != ParseOMA %.3f", wR, w2)
	}
}

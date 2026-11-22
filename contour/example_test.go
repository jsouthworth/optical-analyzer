package contour_test

import (
	"fmt"

	"jsouthworth.net/go/optical-analyzer/contour"
)

func ExampleSuperellipse() {
	c := contour.Superellipse(52, 40, contour.ShapeOval)
	w, h := c.BoxingSize()
	fmt.Printf("Boxing: %.1f x %.1f mm  Area: %.1f mm^2\n", w, h, c.AreaMM2())
	// Output:
	// Boxing: 52.0 x 40.0 mm  Area: 1633.6 mm^2
}

func ExampleParseOMA() {
	oma := "HBOX=50.00\nVBOX=40.00\nDBL=18.00\nTRCFMT=1;8;E;R;F\nR=2500;2200;2000;2200;2500;2200;2000;2200\n"
	tr, err := contour.ParseOMA([]byte(oma))
	if err != nil {
		panic(err)
	}
	a, b, dbl := tr.EffectiveDimensions(0, 0, 0)
	fmt.Printf("A=%.1f B=%.1f DBL=%.1f OS_mirrored=%v\n", a, b, dbl, tr.OSMirrored)
	// Output:
	// A=50.0 B=40.0 DBL=18.0 OS_mirrored=true
}

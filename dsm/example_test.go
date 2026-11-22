package dsm_test

import (
	"fmt"

	"jsouthworth.net/go/optical-analyzer/dsm"
	"jsouthworth.net/go/optical-analyzer/optics"
)

func ExampleMakeDSM() {
	data := optics.NormalizeData(optics.Data{
		RX: optics.RX{
			OD: optics.Refraction{Sph: -4.00, Cyl: -1.00, Axis: 90},
			OS: optics.Refraction{Sph: -4.00, Cyl: -1.00, Axis: 90},
		},
		Lenses: optics.LensPair{Material: "1.60-plastic"},
	}, optics.NormalizeOptions{})

	od, _ := dsm.MakeDSM(data)
	fmt.Printf("Major mag: %.4f  Prism @ 10mm (0 deg): %.2f Δ\n",
		od.Magnification(od.FvMajor), od.Prism(10, 0))
	// Output:
	// Major mag: 0.9043  Prism @ 10mm (0 deg): 4.34 Δ
}

func ExampleReadingPrismaticDifference() {
	data := optics.NormalizeData(optics.Data{
		RX: optics.RX{
			OD: optics.Refraction{Sph: -4.00},
			OS: optics.Refraction{Sph: -2.00},
		},
		Lenses: optics.LensPair{Material: "1.60-plastic"},
		Fit: optics.Fit{
			OD: optics.EyeFit{PD: 31.0},
			OS: optics.EyeFit{PD: 31.0},
		},
	}, optics.NormalizeOptions{})

	rp := dsm.ReadingPrismaticDifference(data, 23, optics.DefaultReadingDistanceMM)
	fmt.Printf("23 deg drop: %.1f mm  vertical diff: %+.2f Δ\n", rp.DropODMM, rp.VerticalDiffPD)
	// Output:
	// 23 deg drop: 11.5 mm  vertical diff: -1.99 Δ
}

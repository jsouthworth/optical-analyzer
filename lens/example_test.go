package lens_test

import (
	"fmt"

	"jsouthworth.net/go/optical-analyzer/contour"
	"jsouthworth.net/go/optical-analyzer/lens"
	"jsouthworth.net/go/optical-analyzer/optics"
)

func ExampleCalcEyeProfile() {
	prof := lens.CalcEyeProfile(optics.EyeGeometry{
		IsOD:         true,
		Sphere:       -5.00,
		FrameA:       52,
		FrameB:       40,
		FrameDBL:     18,
		MonoPD:       31,
		Index:        1.67,
		Family:       optics.FamilyPlastic,
		ChamferMM:    optics.DefaultChamferMM,
		BevelDepthMM: optics.DefaultBevelDepthMM,
		ShapeExp:     contour.ShapeSquircle,
		Design:       optics.DesignBiAspheric,
	})
	fmt.Printf("BC=+%.2f D CT=%.2f mm MaxEdge=%.2f mm Weight=%.1f g\n",
		prof.BaseCurve, prof.CenterThickness, prof.MaxCaliperThickness, prof.Weight)
	// Output:
	// BC=+2.00 D CT=1.50 mm MaxEdge=4.87 mm Weight=6.6 g
}

func ExampleRecommendMaterials() {
	shapes, _ := lens.ResolveShapes("squircle")
	inputs := lens.ReportInputs{
		OD:         lens.EyePrescription{Sphere: -5.00, PD: 31},
		OS:         lens.EyePrescription{Sphere: -4.50, PD: 31},
		Chamfer:    optics.DefaultChamferMM,
		BevelDepth: optics.DefaultBevelDepthMM,
		Frame:      lens.FrameSpec{A: 52, B: 40, DBL: 18},
	}
	report := lens.RecommendMaterials(inputs, shapes[0], optics.DesignBiAspheric, lens.RecommendOptions{
		Region: optics.RegionUS,
	})
	fmt.Printf("Top material: %s (%s)\n", report.Recommendations[0].Key, report.Recommendations[0].Family)
	// Output:
	// Top material: 1.60-plastic (plastic)
}

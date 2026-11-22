package optics_test

import (
	"fmt"
	"strings"

	"jsouthworth.net/go/optical-analyzer/optics"
)

func ExampleDecodeData() {
	const raw = `{
		"version": 1,
		"rx": {
			"od": { "sph": -4.00, "cyl": -1.00, "axis": 90 },
			"os": { "sph": -3.50 }
		},
		"lenses": {
			"material": "1.67-plastic",
			"design": "aspheric"
		},
		"frame": { "a": 52, "b": 40, "dbl": 18 },
		"fit": { "od": { "pd": 31 }, "os": { "pd": 30.5 } }
	}`

	data, err := optics.DecodeData(strings.NewReader(raw))
	if err != nil {
		panic(err)
	}
	norm := optics.NormalizeData(data, optics.NormalizeOptions{})
	fmt.Printf("OD: n=%.2f V=%d BC=+%.2f D CT=%.1f mm\n",
		norm.Lenses.OD.Index, norm.Lenses.OD.Abbe, norm.Lenses.OD.BaseCurve, norm.Lenses.OD.CenterThickness)
	// Output:
	// OD: n=1.67 V=32 BC=+2.75 D CT=1.5 mm
}

func ExampleLookupMaterialByKey() {
	spec, ok := optics.LookupMaterialByKey("trivex")
	fmt.Printf("found=%v key=%s n=%.2f family=%s abbe=%d\n",
		ok, spec.Key, spec.Index, spec.Family, spec.Abbe)
	// Output:
	// found=true key=1.53-plastic n=1.53 family=plastic abbe=45
}

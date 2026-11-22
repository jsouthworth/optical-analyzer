package optics

import (
	"fmt"
	"math"
	"strings"
)

// MaterialFamily distinguishes lens substrate families that share refractive
// indices (e.g., 1.60/1.70 plastic vs. 1.60/1.70 mineral glass).
type MaterialFamily string

const (
	// FamilyAuto infers plastic or mineral glass from the refractive index.
	FamilyAuto MaterialFamily = ""
	// FamilyPlastic selects organic resin / polymer lens substrates.
	FamilyPlastic MaterialFamily = "plastic"
	// FamilyGlass selects mineral glass lens substrates.
	FamilyGlass MaterialFamily = "glass"
)

// ResolveMaterialFamily parses a substrate family string into a MaterialFamily.
func ResolveMaterialFamily(input string) (MaterialFamily, error) {
	switch strings.ToLower(strings.TrimSpace(input)) {
	case "", "auto":
		return FamilyAuto, nil
	case "plastic", "resin", "organic", "poly", "cr39", "cr-39":
		return FamilyPlastic, nil
	case "glass", "mineral":
		return FamilyGlass, nil
	default:
		return FamilyAuto, fmt.Errorf("invalid material family %q: use auto, plastic, or glass", input)
	}
}

// MarketRegion selects regional optical lab availability profiles.
type MarketRegion string

const (
	// RegionUS selects North American optical lab material availability profiles.
	RegionUS MarketRegion = "us"
	// RegionEU selects European / international optical lab material availability profiles.
	RegionEU MarketRegion = "eu"
)

// ResolveMarketRegion parses a region flag string into a MarketRegion.
func ResolveMarketRegion(input string) (MarketRegion, error) {
	switch strings.ToLower(strings.TrimSpace(input)) {
	case "", "us", "usa", "na", "north-america":
		return RegionUS, nil
	case "eu", "europe", "global", "intl", "international":
		return RegionEU, nil
	default:
		return RegionUS, fmt.Errorf("invalid -region %q: use us or eu", input)
	}
}

// MaterialSpec describes a standard ophthalmic lens material in the catalog.
type MaterialSpec struct {
	Key                string         // Canonical preset identifier (e.g., "1.60-plastic")
	Name               string         // Display name (e.g., "High-Index 1.60 Plastic (MR-8)")
	Index              float64        // Nominal refractive index
	Family             MaterialFamily // Substrate family (FamilyPlastic or FamilyGlass)
	SpecificGravity    float64        // Typical density in g/cm³
	Abbe               int            // Typical Abbe number
	MinCenterThickness float64        // Typical lab minimum center thickness for minus lenses (mm)
	MinEdgeThickness   float64        // Typical minimum structural edge thickness for plus lenses (mm)
	RelativeCost       float64        // Relative single-vision optician pair cost multiplier (CR-39 = 1.0)
	AvailabilityUS     float64        // 0..100 lab/retail availability score in the US market
	AvailabilityEU     float64        // 0..100 lab/retail availability score in EU / global markets
}

// standardMaterials is the single canonical catalog of the 12 standard
// ophthalmic plastic and mineral glass materials used across the library and GUI.
var standardMaterials = []MaterialSpec{
	{
		Key: "1.50-plastic", Name: "CR-39 Plastic (1.50)", Index: 1.50, Family: FamilyPlastic,
		SpecificGravity: 1.32, Abbe: 58, MinCenterThickness: 2.0, MinEdgeThickness: 1.5,
		RelativeCost: 1.0, AvailabilityUS: 100, AvailabilityEU: 100,
	},
	{
		Key: "1.523-glass", Name: "Crown Glass (1.523)", Index: 1.523, Family: FamilyGlass,
		SpecificGravity: 2.54, Abbe: 59, MinCenterThickness: 2.2, MinEdgeThickness: 1.8,
		RelativeCost: 1.4, AvailabilityUS: 50, AvailabilityEU: 70,
	},
	{
		Key: "1.53-plastic", Name: "Trivex (1.53)", Index: 1.53, Family: FamilyPlastic,
		SpecificGravity: 1.11, Abbe: 45, MinCenterThickness: 1.3, MinEdgeThickness: 1.2,
		RelativeCost: 2.3, AvailabilityUS: 85, AvailabilityEU: 75,
	},
	{
		Key: "1.59-plastic", Name: "Polycarbonate (1.59)", Index: 1.59, Family: FamilyPlastic,
		SpecificGravity: 1.20, Abbe: 30, MinCenterThickness: 1.3, MinEdgeThickness: 1.2,
		RelativeCost: 1.5, AvailabilityUS: 100, AvailabilityEU: 85,
	},
	{
		Key: "1.60-plastic", Name: "High-Index 1.60 Plastic (MR-8)", Index: 1.60, Family: FamilyPlastic,
		SpecificGravity: 1.30, Abbe: 42, MinCenterThickness: 1.4, MinEdgeThickness: 1.3,
		RelativeCost: 2.6, AvailabilityUS: 90, AvailabilityEU: 100,
	},
	{
		Key: "1.60-glass", Name: "High-Index 1.60 Glass", Index: 1.60, Family: FamilyGlass,
		SpecificGravity: 2.65, Abbe: 42, MinCenterThickness: 2.0, MinEdgeThickness: 1.6,
		RelativeCost: 2.1, AvailabilityUS: 40, AvailabilityEU: 65,
	},
	{
		Key: "1.67-plastic", Name: "High-Index 1.67 Plastic (MR-7/10)", Index: 1.67, Family: FamilyPlastic,
		SpecificGravity: 1.35, Abbe: 32, MinCenterThickness: 1.5, MinEdgeThickness: 1.3,
		RelativeCost: 3.4, AvailabilityUS: 100, AvailabilityEU: 100,
	},
	{
		Key: "1.70-plastic", Name: "High-Index 1.70 Plastic (EYRY)", Index: 1.70, Family: FamilyPlastic,
		SpecificGravity: 1.41, Abbe: 36, MinCenterThickness: 1.0, MinEdgeThickness: 1.2,
		RelativeCost: 4.4, AvailabilityUS: 10, AvailabilityEU: 55,
	},
	{
		Key: "1.70-glass", Name: "High-Index 1.70 Glass", Index: 1.70, Family: FamilyGlass,
		SpecificGravity: 3.20, Abbe: 39, MinCenterThickness: 1.8, MinEdgeThickness: 1.5,
		RelativeCost: 3.2, AvailabilityUS: 35, AvailabilityEU: 60,
	},
	{
		Key: "1.74-plastic", Name: "High-Index 1.74 Plastic (MR-174)", Index: 1.74, Family: FamilyPlastic,
		SpecificGravity: 1.47, Abbe: 33, MinCenterThickness: 1.6, MinEdgeThickness: 1.4,
		RelativeCost: 5.0, AvailabilityUS: 85, AvailabilityEU: 90,
	},
	{
		Key: "1.80-glass", Name: "High-Index 1.80 Glass", Index: 1.80, Family: FamilyGlass,
		SpecificGravity: 3.60, Abbe: 35, MinCenterThickness: 1.6, MinEdgeThickness: 1.5,
		RelativeCost: 5.3, AvailabilityUS: 15, AvailabilityEU: 45,
	},
	{
		Key: "1.90-glass", Name: "High-Index 1.90 Glass (Lantal)", Index: 1.90, Family: FamilyGlass,
		SpecificGravity: 4.00, Abbe: 31, MinCenterThickness: 1.5, MinEdgeThickness: 1.5,
		RelativeCost: 7.0, AvailabilityUS: 10, AvailabilityEU: 35,
	},
}

// StandardMaterials returns a copy of the built-in catalog of 12 plastic and
// mineral glass ophthalmic lens materials.
func StandardMaterials() []MaterialSpec {
	out := make([]MaterialSpec, len(standardMaterials))
	copy(out, standardMaterials)
	return out
}

// IsGlassMaterial reports whether the given refractive index and substrate
// family hint represent a mineral glass lens. When family is FamilyAuto, crown
// glass (1.523) and ultra-high-index glass (>= 1.78, i.e. 1.80 and 1.90)
// resolve to glass automatically.
func IsGlassMaterial(index float64, family MaterialFamily) bool {
	switch family {
	case FamilyGlass:
		return true
	case FamilyPlastic:
		return false
	default:
		return math.Abs(index-1.523) < 0.006 || index >= 1.78
	}
}

// LookupMaterial finds a catalog material whose nominal refractive index is
// within 0.002 of index, preferring the substrate family indicated by family.
func LookupMaterial(index float64, family MaterialFamily) (MaterialSpec, bool) {
	wantGlass := IsGlassMaterial(index, family)
	var fallback *MaterialSpec
	for i := range standardMaterials {
		m := &standardMaterials[i]
		if math.Abs(index-m.Index) < 0.002 {
			if (m.Family == FamilyGlass) == wantGlass {
				return *m, true
			}
			if fallback == nil {
				fallback = m
			}
		}
	}
	if fallback != nil {
		return *fallback, true
	}
	return MaterialSpec{}, false
}

// LookupMaterialByKey finds a catalog material by its canonical preset key
// (e.g., "1.60-plastic", "1.90-glass") or common alias (e.g., "cr39", "trivex",
// "polycarbonate", "crown", "mr-8", "eyry", "lantal").
func LookupMaterialByKey(key string) (MaterialSpec, bool) {
	norm := strings.ToLower(strings.TrimSpace(key))
	if norm == "" {
		return MaterialSpec{}, false
	}
	for _, m := range standardMaterials {
		if m.Key == norm {
			return m, true
		}
	}
	aliasToKey := map[string]string{
		"cr39":          "1.50-plastic",
		"cr-39":         "1.50-plastic",
		"1.50":          "1.50-plastic",
		"crown":         "1.523-glass",
		"crown-glass":   "1.523-glass",
		"1.523":         "1.523-glass",
		"trivex":        "1.53-plastic",
		"1.53":          "1.53-plastic",
		"poly":          "1.59-plastic",
		"polycarbonate": "1.59-plastic",
		"1.59":          "1.59-plastic",
		"mr8":           "1.60-plastic",
		"mr-8":          "1.60-plastic",
		"mr7":           "1.67-plastic",
		"mr-7":          "1.67-plastic",
		"mr10":          "1.67-plastic",
		"mr-10":         "1.67-plastic",
		"1.67":          "1.67-plastic",
		"eyry":          "1.70-plastic",
		"mr174":         "1.74-plastic",
		"mr-174":        "1.74-plastic",
		"1.74":          "1.74-plastic",
		"1.80":          "1.80-glass",
		"lantal":        "1.90-glass",
		"1.90":          "1.90-glass",
	}
	if canonical, ok := aliasToKey[norm]; ok {
		for _, m := range standardMaterials {
			if m.Key == canonical {
				return m, true
			}
		}
	}
	return MaterialSpec{}, false
}

// resolveMaterialSpec maps any refractive index and substrate family to the
// nearest standard catalog entry for property estimation (falling back to CR-39
// for unknown plastic indices and Crown Glass for low glass indices).
func resolveMaterialSpec(index float64, family MaterialFamily) MaterialSpec {
	if IsGlassMaterial(index, family) {
		switch {
		case index >= 1.85:
			return standardMaterials[11] // 1.90 Glass
		case index >= 1.75:
			return standardMaterials[10] // 1.80 Glass
		case index >= 1.65:
			return standardMaterials[8] // 1.70 Glass
		case index >= 1.56:
			return standardMaterials[5] // 1.60 Glass
		default:
			return standardMaterials[1] // Crown Glass 1.523
		}
	}
	for _, m := range standardMaterials {
		if m.Family == FamilyPlastic && math.Abs(index-m.Index) < 0.006 {
			return m
		}
	}
	return standardMaterials[0] // CR-39 Plastic 1.50
}

// EstimateSpecificGravity returns typical material density (g/cm³) for the
// given refractive index and substrate family.
func EstimateSpecificGravity(index float64, family MaterialFamily) float64 {
	return resolveMaterialSpec(index, family).SpecificGravity
}

// EstimateAbbe returns the typical Abbe number for the given refractive index
// and substrate family.
func EstimateAbbe(index float64, family MaterialFamily) int {
	return resolveMaterialSpec(index, family).Abbe
}

// MinCenterThickness returns typical lab minimum center thickness (mm) for minus
// dress lenses of the given refractive index and substrate family.
func MinCenterThickness(index float64, family MaterialFamily) float64 {
	return resolveMaterialSpec(index, family).MinCenterThickness
}

// MinEdgeThickness returns typical minimum structural edge thickness (mm)
// required for plus dress lenses of the given refractive index and substrate
// family.
func MinEdgeThickness(index float64, family MaterialFamily) float64 {
	return resolveMaterialSpec(index, family).MinEdgeThickness
}

// EstimateRelativeCost returns the typical relative single-vision optician pair
// cost multiplier (CR-39 = 1.0×) for the given index and substrate family.
func EstimateRelativeCost(index float64, family MaterialFamily) float64 {
	return resolveMaterialSpec(index, family).RelativeCost
}

// EstimateAvailability returns a 0..100 availability score reflecting how
// commonly a material is stocked by optical labs in the specified market region.
func EstimateAvailability(index float64, family MaterialFamily, region MarketRegion) float64 {
	spec := resolveMaterialSpec(index, family)
	if region == RegionEU {
		return spec.AvailabilityEU
	}
	return spec.AvailabilityUS
}

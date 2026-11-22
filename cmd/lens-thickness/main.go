package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"jsouthworth.net/go/optical-analyzer/contour"
	"jsouthworth.net/go/optical-analyzer/internal/version"
	"jsouthworth.net/go/optical-analyzer/lens"
	"jsouthworth.net/go/optical-analyzer/optics"
)

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("lens-thickness", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		odSph, odCyl, odAxis, odPD, odOCH float64
		osSph, osCyl, osAxis, osPD, osOCH float64
		rIndex, specGrav, baseCurve       float64
		centerThickness, chamferMM        float64
		bevelDepthMM                      float64
		frameA, frameB, frameDBL          float64
		materialFlag                      string
		shapeFlag, designFlag, formatFlag string
		traceFlag, traceFormatFlag        string
		dataFlag                          string
		recommendFlag                     bool
		regionFlag                        string
		priorityFlag                      string
		wClarity, wThickness, wWeight     float64
		wCost, wAvail                     float64
		showVersion                       bool
	)

	fs.Float64Var(&odSph, "odsph", 0, "OD Sph")
	fs.Float64Var(&odCyl, "odcyl", 0, "OD Cyl")
	fs.Float64Var(&odAxis, "odaxis", 0, "OD Axis")
	fs.Float64Var(&odPD, "odpd", 0, "OD PD (0 = auto-calculate as (A+DBL)/2)")
	fs.Float64Var(&odOCH, "odoch", 0, "OD Optical Center height from bottom of frame in mm (0 = auto-calculate as B/2)")

	fs.Float64Var(&osSph, "ossph", 0, "OS Sph")
	fs.Float64Var(&osCyl, "oscyl", 0, "OS Cyl")
	fs.Float64Var(&osAxis, "osaxis", 0, "OS Axis")
	fs.Float64Var(&osPD, "ospd", 0, "OS PD (0 = auto-calculate as (A+DBL)/2)")
	fs.Float64Var(&osOCH, "osoch", 0, "OS Optical Center height from bottom of frame in mm (0 = auto-calculate as B/2)")

	fs.Float64Var(&rIndex, "index", optics.DefaultRefractiveIndex, "Refractive Index")
	fs.StringVar(&materialFlag, "material", "", "Material family: auto (default), plastic, or glass (disambiguates 1.60/1.70 plastic vs glass; 1.523, 1.80, and 1.90 default to glass)")
	fs.Float64Var(&specGrav, "sg", 0, "Specific Gravity in g/cm^3 (0 = auto-estimate from index and material)")
	fs.Float64Var(&baseCurve, "bc", 0, "Base curve (0 = auto-estimate)")
	fs.Float64Var(&centerThickness, "thickness", 0, "Center Thickness in mm (0 = auto-estimate from Rx, material, and frame)")
	fs.Float64Var(&chamferMM, "chamfer", optics.DefaultChamferMM, "Edge pin-bevel/safety chamfer thickness reduction in mm (default 0.25)")
	fs.Float64Var(&bevelDepthMM, "bevel-depth", optics.DefaultBevelDepthMM, "V-bevel / eyewire groove depth per side in mm inset from groove-to-groove contour to the bevel shoulder (0 = flat edge for rimless/semi-rimless)")

	fs.Float64Var(&frameA, "a", 0, "Frame A measurement")
	fs.Float64Var(&frameB, "b", 0, "Frame B measurement")
	fs.Float64Var(&frameDBL, "dbl", 0, "Frame DBL measurement")

	fs.StringVar(&shapeFlag, "shape", "", "Frame shape filter: oval, softrect, squircle, box, or custom numeric exponent (empty = all)")
	fs.StringVar(&designFlag, "design", "", "Lens design filter: spherical, aspheric, biaspheric (empty = all)")
	fs.StringVar(&formatFlag, "format", "text", "Output format: text, json, or csv")
	fs.StringVar(&traceFlag, "trace", "", "Path to a lens trace file (.trc/.oma, .json, or .svg), or '-' to read from stdin")
	fs.StringVar(&traceFormatFlag, "trace-format", "", "Trace input format: oma (default for stdin), json, or svg (empty = infer from extension or default to oma)")
	fs.StringVar(&dataFlag, "data", "", "Path to an optical JSON data file (or '-' for stdin; CLI flags override file values)")
	fs.BoolVar(&recommendFlag, "recommend", false, "Recommend and rank lens materials by ±30° clarity, thickness, weight, cost, and availability using DSM")
	fs.StringVar(&regionFlag, "region", "us", "Market region for material availability scoring: us or eu (global)")
	fs.StringVar(&priorityFlag, "priority", "", "Comma-separated priority ranking (highest to lowest), e.g. clarity,thickness,weight,cost,availability")
	fs.Float64Var(&wClarity, "w-clarity", 40, "Relative recommendation weight for ±30° optical clarity")
	fs.Float64Var(&wThickness, "w-thickness", 25, "Relative recommendation weight for maximum lens thickness")
	fs.Float64Var(&wWeight, "w-weight", 15, "Relative recommendation weight for lens pair weight")
	fs.Float64Var(&wCost, "w-cost", 10, "Relative recommendation weight for relative material cost")
	fs.Float64Var(&wAvail, "w-avail", 10, "Relative recommendation weight for market availability")
	fs.BoolVar(&showVersion, "version", false, "Print version and exit")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if showVersion {
		fmt.Fprintf(stdout, "lens-thickness %s\n", version.String())
		return nil
	}

	setFlags := make(map[string]bool)
	fs.Visit(func(f *flag.Flag) {
		setFlags[f.Name] = true
	})

	bevelDepthExplicit := setFlags["bevel-depth"]
	var (
		hasConfig          bool
		cfgData            optics.Data
		odCorMM, osCorMM   float64
		odEyeIdx, osEyeIdx float64
		odEyeMat, osEyeMat optics.MaterialFamily
		odEyeSG, osEyeSG   float64
		odEyeBC, osEyeBC   float64
		odEyeCT, osEyeCT   float64
	)

	if dataFlag != "" {
		if dataFlag == "-" && traceFlag == "-" {
			return fmt.Errorf("-data - and -trace - cannot both read from stdin")
		}
		var err error
		cfgData, err = optics.LoadDataFromPath(dataFlag, stdin)
		if err != nil {
			return err
		}
		hasConfig = true

		if !setFlags["odsph"] {
			odSph = cfgData.RX.OD.Sph
		}
		if !setFlags["odcyl"] {
			odCyl = cfgData.RX.OD.Cyl
		}
		if !setFlags["odaxis"] {
			odAxis = cfgData.RX.OD.Axis
		}
		if !setFlags["odpd"] {
			odPD = cfgData.Fit.OD.PD
		}
		if !setFlags["odoch"] {
			odOCH = cfgData.Fit.OD.OCH
		}
		odCorMM = cfgData.Fit.EffectiveODCenterOfRotation()

		if !setFlags["ossph"] {
			osSph = cfgData.RX.OS.Sph
		}
		if !setFlags["oscyl"] {
			osCyl = cfgData.RX.OS.Cyl
		}
		if !setFlags["osaxis"] {
			osAxis = cfgData.RX.OS.Axis
		}
		if !setFlags["ospd"] {
			osPD = cfgData.Fit.OS.PD
		}
		if !setFlags["osoch"] {
			osOCH = cfgData.Fit.OS.OCH
		}
		osCorMM = cfgData.Fit.EffectiveOSCenterOfRotation()

		odEff := cfgData.Lenses.EffectiveEye(true)
		osEff := cfgData.Lenses.EffectiveEye(false)

		if !setFlags["index"] && odEff.Index > 0 {
			rIndex = odEff.Index
		}
		if !setFlags["material"] && odEff.Family != "" {
			materialFlag = string(odEff.Family)
		}
		if !setFlags["sg"] && odEff.SpecificGravity > 0 {
			specGrav = odEff.SpecificGravity
		}
		if !setFlags["bc"] && odEff.BaseCurve > 0 {
			baseCurve = odEff.BaseCurve
		}
		if !setFlags["thickness"] && odEff.CenterThickness > 0 {
			centerThickness = odEff.CenterThickness
		}
		if cfgData.Lenses.Chamfer != nil && !setFlags["chamfer"] {
			chamferMM = *cfgData.Lenses.Chamfer
		}
		if cfgData.Lenses.BevelDepth != nil && !setFlags["bevel-depth"] {
			bevelDepthMM = *cfgData.Lenses.BevelDepth
			bevelDepthExplicit = true
		}

		// Preserve per-eye overrides from config when not overridden by CLI flags.
		if !setFlags["index"] {
			odEyeIdx = odEff.Index
			osEyeIdx = osEff.Index
		}
		if !setFlags["material"] {
			odEyeMat = odEff.Family
			osEyeMat = osEff.Family
		}
		if !setFlags["sg"] {
			odEyeSG = odEff.SpecificGravity
			osEyeSG = osEff.SpecificGravity
		}
		if !setFlags["bc"] {
			odEyeBC = odEff.BaseCurve
			osEyeBC = osEff.BaseCurve
		}
		if !setFlags["thickness"] {
			odEyeCT = odEff.CenterThickness
			osEyeCT = osEff.CenterThickness
		}

		if !setFlags["a"] && cfgData.Frame.A > 0 {
			frameA = cfgData.Frame.A
		}
		if !setFlags["b"] && cfgData.Frame.B > 0 {
			frameB = cfgData.Frame.B
		}
		if !setFlags["dbl"] && cfgData.Frame.DBL > 0 {
			frameDBL = cfgData.Frame.DBL
		}
		if !setFlags["shape"] && cfgData.Frame.Shape != "" {
			shapeFlag = cfgData.Frame.Shape
		}
		if !setFlags["design"] && cfgData.Lenses.Design != optics.DesignBiAspheric {
			designFlag = cfgData.Lenses.Design.Key()
		}
		if !setFlags["trace"] && cfgData.Frame.Trace != "" {
			traceFlag = optics.ResolveTracePath(dataFlag, cfgData.Frame.Trace)
		}
		if !setFlags["trace-format"] && cfgData.Frame.TraceFormat != "" {
			traceFormatFlag = cfgData.Frame.TraceFormat
		}

		if dataFlag == "-" && traceFlag == "-" {
			return fmt.Errorf("-data - and -trace - cannot both read from stdin")
		}
	}

	material, err := optics.ResolveMaterialFamily(materialFlag)
	if err != nil {
		return err
	}

	designs, err := lens.ResolveDesigns(designFlag)
	if err != nil {
		return err
	}

	format, err := lens.ResolveFormat(formatFlag)
	if err != nil {
		return err
	}

	if chamferMM < 0 {
		return fmt.Errorf("-chamfer must be >= 0")
	}
	if bevelDepthMM < 0 {
		return fmt.Errorf("-bevel-depth must be >= 0")
	}

	var shapes []lens.ShapeOption
	if traceFlag != "" {
		loaded, err := contour.ReadFromPath(traceFlag, traceFormatFlag, stdin, frameA, frameB, frameDBL)
		if err != nil {
			return err
		}
		if frameA <= 0 && loaded.FrameA > 0 {
			frameA = loaded.FrameA
		}
		if frameB <= 0 && loaded.FrameB > 0 {
			frameB = loaded.FrameB
		}
		if frameDBL <= 0 && loaded.FrameDBL > 0 {
			frameDBL = loaded.FrameDBL
		}
		if !bevelDepthExplicit && loaded.GrooveDepthMM > 0 {
			bevelDepthMM = loaded.GrooveDepthMM
		}
		name := loaded.Name
		if name == "" {
			if traceFlag == "-" {
				name = "Trace (stdin)"
			} else {
				name = fmt.Sprintf("Trace (%s)", filepath.Base(traceFlag))
			}
		}
		shapes = []lens.ShapeOption{
			{
				Name:        name,
				ODContour:   &loaded.OD,
				OSContour:   &loaded.OS,
				Source:      loaded.Source,
				GrooveDepth: loaded.GrooveDepthMM,
			},
		}
	} else {
		shapes, err = lens.ResolveShapes(shapeFlag)
		if err != nil {
			return err
		}
		if frameA <= 0 && frameB <= 0 && frameDBL <= 0 {
			frameA = optics.DefaultFrameA
			frameB = optics.DefaultFrameB
			frameDBL = optics.DefaultFrameDBL
		} else {
			if frameA <= 0 {
				frameA = optics.DefaultFrameA
			}
			if frameB <= 0 {
				frameB = optics.DefaultFrameB
			}
		}
	}

	if bevelDepthMM > 0 {
		if frameA > 0 && 2.0*bevelDepthMM >= frameA {
			return fmt.Errorf("invalid -bevel-depth (%.2f mm/side): 2*bevel-depth must be less than frame A (%.2f mm)", bevelDepthMM, frameA)
		}
		if frameB > 0 && 2.0*bevelDepthMM >= frameB {
			return fmt.Errorf("invalid -bevel-depth (%.2f mm/side): 2*bevel-depth must be less than frame B (%.2f mm)", bevelDepthMM, frameB)
		}
	}

	inputs := lens.ReportInputs{
		OD: lens.EyePrescription{
			Sphere:   odSph,
			Cyl:      odCyl,
			Axis:     odAxis,
			PD:       odPD,
			OCHeight: odOCH,
		},
		OS: lens.EyePrescription{
			Sphere:   osSph,
			Cyl:      osCyl,
			Axis:     osAxis,
			PD:       osPD,
			OCHeight: osOCH,
		},
		RefractiveIndex:      rIndex,
		Family:               material,
		SpecificGravity:      specGrav,
		InputBaseCurve:       baseCurve,
		InputCenterThickness: centerThickness,
		Chamfer:              chamferMM,
		BevelDepth:           bevelDepthMM,
		Frame: lens.FrameSpec{
			A:   frameA,
			B:   frameB,
			DBL: frameDBL,
		},
	}

	if hasConfig && (odEyeIdx != osEyeIdx || odEyeMat != osEyeMat || odEyeSG != osEyeSG || odEyeBC != osEyeBC || odEyeCT != osEyeCT) {
		inputs.OD.RefractiveIndex = odEyeIdx
		inputs.OD.Family = odEyeMat
		inputs.OD.SpecificGravity = odEyeSG
		inputs.OD.InputBaseCurve = odEyeBC
		inputs.OD.InputCenterThickness = odEyeCT
		inputs.OS.RefractiveIndex = osEyeIdx
		inputs.OS.Family = osEyeMat
		inputs.OS.SpecificGravity = osEyeSG
		inputs.OS.InputBaseCurve = osEyeBC
		inputs.OS.InputCenterThickness = osEyeCT
	}

	if recommendFlag {
		region, err := optics.ResolveMarketRegion(regionFlag)
		if err != nil {
			return err
		}
		if wClarity < 0 || wThickness < 0 || wWeight < 0 || wCost < 0 || wAvail < 0 {
			return fmt.Errorf("recommendation weights must be >= 0")
		}
		recShape := shapes[0]
		if traceFlag == "" && shapeFlag == "" {
			if sq, err := lens.ResolveShapes("squircle"); err == nil && len(sq) > 0 {
				recShape = sq[0]
			}
		}
		recDesign := designs[0]
		if designFlag == "" {
			recDesign = optics.DesignAspheric
		}
		weights := lens.RecommendWeights{
			Clarity:      wClarity,
			Thickness:    wThickness,
			Weight:       wWeight,
			Cost:         wCost,
			Availability: wAvail,
		}
		if priorityFlag != "" {
			order, err := lens.ParsePriorityOrder(priorityFlag)
			if err != nil {
				return err
			}
			weights = lens.WeightsForPriorityOrder(order)
		}
		rec := lens.RecommendMaterials(inputs, recShape, recDesign, lens.RecommendOptions{
			ODCenterOfRotationMM: odCorMM,
			OSCenterOfRotationMM: osCorMM,
			FamilyFilter:         material,
			Region:               region,
			Weights:              weights,
		})
		return lens.WriteRecommendationReport(stdout, rec, format)
	}

	report := lens.BuildReport(inputs, shapes, designs)
	return lens.WriteReport(stdout, report, format)
}

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		if err == flag.ErrHelp {
			os.Exit(0)
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

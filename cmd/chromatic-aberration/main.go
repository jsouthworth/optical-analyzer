package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"jsouthworth.net/go/optical-analyzer/contour"
	"jsouthworth.net/go/optical-analyzer/dsm"
	"jsouthworth.net/go/optical-analyzer/internal/version"
	"jsouthworth.net/go/optical-analyzer/optics"
)

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("chromatic-aberration", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		dataFile     string
		traceFile    string
		outputODFile string
		outputOSFile string
		designFlag   string
		materialFlag string
		dimOutside   bool
		noBox        bool
		showVersion  bool
	)

	fs.StringVar(&dataFile, "data", "", "path to optical JSON data file, or '-' for stdin (may also be given as a positional argument)")
	fs.StringVar(&traceFile, "trace", "", "optional frame trace file (.trc/.oma, .json, or .svg) to overlay, or '-' for stdin")
	fs.StringVar(&outputODFile, "od", "od.png", "OD output PNG file path")
	fs.StringVar(&outputOSFile, "os", "os.png", "OS output PNG file path")
	fs.StringVar(&designFlag, "design", "biaspheric", "lens design for auto base curve/thickness: biaspheric, aspheric, or spherical")
	fs.StringVar(&materialFlag, "material", "auto", "material family for auto thickness/Abbe: auto, plastic, or glass")
	fs.BoolVar(&dimOutside, "dim-outside", false, "dim chromatic aberration outside the loaded frame trace")
	fs.BoolVar(&noBox, "no-box", false, "hide rectangular frame bounding box overlay")
	fs.BoolVar(&showVersion, "version", false, "print version and exit")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if showVersion {
		fmt.Fprintf(stdout, "chromatic-aberration %s\n", version.String())
		return nil
	}

	if dataFile == "" {
		if fs.NArg() == 1 {
			dataFile = fs.Arg(0)
		} else if fs.NArg() > 1 {
			return fmt.Errorf("expected a single optical data file path, got %d positional arguments", fs.NArg())
		} else {
			return fmt.Errorf("missing optical data file: pass -data <path> (or positional argument, or '-' for stdin)")
		}
	} else if fs.NArg() > 0 {
		return fmt.Errorf("unexpected positional arguments when -data is already set")
	}

	if dataFile == "-" && traceFile == "-" {
		return fmt.Errorf("-data - and -trace - cannot both read from stdin")
	}

	setFlags := make(map[string]bool)
	fs.Visit(func(f *flag.Flag) {
		setFlags[f.Name] = true
	})

	design, err := optics.ResolveLensDesign(designFlag)
	if err != nil {
		return err
	}
	mat, err := optics.ResolveMaterialFamily(materialFlag)
	if err != nil {
		return err
	}

	data, err := optics.LoadDataFromPath(dataFile, stdin)
	if err != nil {
		return err
	}

	var trace *contour.Trace
	if traceFile != "" {
		trace, err = contour.ReadFromPath(traceFile, "", stdin, 0, 0, 0)
		if err != nil {
			return err
		}
		data.Frame = optics.FrameFromTrace(trace, data.Frame)
	} else if data.Frame.Trace != "" {
		if dataFile == "-" && data.Frame.Trace == "-" {
			return fmt.Errorf("-data - and frame.trace \"-\" cannot both read from stdin")
		}
		trace, err = optics.LoadTraceForData(dataFile, data, stdin)
		if err != nil {
			return err
		}
		data.Frame = optics.FrameFromTrace(trace, data.Frame)
	}

	data = optics.NormalizeData(data, optics.NormalizeOptions{
		Design:         design,
		OverrideDesign: setFlags["design"],
		ODFamily:       mat,
		OSFamily:       mat,
		Trace:          trace,
	})

	odImg, osImg := dsm.RenderChromaticAberrationWithTrace(data, trace, dsm.TraceOverlayOptions{
		ShowTrace:       trace != nil,
		ShowBoundingBox: !noBox,
		DimOutsideTrace: dimOutside,
	})
	if err := dsm.SavePNG(outputODFile, odImg); err != nil {
		return err
	}
	return dsm.SavePNG(outputOSFile, osImg)
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

package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"jsouthworth.net/go/optical-analyzer/contour"
	"jsouthworth.net/go/optical-analyzer/internal/version"
	"jsouthworth.net/go/optical-analyzer/tracer"
)

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("lens-trace", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		imagePath        string
		outPath          string
		formatFlag       string
		frameA           float64
		frameB           float64
		frameDBL         float64
		grooveDepthMM    float64
		debugOverlayPath string
		odPD, osPD       float64
		odOCH, osOCH     float64
		showVersion      bool
	)

	fs.StringVar(&imagePath, "image", "", "Path to front-view frame photo (PNG/JPEG), or '-' for stdin (may also be given as a positional argument)")
	fs.StringVar(&outPath, "out", "-", "Output trace file path, or '-' for stdout")
	fs.StringVar(&formatFlag, "format", "", "Output trace format: oma (default), json, or svg (empty = infer from -out extension or default to oma)")
	fs.Float64Var(&frameA, "a", 0, "Frame stamped A measurement in mm (required)")
	fs.Float64Var(&frameB, "b", 0, "Frame stamped B measurement in mm (0 = infer from photo)")
	fs.Float64Var(&frameDBL, "dbl", 0, "Frame DBL measurement in mm (0 = infer from photo)")
	fs.Float64Var(&grooveDepthMM, "groove-depth", tracer.DefaultGrooveDepthMM, "Eyewire groove depth per side in mm (stamped A = visible opening + 2*groove-depth; 0 = exact visible opening)")
	fs.StringVar(&debugOverlayPath, "debug-overlay", "", "Optional PNG output path to draw detected lens contours, boxing rectangles, and optical centers")
	fs.Float64Var(&odPD, "odpd", 0, "Optional OD PD in mm for -debug-overlay crosshair (0 = auto (A+DBL)/2)")
	fs.Float64Var(&osPD, "ospd", 0, "Optional OS PD in mm for -debug-overlay crosshair (0 = auto (A+DBL)/2)")
	fs.Float64Var(&odOCH, "odoch", 0, "Optional OD OC height from bottom in mm for -debug-overlay crosshair (0 = auto B/2)")
	fs.Float64Var(&osOCH, "osoch", 0, "Optional OS OC height from bottom in mm for -debug-overlay crosshair (0 = auto B/2)")
	fs.BoolVar(&showVersion, "version", false, "Print version and exit")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if showVersion {
		fmt.Fprintf(stdout, "lens-trace %s\n", version.String())
		return nil
	}

	if imagePath == "" {
		if fs.NArg() == 1 {
			imagePath = fs.Arg(0)
		} else if fs.NArg() > 1 {
			return fmt.Errorf("expected a single frame image path, got %d positional arguments", fs.NArg())
		} else {
			return fmt.Errorf("missing frame image: pass -image <path> (or positional argument, or '-' for stdin)")
		}
	} else if fs.NArg() > 0 {
		return fmt.Errorf("unexpected positional arguments when -image is already set")
	}

	traced, err := tracer.TraceFile(imagePath, stdin, frameA, frameB, grooveDepthMM)
	if err != nil {
		return err
	}

	effectiveB := frameB
	if effectiveB <= 0 {
		effectiveB = traced.InferredB
	}
	effectiveDBL := frameDBL
	if effectiveDBL <= 0 {
		effectiveDBL = traced.InferredDBL
	}

	if debugOverlayPath != "" {
		if err := tracer.WriteDebugOverlayFile(debugOverlayPath, traced, tracer.OverlayParams{
			FrameA:   frameA,
			FrameB:   effectiveB,
			FrameDBL: effectiveDBL,
			ODPD:     odPD,
			OSPD:     osPD,
			ODOCH:    odOCH,
			OSOCH:    osOCH,
		}); err != nil {
			return err
		}
	}

	name := "Traced (stdin)"
	source := "stdin"
	if imagePath != "-" {
		name = fmt.Sprintf("Traced (%s)", filepath.Base(imagePath))
		source = imagePath
	}

	tr := traced.ToContourTrace(name, source, frameA, effectiveB, effectiveDBL)
	return contour.WriteToPath(outPath, formatFlag, stdout, tr)
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

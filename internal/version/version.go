// Package version holds the build-stamped release version for Optical Analyzer
// and its companion CLI tools.
package version

import "runtime/debug"

// Version may be injected at link time via:
//
//	-ldflags "-X jsouthworth.net/go/optical-analyzer/internal/version.Version=1.0.0"
var Version = ""

// String returns the resolved application version, preferring the link-time
// Version string, then the Go module build info version, and finally "dev".
func String() string {
	if Version != "" {
		return Version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}

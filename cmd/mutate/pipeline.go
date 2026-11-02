package main

import (
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/rfdetoni/mutate/internal/detector"
	"github.com/rfdetoni/mutate/internal/domain"
	"github.com/rfdetoni/mutate/internal/elfanalyzer"
	"github.com/rfdetoni/mutate/internal/fhs"
	"github.com/rfdetoni/mutate/internal/resolver"
	"github.com/rfdetoni/mutate/internal/scripts"
	"github.com/rfdetoni/mutate/internal/unpacker"
)

func distroForFormat(target string) (resolver.Distro, domain.Format, error) {
	switch strings.ToLower(target) {
	case "deb":
		return resolver.DistroDebian, domain.FormatDEB, nil
	case "rpm":
		return resolver.DistroFedora, domain.FormatRPM, nil
	case "arch":
		return resolver.DistroArch, domain.FormatArch, nil
	case "apk":
		return resolver.DistroAlpine, domain.FormatAlpineAPK, nil
	default:
		return resolver.DistroOther, domain.FormatUnknown, fmt.Errorf("unsupported target format %q (deb, rpm, arch, apk)", target)
	}
}

// prepareConversion is the single conversion path used by build and install.
// Its cleanup function must be called even when later packaging fails.
func prepareConversion(src, target string, allowSourceBuild bool, libraryDirs ...string) (*domain.PackageIR, func(), error) {
	targetDistro, targetFormat, err := distroForFormat(target)
	if err != nil {
		return nil, nil, err
	}
	format, err := detector.DetectFile(src)
	if err != nil {
		return nil, nil, err
	}
	stage, err := os.MkdirTemp("", "mutate-convert-*")
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() { _ = os.RemoveAll(stage) }
	fail := func(e error) (*domain.PackageIR, func(), error) { cleanup(); return nil, nil, e }
	u, err := getUnpackerForFormat(format)
	if err != nil {
		return fail(err)
	}
	ir, err := u.Unpack(src, stage)
	if err != nil {
		return fail(fmt.Errorf("unpack source: %w", err))
	}
	if ir.Metadata.Name == "" || strings.ContainsAny(ir.Metadata.Name, "/\\\x00\n\r\t\"' ;$`") || ir.Metadata.Name == "." || ir.Metadata.Name == ".." {
		return fail(fmt.Errorf("invalid package name %q", ir.Metadata.Name))
	}
	if format == domain.FormatTarball {
		if !allowSourceBuild {
			return fail(fmt.Errorf("source packages require --allow-source-build; this executes untrusted build tools on the host without sandboxing"))
		}
		if err := u.(*unpacker.SourceUnpacker).BuildSource(ir); err != nil {
			return fail(fmt.Errorf("compile source: %w", err))
		}
	}
	analysis, err := elfanalyzer.AnalyzePackage(ir)
	if err != nil {
		return fail(err)
	}
	host := resolver.DetectHostDistro()
	if targetDistro != host {
		if format != targetFormat && (len(ir.Metadata.Dependencies) > 0 || len(analysis.ExternalLibraries) > 0) {
			return fail(fmt.Errorf("cannot safely map dependencies for %s target from %s host; use a target-distro environment or provide a dependency mapping", targetDistro, host))
		}
		// Repackaging the same format preserves existing dependency metadata.
	} else {
		if format != targetFormat && len(ir.Metadata.Dependencies) > 0 {
			return fail(fmt.Errorf("source package dependencies %v cannot be translated safely to %s without a mapping", ir.Metadata.Dependencies, targetDistro))
		}
		arch := strings.ToLower(ir.Metadata.Architecture)
		if arch != "all" && arch != "noarch" && arch != "any" && arch != "" && ((runtime.GOARCH == "amd64" && arch != "amd64" && arch != "x86_64") || (runtime.GOARCH == "arm64" && arch != "arm64" && arch != "aarch64")) {
			return fail(fmt.Errorf("cannot resolve %s architecture dependencies on %s host", arch, runtime.GOARCH))
		}
		if len(analysis.ExternalLibraries) > 0 {
			res, err := resolver.NewResolverFor(targetDistro)
			if err != nil {
				return fail(err)
			}
			dedup := make(map[string]bool)
			for _, dep := range ir.Metadata.Dependencies {
				dedup[dep] = true
			}
			var missing []string
			for _, lib := range analysis.ExternalLibraries {
				dep, err := res.Resolve(lib)
				if err != nil {
					missing = append(missing, lib)
					continue
				}
				if !dedup[dep] {
					ir.Metadata.Dependencies = append(ir.Metadata.Dependencies, dep)
					dedup[dep] = true
				}
			}
			if len(missing) > 0 {
				dir := ""
				if len(libraryDirs) > 0 {
					dir = libraryDirs[0]
				}
				if dir == "" {
					return fail(fmt.Errorf("required libraries %v unavailable on target; supply --library-dir with matching ELF objects", missing))
				}
				if err := resolver.NewLibraryBundler(ir.Metadata.Name).BundleFromDir(ir, missing, dir); err != nil {
					return fail(fmt.Errorf("isolate libraries: %w", err))
				}
			}
		}
	}
	if format != targetFormat && (ir.Scripts.PreInstall != "" || ir.Scripts.PostInstall != "" || ir.Scripts.PreRemove != "" || ir.Scripts.PostRemove != "") {
		return fail(fmt.Errorf("cross-format maintainer scripts require lifecycle mapping; refusing unsafe conversion"))
	}
	if err := fhs.NormalizePackage(ir, targetDistro); err != nil {
		return fail(fmt.Errorf("normalize FHS: %w", err))
	}
	if err := scripts.TranspilePackageScripts(ir, targetFormat); err != nil {
		return fail(fmt.Errorf("translate scripts: %w", err))
	}
	return ir, cleanup, nil
}

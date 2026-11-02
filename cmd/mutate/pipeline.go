package main

import (
	"encoding/json"
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

// conversionOptions defines explicit configuration for cross-distro conversion.
type conversionOptions struct {
	LibraryDir        string
	DependencyMapPath string
}

// prepareConversion maintains compatibility with existing callers.
func prepareConversion(src, target string, allowSourceBuild bool, libraryDirs ...string) (*domain.PackageIR, func(), error) {
	opts := conversionOptions{}
	if len(libraryDirs) > 0 {
		opts.LibraryDir = libraryDirs[0]
	}
	return prepareConversionWithOptions(src, target, allowSourceBuild, opts)
}

func readDependencyMap(path string) (map[string]string, error) {
	if path == "" {
		return map[string]string{}, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read dependency map: %w", err)
	}
	var mapped map[string]string
	if err := json.Unmarshal(b, &mapped); err != nil {
		return nil, fmt.Errorf("parse dependency map JSON: %w", err)
	}
	if mapped == nil {
		return nil, fmt.Errorf("dependency map must be a JSON object")
	}
	for from, to := range mapped {
		if strings.TrimSpace(from) == "" || strings.TrimSpace(to) == "" || strings.ContainsAny(to, "\n\r\x00") {
			return nil, fmt.Errorf("invalid dependency mapping %q -> %q", from, to)
		}
	}
	return mapped, nil
}

// prepareConversionWithOptions fails closed when package dependencies, ABI,
// or lifecycle transformations lack a verified target mapping.
func prepareConversionWithOptions(src, target string, allowSourceBuild bool, opts conversionOptions) (*domain.PackageIR, func(), error) {
	targetDistro, targetFormat, err := distroForFormat(target)
	if err != nil {
		return nil, nil, err
	}
	mapped, err := readDependencyMap(opts.DependencyMapPath)
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
	fail := func(err error) (*domain.PackageIR, func(), error) { cleanup(); return nil, nil, err }
	u, err := getUnpackerForFormat(format)
	if err != nil {
		return fail(err)
	}
	ir, err := u.Unpack(src, stage)
	if err != nil {
		return fail(fmt.Errorf("unpack: %w", err))
	}
	if ir.Metadata.Name == "" || strings.ContainsAny(ir.Metadata.Name, "/\\\x00\n\r\t\"' ;$`") || ir.Metadata.Name == "." || ir.Metadata.Name == ".." {
		return fail(fmt.Errorf("invalid package name %q", ir.Metadata.Name))
	}
	if format == domain.FormatTarball {
		if !allowSourceBuild {
			return fail(fmt.Errorf("source compilation requires --allow-source-build and bubblewrap"))
		}
		if err := u.(*unpacker.SourceUnpacker).BuildSource(ir); err != nil {
			return fail(fmt.Errorf("sandboxed source build: %w", err))
		}
	}
	analysis, err := elfanalyzer.AnalyzePackage(ir)
	if err != nil {
		return fail(err)
	}
	host := resolver.DetectHostDistro()
	crossFormat := format != targetFormat && format != domain.FormatAppImage && format != domain.FormatTarball && format != domain.FormatAndroidAPK
	// Cross-format package metadata is not assumed to be equivalent.
	if crossFormat {
		var targetDeps []string
		for _, dep := range ir.Metadata.Dependencies {
			targetDep, ok := mapped[dep]
			if !ok {
				return fail(fmt.Errorf("dependency %q requires explicit --dependency-map for %s", dep, targetDistro))
			}
			targetDeps = append(targetDeps, targetDep)
		}
		ir.Metadata.Dependencies = targetDeps
		// Source virtual provisions are not valid target package capabilities.
		ir.Metadata.Provides = nil
		ir.Metadata.Recommends = nil
	}
	// Only native host libraries are discoverable from the host DB. Otherwise,
	// the map must specify each ELF SONAME with an explicit "so:" key.
	arch := strings.ToLower(ir.Metadata.Architecture)
	if targetDistro == host && arch != "all" && arch != "noarch" && arch != "any" && arch != "" &&
		((runtime.GOARCH == "amd64" && arch != "amd64" && arch != "x86_64") ||
			(runtime.GOARCH == "arm64" && arch != "arm64" && arch != "aarch64")) {
		return fail(fmt.Errorf("cannot resolve %s binaries against %s host", arch, runtime.GOARCH))
	}
	unique := make(map[string]bool)
	for _, dep := range ir.Metadata.Dependencies {
		unique[dep] = true
	}
	var missing []string
	var native resolver.DependencyResolver
	if targetDistro == host {
		native, err = resolver.NewResolverFor(targetDistro)
		if err != nil {
			return fail(err)
		}
	}
	for _, lib := range analysis.ExternalLibraries {
		provider := mapped["so:"+lib]
		if provider == "" && native != nil {
			if found, e := native.Resolve(lib); e == nil {
				provider = found
			}
		}
		if provider == "" {
			missing = append(missing, lib)
			continue
		}
		if !unique[provider] {
			ir.Metadata.Dependencies = append(ir.Metadata.Dependencies, provider)
			unique[provider] = true
		}
	}
	if len(missing) > 0 {
		if opts.LibraryDir == "" {
			return fail(fmt.Errorf("target %s libraries %v unresolved; provide --dependency-map with so: entries or --library-dir", targetDistro, missing))
		}
		if err := resolver.NewLibraryBundler(ir.Metadata.Name).BundleFromDir(ir, missing, opts.LibraryDir); err != nil {
			return fail(fmt.Errorf("bundle target libraries: %w", err))
		}
	}
	if err := fhs.NormalizePackage(ir, targetDistro); err != nil {
		return fail(fmt.Errorf("normalize FHS: %w", err))
	}
	if err := scripts.TranspilePackageScripts(ir, targetFormat); err != nil {
		return fail(fmt.Errorf("translate scripts: %w", err))
	}
	return ir, cleanup, nil
}

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rfdetoni/mutate/internal/detector"
	"github.com/rfdetoni/mutate/internal/domain"
	"github.com/rfdetoni/mutate/internal/elfanalyzer"
	"github.com/rfdetoni/mutate/internal/packager"
	"github.com/rfdetoni/mutate/internal/prerequisites"
	"github.com/rfdetoni/mutate/internal/resolver"
	"github.com/rfdetoni/mutate/internal/unpacker"
	"github.com/spf13/cobra"
)

var (
	version = "1.0.0"
)

func main() {
	rootCmd := &cobra.Command{
		Use:     "mutate",
		Aliases: []string{"mut"},
		Short:   "M.U.T.A.T.E. - Multi-format Universal Transpiler & Application Translation Engine",
		Version: version,
	}

	rootCmd.AddCommand(newInspectCmd())
	rootCmd.AddCommand(newBuildCmd())
	rootCmd.AddCommand(newInstallCmd())

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func getUnpackerForFormat(fmtType domain.Format) (unpacker.Unpacker, error) {
	switch fmtType {
	case domain.FormatDEB:
		return unpacker.NewDebUnpacker(), nil
	case domain.FormatRPM:
		return unpacker.NewRpmUnpacker(), nil
	case domain.FormatArch:
		return unpacker.NewArchUnpacker(), nil
	case domain.FormatAlpineAPK:
		return unpacker.NewAlpineUnpacker(), nil
	case domain.FormatAndroidAPK:
		return unpacker.NewAndroidUnpacker(), nil
	case domain.FormatTarball:
		return unpacker.NewSourceUnpacker(), nil
	case domain.FormatAppImage:
		return unpacker.NewAppImageUnpacker(), nil
	default:
		return nil, fmt.Errorf("unsupported package format: %s", fmtType)
	}
}

func newInspectCmd() *cobra.Command {
	var extract, fast, jsonOutput bool
	cmd := &cobra.Command{
		Use:   "inspect <package-file>",
		Short: "Inspect package metadata, ELF binaries, interpreter and dependencies",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			src := args[0]
			if fast && extract {
				return fmt.Errorf("--fast and --extract cannot be combined")
			}
			format, err := detector.DetectFile(src)
			if err != nil {
				return fmt.Errorf("detect format: %w", err)
			}
			if extract && format != domain.FormatAppImage {
				return fmt.Errorf("--extract only applies to AppImage")
			}
			if !jsonOutput {
				fmt.Fprintf(cmd.OutOrStdout(), "Analyzing %s...\nFormat detected: %s\n\n", src, strings.ToUpper(string(format)))
			}
			informRuntimePrerequisites(cmd, format, src, extract)

			var ir *domain.PackageIR
			// Native formats provide their package metadata in their headers;
			// reading and extracting the payload is unnecessary for --fast.
			if fast && (format == domain.FormatDEB || format == domain.FormatRPM ||
				format == domain.FormatArch || format == domain.FormatAlpineAPK || format == domain.FormatTarball) {
				metadata, err := unpacker.InspectMetadata(src, format)
				if err != nil {
					return fmt.Errorf("fast inspect: %w", err)
				}
				if jsonOutput {
					return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{
						"format": format, "fast": true, "metadata": metadata,
					})
				}
				printInspectMetadata(cmd, metadata, -1)
				fmt.Fprintln(cmd.OutOrStdout(), "ELF analysis skipped (--fast)")
				return nil
			}
			stage, err := os.MkdirTemp("", "mutate-inspect-*")
			if err != nil {
				return err
			}
			defer os.RemoveAll(stage)
			u, err := getUnpackerForFormat(format)
			if err != nil {
				return err
			}
			if !extract {
				if preserver, ok := u.(unpacker.PortablePreserver); ok {
					ir, err = preserver.Preserve(src, stage)
				} else {
					ir, err = u.Unpack(src, stage)
				}
			} else {
				ir, err = u.Unpack(src, stage)
			}
			if err != nil {
				return fmt.Errorf("inspect package: %w", err)
			}
			if fast {
				if jsonOutput {
					return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{
						"format": format, "fast": true, "metadata": ir.Metadata,
					})
				}
				printInspectMetadata(cmd, ir.Metadata, -1)
				fmt.Fprintln(cmd.OutOrStdout(), "ELF analysis skipped (--fast)")
				return nil
			}
			analysis, err := elfanalyzer.AnalyzePackage(ir)
			if err != nil {
				return fmt.Errorf("analyze package ELFs: %w", err)
			}
			if jsonOutput {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{
					"format": format, "fast": false, "metadata": ir.Metadata,
					"file_count": len(ir.Files), "elf": analysis,
				})
			}
			printInspectMetadata(cmd, ir.Metadata, len(ir.Files))
			fmt.Fprintln(cmd.OutOrStdout(), "=== ELF Binary Analysis ===")
			fmt.Fprintf(cmd.OutOrStdout(), "Total ELF Binaries: %d\n", analysis.TotalELFs)
			fmt.Fprint(cmd.OutOrStdout(), "Target libc:        ")
			switch {
			case analysis.HasMusl && analysis.HasGlibc:
				fmt.Fprintln(cmd.OutOrStdout(), "Mixed (musl + glibc)")
			case analysis.HasMusl:
				fmt.Fprintln(cmd.OutOrStdout(), "musl libc")
			case analysis.HasGlibc:
				fmt.Fprintln(cmd.OutOrStdout(), "glibc")
			default:
				fmt.Fprintln(cmd.OutOrStdout(), "Pure static / None")
			}
			res := resolver.NewAutoResolver()
			fmt.Fprintf(cmd.OutOrStdout(), "Host Distribution:  %s\n\n", res.HostDistro())
			if len(analysis.ExternalLibraries) > 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "=== External Shared Libraries (.so) & Host Package Providers ===")
				for _, lib := range analysis.ExternalLibraries {
					provider, err := res.Resolve(lib)
					if err != nil {
						fmt.Fprintf(cmd.OutOrStdout(), "  • %-25s -> [NOT FOUND on host; explicit mapping or --library-dir required]\n", lib)
					} else {
						fmt.Fprintf(cmd.OutOrStdout(), "  • %-25s -> %s\n", lib, provider)
					}
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&extract, "extract", false, "Extract AppImage filesystem for deep inspection")
	cmd.Flags().BoolVar(&fast, "fast", false, "Read package metadata without extracting its payload or analyzing ELF files")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output machine-readable JSON (runtime notices go to stderr)")
	return cmd
}

func printInspectMetadata(cmd *cobra.Command, meta domain.Metadata, totalFiles int) {
	out := cmd.OutOrStdout()
	fmt.Fprintln(out, "=== Package Metadata ===")
	fmt.Fprintf(out, "Name:         %s\n", meta.Name)
	fmt.Fprintf(out, "Version:      %s-%s\n", meta.Version, meta.Release)
	fmt.Fprintf(out, "Architecture: %s\n", meta.Architecture)
	fmt.Fprintf(out, "Description:  %s\n", strings.TrimSpace(meta.Description))
	if totalFiles >= 0 {
		fmt.Fprintf(out, "Total Files:  %d\n", totalFiles)
	}
	fmt.Fprintln(out)
}

func newBuildCmd() *cobra.Command {
	var targetFormat, outputPath string
	var allowSourceBuild bool
	var libraryDir string
	var dependencyMapPath string
	var extract bool
	var packageName, packageVersion string
	cmd := &cobra.Command{
		Use:   "build <package-file> --to <deb|rpm|arch|apk> [-o output]",
		Short: "Convert a package to a native Linux package",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := detector.DetectFile(args[0])
			if err != nil {
				return err
			}
			informRuntimePrerequisites(cmd, format, args[0], extract)
			ir, cleanup, err := prepareConversionWithOptions(args[0], targetFormat, allowSourceBuild, conversionOptions{LibraryDir: libraryDir, DependencyMapPath: dependencyMapPath, Extract: extract, PackageName: packageName, PackageVersion: packageVersion})
			if err != nil {
				return err
			}
			defer cleanup()
			out := outputPath
			if out == "" {
				ext := targetFormat
				if ext == "arch" {
					ext = "pkg.tar.zst"
				}
				out = fmt.Sprintf("%s_%s-%s_%s.%s", ir.Metadata.Name, ir.Metadata.Version, ir.Metadata.Release, ir.Metadata.Architecture, ext)
			}
			if err := packager.BuildPackageToFile(ir, targetFormat, out); err != nil {
				return err
			}
			fmt.Printf("Package created: %s\n", out)
			return nil
		},
	}
	cmd.Flags().StringVar(&targetFormat, "to", "", "Target format (deb, rpm, arch, apk)")
	cmd.Flags().StringVarP(&outputPath, "output", "o", "", "Output package path")
	cmd.Flags().BoolVar(&allowSourceBuild, "allow-source-build", false, "Build trusted source in mandatory bubblewrap sandbox (offline)")
	cmd.Flags().StringVar(&libraryDir, "library-dir", "", "Directory of explicitly supplied ELF libraries to isolate")
	cmd.Flags().StringVar(&dependencyMapPath, "dependency-map", "", "JSON map of source dependencies / so:library names to target packages")
	cmd.Flags().BoolVar(&extract, "extract", false, "Extract AppImage files instead of packaging the original artifact")
	cmd.Flags().StringVar(&packageName, "package-name", "", "Override AppImage package name")
	cmd.Flags().StringVar(&packageVersion, "package-version", "", "Override AppImage package version")
	_ = cmd.MarkFlagRequired("to")
	return cmd
}

func newInstallCmd() *cobra.Command {
	var dryRun, allowSourceBuild bool
	var libraryDir string
	var dependencyMapPath string
	var extract bool
	var packageName, packageVersion string
	cmd := &cobra.Command{
		Use:   "install <package-file> [--dry-run]",
		Short: "Convert to host format and install with native package manager",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			host := resolver.DetectHostDistro()
			var target string
			switch host {
			case resolver.DistroDebian:
				target = "deb"
			case resolver.DistroFedora:
				target = "rpm"
			case resolver.DistroArch:
				target = "arch"
			case resolver.DistroAlpine:
				target = "apk"
			default:
				return fmt.Errorf("unsupported host distribution %q", host)
			}
			src := args[0]
			sourceFmt, err := detector.DetectFile(src)
			if err != nil {
				return err
			}
			informRuntimePrerequisites(cmd, sourceFmt, src, extract)
			if dryRun {
				fmt.Printf("[dry-run] Would convert %s (%s) to %s and request installation; no source build, packaging, or installation executed\n", src, sourceFmt, target)
				return nil
			}
			ir, cleanup, err := prepareConversionWithOptions(src, target, allowSourceBuild, conversionOptions{LibraryDir: libraryDir, DependencyMapPath: dependencyMapPath, Extract: extract, PackageName: packageName, PackageVersion: packageVersion})
			if err != nil {
				return err
			}
			defer cleanup()
			// Unique secure temporary directory; never derive a path from PID alone.
			out := filepath.Join(ir.StagingDir, "mutate-output."+map[string]string{"arch": "pkg.tar.zst", "deb": "deb", "rpm": "rpm", "apk": "apk"}[target])
			if err := packager.BuildPackageToFile(ir, target, out); err != nil {
				return err
			}
			return packager.InstallPackage(out, host, false)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print planned operation without executing conversion or install")
	cmd.Flags().BoolVar(&allowSourceBuild, "allow-source-build", false, "Build trusted source in mandatory bubblewrap sandbox (offline)")
	cmd.Flags().StringVar(&libraryDir, "library-dir", "", "Directory of explicitly supplied ELF libraries to isolate")
	cmd.Flags().StringVar(&dependencyMapPath, "dependency-map", "", "JSON map of source dependencies / so:library names to target packages")
	cmd.Flags().BoolVar(&extract, "extract", false, "Extract AppImage files instead of packaging the original artifact")
	cmd.Flags().StringVar(&packageName, "package-name", "", "Override AppImage package name")
	cmd.Flags().StringVar(&packageVersion, "package-version", "", "Override AppImage package version")
	return cmd
}

func parseDomainFormat(s string) domain.Format {
	switch strings.ToLower(s) {
	case "deb":
		return domain.FormatDEB
	case "rpm":
		return domain.FormatRPM
	case "arch", "archlinux":
		return domain.FormatArch
	case "apk", "alpine":
		return domain.FormatAlpineAPK
	default:
		return domain.FormatUnknown
	}
}

// informRuntimePrerequisites reports host-local runtime checks without
// preventing conversion for a different target machine.
func informRuntimePrerequisites(cmd *cobra.Command, format domain.Format, source string, extract bool) {
	notices, err := prerequisites.Check(format, source, extract, prerequisites.DetectHostState(), resolver.DetectHostDistro())
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "[WARN] Could not check runtime prerequisites: %v\n", err)
		return
	}
	for _, notice := range notices {
		level := "INFO"
		if notice.Warning {
			level = "WARN"
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "[%s] %s\n", level, notice.Message)
	}
	if len(notices) > 0 {
		fmt.Fprintln(cmd.ErrOrStderr(), "[INFO] Checks reflect the current host, not necessarily the target system.")
	}
}

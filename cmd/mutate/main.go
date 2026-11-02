package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rfdetoni/mutate/internal/detector"
	"github.com/rfdetoni/mutate/internal/domain"
	"github.com/rfdetoni/mutate/internal/elfanalyzer"
	"github.com/rfdetoni/mutate/internal/fhs"
	"github.com/rfdetoni/mutate/internal/packager"
	"github.com/rfdetoni/mutate/internal/resolver"
	"github.com/rfdetoni/mutate/internal/scripts"
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
	default:
		return nil, fmt.Errorf("unsupported package format: %s", fmtType)
	}
}

func newInspectCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "inspect <package-file>",
		Short: "Inspect package metadata, ELF binaries, interpreter and dependencies",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			srcFile := args[0]
			fmt.Printf("Analyzing %s...\n", srcFile)

			format, err := detector.DetectFile(srcFile)
			if err != nil {
				return fmt.Errorf("detect format: %w", err)
			}
			fmt.Printf("Format detected: %s\n\n", strings.ToUpper(string(format)))

			stagingDir, err := os.MkdirTemp("", "mutate-inspect-*")
			if err != nil {
				return err
			}
			defer os.RemoveAll(stagingDir)

			u, err := getUnpackerForFormat(format)
			if err != nil {
				return err
			}

			ir, err := u.Unpack(srcFile, stagingDir)
			if err != nil {
				return fmt.Errorf("unpack package: %w", err)
			}

			// Package Metadata
			fmt.Println("=== Package Metadata ===")
			fmt.Printf("Name:         %s\n", ir.Metadata.Name)
			fmt.Printf("Version:      %s-%s\n", ir.Metadata.Version, ir.Metadata.Release)
			fmt.Printf("Architecture: %s\n", ir.Metadata.Architecture)
			fmt.Printf("Description:  %s\n", strings.TrimSpace(ir.Metadata.Description))
			fmt.Printf("Total Files:  %d\n\n", len(ir.Files))

			// ELF Analysis
			analysis, err := elfanalyzer.AnalyzePackage(ir)
			if err != nil {
				return fmt.Errorf("analyze package ELFs: %w", err)
			}

			fmt.Println("=== ELF Binary Analysis ===")
			fmt.Printf("Total ELF Binaries: %d\n", analysis.TotalELFs)
			fmt.Printf("Target libc:        ")
			if analysis.HasMusl && analysis.HasGlibc {
				fmt.Println("Mixed (musl + glibc)")
			} else if analysis.HasMusl {
				fmt.Println("musl libc")
			} else if analysis.HasGlibc {
				fmt.Println("glibc")
			} else {
				fmt.Println("Pure static / None")
			}

			res := resolver.NewAutoResolver()
			fmt.Printf("Host Distribution:  %s\n\n", res.HostDistro())

			if len(analysis.ExternalLibraries) > 0 {
				fmt.Println("=== External Shared Libraries (.so) & Host Package Providers ===")
				for _, lib := range analysis.ExternalLibraries {
					provider, err := res.Resolve(lib)
					if err != nil {
						fmt.Printf("  • %-25s -> [NOT FOUND on host - bundler will isolate]\n", lib)
					} else {
						fmt.Printf("  • %-25s -> %s\n", lib, provider)
					}
				}
			}

			return nil
		},
	}
}

func newBuildCmd() *cobra.Command {
	var targetFormat string
	var outputPath string

	cmd := &cobra.Command{
		Use:   "build <package-file> --to <deb|rpm|arch|apk> [-o output]",
		Short: "Transpile a package to another native Linux format without installing",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			srcFile := args[0]

			if targetFormat == "" {
				return fmt.Errorf("--to format flag is required (e.g. --to deb, --to rpm, --to arch, --to apk)")
			}

			format, err := detector.DetectFile(srcFile)
			if err != nil {
				return fmt.Errorf("detect format: %w", err)
			}

			stagingDir, err := os.MkdirTemp("", "mutate-build-*")
			if err != nil {
				return err
			}
			defer os.RemoveAll(stagingDir)

			u, err := getUnpackerForFormat(format)
			if err != nil {
				return err
			}

			fmt.Printf("[1/5] Unpacking %s (%s)...\n", srcFile, format)
			ir, err := u.Unpack(srcFile, stagingDir)
			if err != nil {
				return fmt.Errorf("unpack: %w", err)
			}

			fmt.Println("[2/5] Analyzing ELF binaries & resolving dynamic dependencies...")
			analysis, err := elfanalyzer.AnalyzePackage(ir)
			if err != nil {
				return fmt.Errorf("elf analysis: %w", err)
			}

			res := resolver.NewAutoResolver()
			var missingLibs []string
			var hostDeps []string
			for _, lib := range analysis.ExternalLibraries {
				pkg, err := res.Resolve(lib)
				if err != nil {
					missingLibs = append(missingLibs, lib)
				} else {
					hostDeps = append(hostDeps, pkg)
				}
			}

			if len(missingLibs) > 0 {
				fmt.Printf("      Isolating %d missing libraries via bundler wrapper...\n", len(missingLibs))
				bundler := resolver.NewLibraryBundler(ir.Metadata.Name)
				if err := bundler.BundleMissingLibraries(ir, missingLibs, res.HostDistro()); err != nil {
					return fmt.Errorf("bundle missing libs: %w", err)
				}
			}

			// Add discovered host dependencies to package metadata
			for _, dep := range hostDeps {
				ir.Metadata.Dependencies = append(ir.Metadata.Dependencies, dep)
			}

			fmt.Println("[3/5] Normalizing FHS library layout & configs...")
			if err := fhs.NormalizePackage(ir, res.HostDistro()); err != nil {
				return fmt.Errorf("fhs normalize: %w", err)
			}

			fmt.Println("[4/5] Transpiling maintainer scripts via AST...")
			destDomainFmt := parseDomainFormat(targetFormat)
			if err := scripts.TranspilePackageScripts(ir, destDomainFmt); err != nil {
				return fmt.Errorf("transpile scripts: %w", err)
			}

			if outputPath == "" {
				ext := targetFormat
				if ext == "arch" {
					ext = "pkg.tar.zst"
				}
				outputPath = fmt.Sprintf("%s_%s-%s_%s.%s",
					ir.Metadata.Name, ir.Metadata.Version, ir.Metadata.Release, ir.Metadata.Architecture, ext)
			}

			fmt.Printf("[5/5] Building native %s package -> %s\n", targetFormat, outputPath)
			if err := packager.BuildPackageToFile(ir, targetFormat, outputPath); err != nil {
				return fmt.Errorf("build package: %w", err)
			}

			fmt.Printf("\n✓ Transpilation complete! Output saved to: %s\n", outputPath)
			return nil
		},
	}

	cmd.Flags().StringVar(&targetFormat, "to", "", "Target package format (deb, rpm, arch, apk)")
	cmd.Flags().StringVarP(&outputPath, "output", "o", "", "Output package file path")
	cmd.MarkFlagRequired("to")

	return cmd
}

func newInstallCmd() *cobra.Command {
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "install <package-file> [--dry-run]",
		Short: "Transpile a package to current host format and install it natively",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			srcFile := args[0]

			hostDistro := resolver.DetectHostDistro()
			var targetFormat string
			switch hostDistro {
			case resolver.DistroDebian:
				targetFormat = "deb"
			case resolver.DistroArch:
				targetFormat = "arch"
			case resolver.DistroFedora:
				targetFormat = "rpm"
			case resolver.DistroAlpine:
				targetFormat = "apk"
			default:
				targetFormat = "deb"
			}

			fmt.Printf("Host distro is %s -> compiling to native %s\n", hostDistro, targetFormat)

			tmpPkg := filepath.Join(os.TempDir(), fmt.Sprintf("mutate-install-%d.%s", os.Getpid(), targetFormat))
			defer os.Remove(tmpPkg)

			// Run build logic into tmpPkg
			format, err := detector.DetectFile(srcFile)
			if err != nil {
				return fmt.Errorf("detect format: %w", err)
			}

			stagingDir, err := os.MkdirTemp("", "mutate-staging-*")
			if err != nil {
				return err
			}
			defer os.RemoveAll(stagingDir)

			u, err := getUnpackerForFormat(format)
			if err != nil {
				return err
			}

			ir, err := u.Unpack(srcFile, stagingDir)
			if err != nil {
				return fmt.Errorf("unpack: %w", err)
			}

			analysis, err := elfanalyzer.AnalyzePackage(ir)
			if err != nil {
				return fmt.Errorf("elf analysis: %w", err)
			}

			res := resolver.NewAutoResolver()
			var missingLibs []string
			for _, lib := range analysis.ExternalLibraries {
				pkg, err := res.Resolve(lib)
				if err != nil {
					missingLibs = append(missingLibs, lib)
				} else {
					ir.Metadata.Dependencies = append(ir.Metadata.Dependencies, pkg)
				}
			}

			if len(missingLibs) > 0 {
				bundler := resolver.NewLibraryBundler(ir.Metadata.Name)
				if err := bundler.BundleMissingLibraries(ir, missingLibs, hostDistro); err != nil {
					return fmt.Errorf("bundle missing libs: %w", err)
				}
			}

			if err := fhs.NormalizePackage(ir, hostDistro); err != nil {
				return fmt.Errorf("fhs normalize: %w", err)
			}

			destDomainFmt := parseDomainFormat(targetFormat)
			if err := scripts.TranspilePackageScripts(ir, destDomainFmt); err != nil {
				return fmt.Errorf("transpile scripts: %w", err)
			}

			if err := packager.BuildPackageToFile(ir, targetFormat, tmpPkg); err != nil {
				return fmt.Errorf("build package: %w", err)
			}

			fmt.Printf("Installing %s...\n", tmpPkg)
			return packager.InstallPackage(tmpPkg, hostDistro, dryRun)
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Simulate installation without modifying host")
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

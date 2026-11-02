package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rfdetoni/mutate/internal/detector"
	"github.com/rfdetoni/mutate/internal/domain"
	"github.com/rfdetoni/mutate/internal/elfanalyzer"
	"github.com/rfdetoni/mutate/internal/packager"
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
  return unpacker.NewAppImageUnpacker(),nil
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
						fmt.Printf("  • %-25s -> [NOT FOUND on host; explicit mapping or --library-dir required]\n", lib)
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
	var targetFormat, outputPath string
	var allowSourceBuild bool
	var libraryDir string
 var dependencyMapPath string
 cmd := &cobra.Command{
		Use:   "build <package-file> --to <deb|rpm|arch|apk> [-o output]",
		Short: "Convert a package to a native Linux package",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ir, cleanup, err := prepareConversionWithOptions(args[0],targetFormat,allowSourceBuild,conversionOptions{LibraryDir:libraryDir,DependencyMapPath:dependencyMapPath})
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
	cmd.Flags().StringVar(&libraryDir,"library-dir","","Directory of explicitly supplied ELF libraries to isolate")
 cmd.Flags().StringVar(&dependencyMapPath,"dependency-map","","JSON map of source dependencies / so:library names to target packages")
	_ = cmd.MarkFlagRequired("to")
	return cmd
}

func newInstallCmd() *cobra.Command {
	var dryRun, allowSourceBuild bool
	var libraryDir string
 var dependencyMapPath string
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
			if dryRun {
				sourceFmt, err := detector.DetectFile(src)
				if err != nil {
					return err
				}
				fmt.Printf("[dry-run] Would convert %s (%s) to %s and request installation; no source build, packaging, or installation executed\n", src, sourceFmt, target)
				return nil
			}
			ir, cleanup, err := prepareConversionWithOptions(src,target,allowSourceBuild,conversionOptions{LibraryDir:libraryDir,DependencyMapPath:dependencyMapPath})
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
	cmd.Flags().StringVar(&libraryDir,"library-dir","","Directory of explicitly supplied ELF libraries to isolate")
 cmd.Flags().StringVar(&dependencyMapPath,"dependency-map","","JSON map of source dependencies / so:library names to target packages")
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

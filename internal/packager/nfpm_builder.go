package packager

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/goreleaser/nfpm/v2"
	_ "github.com/goreleaser/nfpm/v2/apk"
	_ "github.com/goreleaser/nfpm/v2/arch"
	_ "github.com/goreleaser/nfpm/v2/deb"
	"github.com/goreleaser/nfpm/v2/files"
	_ "github.com/goreleaser/nfpm/v2/rpm"
	"github.com/rfdetoni/mutate/internal/domain"
)

// BuildPackage generates a native package file (.deb, .rpm, .pkg.tar.zst, or .apk) from PackageIR.
func BuildPackage(ir *domain.PackageIR, targetFormat string, out io.Writer) error {
	normFormat := normalizeFormatName(targetFormat)
	pkg, err := nfpm.Get(normFormat)
	if err != nil {
		return fmt.Errorf("get packager for %s: %w", targetFormat, err)
	}

	info := &nfpm.Info{
		Name:        cleanPackageName(ir.Metadata.Name),
		Arch:        normalizeArch(ir.Metadata.Architecture, normFormat),
		Platform:    "linux",
		Version:     cleanVersion(ir.Metadata.Version),
		Release:     cleanRelease(ir.Metadata.Release),
		Section:     defaultString(ir.Metadata.Section, "utils"),
		Maintainer:  defaultString(ir.Metadata.Maintainer, "Mutate Packager <mutate@localhost>"),
		Description: defaultString(ir.Metadata.Description, "Universal Linux Transpiled Package"),
		Vendor:      defaultString(ir.Metadata.Vendor, "Mutate"),
		Homepage:    defaultString(ir.Metadata.Homepage, "https://github.com/rfdetoni/mutate"),
		License:     defaultString(ir.Metadata.License, "Unknown"),
		Overridables: nfpm.Overridables{
			Depends:    ir.Metadata.Dependencies,
			Provides:   ir.Metadata.Provides,
			Recommends: ir.Metadata.Recommends,
		},
	}

	// Setup scripts
	scriptDir := filepath.Join(ir.StagingDir, ".mutate_scripts")
	if err := os.MkdirAll(scriptDir, 0755); err != nil {
		return fmt.Errorf("create scripts dir: %w", err)
	}

	if ir.Scripts.PreInstall != "" {
		p := filepath.Join(scriptDir, "preinstall.sh")
		if err := os.WriteFile(p, []byte(ir.Scripts.PreInstall), 0755); err == nil {
			info.Scripts.PreInstall = p
		}
	}
	if ir.Scripts.PostInstall != "" {
		p := filepath.Join(scriptDir, "postinstall.sh")
		if err := os.WriteFile(p, []byte(ir.Scripts.PostInstall), 0755); err == nil {
			info.Scripts.PostInstall = p
		}
	}
	if ir.Scripts.PreRemove != "" {
		p := filepath.Join(scriptDir, "preremove.sh")
		if err := os.WriteFile(p, []byte(ir.Scripts.PreRemove), 0755); err == nil {
			info.Scripts.PreRemove = p
		}
	}
	if ir.Scripts.PostRemove != "" {
		p := filepath.Join(scriptDir, "postremove.sh")
		if err := os.WriteFile(p, []byte(ir.Scripts.PostRemove), 0755); err == nil {
			info.Scripts.PostRemove = p
		}
	}

	// Setup files content
	for destPath, entry := range ir.Files {
		if entry.IsSymlink {
			info.Contents = append(info.Contents, &files.Content{
				Source:      entry.SymlinkTarget,
				Destination: destPath,
				Type:        "symlink",
			})
		} else if entry.IsDir {
			info.Contents = append(info.Contents, &files.Content{
				Destination: destPath,
				Type:        "dir",
			})
		} else {
			src := entry.SourcePath
			if src == "" && ir.StagingDir != "" {
				src = filepath.Join(ir.StagingDir, strings.TrimPrefix(destPath, "/"))
			}
			info.Contents = append(info.Contents, &files.Content{
				Source:      src,
				Destination: destPath,
			})
		}
	}

	info = nfpm.WithDefaults(info)

	if err := pkg.Package(info, out); err != nil {
		return fmt.Errorf("package generation failed for %s: %w", normFormat, err)
	}

	return nil
}

// BuildPackageToFile builds package directly into destination file on disk.
func BuildPackageToFile(ir *domain.PackageIR, targetFormat, outputPath string) error {
	dir := filepath.Dir(outputPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	f, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("create output file %s: %w", outputPath, err)
	}
	defer f.Close()

	return BuildPackage(ir, targetFormat, f)
}

func normalizeFormatName(fmtStr string) string {
	s := strings.ToLower(strings.TrimPrefix(fmtStr, "."))
	switch s {
	case "deb":
		return "deb"
	case "rpm":
		return "rpm"
	case "apk", "alpine":
		return "apk"
	case "arch", "archlinux", "pkg.tar.zst":
		return "archlinux"
	default:
		return s
	}
}

func normalizeArch(arch string, format string) string {
	a := strings.ToLower(arch)
	switch format {
	case "deb":
		if a == "x86_64" {
			return "amd64"
		} else if a == "aarch64" {
			return "arm64"
		}
	case "rpm", "archlinux":
		if a == "amd64" {
			return "x86_64"
		} else if a == "arm64" {
			return "aarch64"
		}
	}
	if a == "" {
		return "all"
	}
	return a
}

func cleanPackageName(name string) string {
	if name == "" {
		return "mutated-package"
	}
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, "_", "-")
	return name
}

func cleanVersion(v string) string {
	if v == "" {
		return "1.0.0"
	}
	// Remove epoch prefix e.g. 1:2.0 -> 2.0
	if idx := strings.Index(v, ":"); idx >= 0 {
		v = v[idx+1:]
	}
	return v
}

func cleanRelease(r string) string {
	if r == "" {
		return "1"
	}
	return r
}

func defaultString(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

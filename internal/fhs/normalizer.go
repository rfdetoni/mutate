package fhs

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rfdetoni/mutate/internal/domain"
	"github.com/rfdetoni/mutate/internal/resolver"
)

// TargetFHS defines library layout for the target distribution.
type TargetFHS struct {
	PrimaryLibDir string // e.g. /usr/lib/x86_64-linux-gnu, /usr/lib64, or /usr/lib
}

// GetTargetFHS returns FHS configuration for target distro and architecture.
func GetTargetFHS(distro resolver.Distro, arch string) TargetFHS {
	switch distro {
	case resolver.DistroDebian:
		if arch == "x86_64" || arch == "amd64" {
			return TargetFHS{PrimaryLibDir: "/usr/lib/x86_64-linux-gnu"}
		} else if arch == "aarch64" || arch == "arm64" {
			return TargetFHS{PrimaryLibDir: "/usr/lib/aarch64-linux-gnu"}
		}
		return TargetFHS{PrimaryLibDir: "/usr/lib"}
	case resolver.DistroFedora:
		if arch == "x86_64" || arch == "amd64" || arch == "aarch64" || arch == "arm64" {
			return TargetFHS{PrimaryLibDir: "/usr/lib64"}
		}
		return TargetFHS{PrimaryLibDir: "/usr/lib"}
	case resolver.DistroArch, resolver.DistroAlpine:
		return TargetFHS{PrimaryLibDir: "/usr/lib"}
	default:
		return TargetFHS{PrimaryLibDir: "/usr/lib"}
	}
}

// NormalizePackage adjusts library paths in PackageIR and replaces paths in text configuration files.
func NormalizePackage(ir *domain.PackageIR, targetDistro resolver.Distro) error {
	targetFHS := GetTargetFHS(targetDistro, ir.Metadata.Architecture)
	pathReplacements := make(map[string]string)

	// Identify lib relocations
	newFiles := make(map[string]domain.FileEntry)
	for destPath, entry := range ir.Files {
		newDest := destPath

		// Match source lib paths
		if strings.HasPrefix(destPath, "/usr/lib64/") && targetFHS.PrimaryLibDir != "/usr/lib64" {
			rel := strings.TrimPrefix(destPath, "/usr/lib64/")
			newDest = filepath.Join(targetFHS.PrimaryLibDir, rel)
		} else if strings.HasPrefix(destPath, "/usr/lib/x86_64-linux-gnu/") && targetFHS.PrimaryLibDir != "/usr/lib/x86_64-linux-gnu" {
			rel := strings.TrimPrefix(destPath, "/usr/lib/x86_64-linux-gnu/")
			newDest = filepath.Join(targetFHS.PrimaryLibDir, rel)
		} else if strings.HasPrefix(destPath, "/usr/lib/aarch64-linux-gnu/") && targetFHS.PrimaryLibDir != "/usr/lib/aarch64-linux-gnu" {
			rel := strings.TrimPrefix(destPath, "/usr/lib/aarch64-linux-gnu/")
			newDest = filepath.Join(targetFHS.PrimaryLibDir, rel)
		}

		if newDest != destPath {
			pathReplacements[destPath] = newDest
			// If moving from /usr/lib64 to another lib dir, record directory replacement
			if strings.HasPrefix(destPath, "/usr/lib64/") {
				pathReplacements["/usr/lib64/"] = targetFHS.PrimaryLibDir + "/"
			} else if strings.HasPrefix(destPath, "/usr/lib/x86_64-linux-gnu/") {
				pathReplacements["/usr/lib/x86_64-linux-gnu/"] = targetFHS.PrimaryLibDir + "/"
			}
			// If file is on disk in staging, relocate it
			if ir.StagingDir != "" && entry.SourcePath != "" {
				newDiskPath := filepath.Join(ir.StagingDir, strings.TrimPrefix(newDest, "/"))
				if err := os.MkdirAll(filepath.Dir(newDiskPath), 0755); err == nil {
					if err := os.Rename(entry.SourcePath, newDiskPath); err == nil {
						entry.SourcePath = newDiskPath
					}
				}
			}
			entry.DestinationPath = newDest
		}

		newFiles[newDest] = entry
	}
	ir.Files = newFiles

	// Update text configuration files: .desktop, .service, .pc
	if len(pathReplacements) > 0 {
		for destPath, entry := range ir.Files {
			if entry.IsDir || entry.IsSymlink {
				continue
			}

			if isTextConfig(destPath) {
				diskPath := entry.SourcePath
				if diskPath == "" && ir.StagingDir != "" {
					diskPath = filepath.Join(ir.StagingDir, strings.TrimPrefix(destPath, "/"))
				}
				if err := updateConfigFilePaths(diskPath, pathReplacements); err != nil {
					return fmt.Errorf("update config %s: %w", destPath, err)
				}
			}
		}
	}

	return nil
}

func isTextConfig(path string) bool {
	return strings.HasSuffix(path, ".desktop") ||
		strings.HasSuffix(path, ".service") ||
		strings.HasSuffix(path, ".pc")
}

func updateConfigFilePaths(diskPath string, replacements map[string]string) error {
	data, err := os.ReadFile(diskPath)
	if err != nil {
		return err
	}

	modified := false
	for oldPath, newPath := range replacements {
		if bytes.Contains(data, []byte(oldPath)) {
			data = bytes.ReplaceAll(data, []byte(oldPath), []byte(newPath))
			modified = true
		}
	}

	if modified {
		return os.WriteFile(diskPath, data, 0644)
	}
	return nil
}

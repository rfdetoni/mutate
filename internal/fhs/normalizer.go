package fhs

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/rfdetoni/mutate/internal/domain"
	"github.com/rfdetoni/mutate/internal/resolver"
)

// TargetFHS defines the library layout of a target distro.
type TargetFHS struct {
	PrimaryLibDir string
}

func GetTargetFHS(distro resolver.Distro, arch string) TargetFHS {
	switch distro {
	case resolver.DistroDebian:
		switch arch {
		case "x86_64", "amd64":
			return TargetFHS{PrimaryLibDir: "/usr/lib/x86_64-linux-gnu"}
		case "aarch64", "arm64":
			return TargetFHS{PrimaryLibDir: "/usr/lib/aarch64-linux-gnu"}
		}
	case resolver.DistroFedora:
		if arch == "x86_64" || arch == "amd64" || arch == "aarch64" || arch == "arm64" {
			return TargetFHS{PrimaryLibDir: "/usr/lib64"}
		}
	}
	return TargetFHS{PrimaryLibDir: "/usr/lib"}
}

func relocatedLibraryPath(dest, libDir string) string {
	for _, prefix := range []string{
		"/usr/lib64/", "/usr/lib/x86_64-linux-gnu/", "/usr/lib/aarch64-linux-gnu/",
	} {
		if strings.HasPrefix(dest, prefix) && libDir+"/" != prefix {
			return libDir + "/" + strings.TrimPrefix(dest, prefix)
		}
	}
	return dest
}

// insideStaging distinguishes owned temporary payloads from immutable user
// inputs (AppImage/APK or explicitly supplied library and configuration files).
func insideStaging(stage, path string) bool {
	if stage == "" || path == "" {
		return false
	}
	root, err := filepath.Abs(stage)
	if err != nil {
		return false
	}
	file, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(root, file)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func stageSource(ir *domain.PackageIR, entry domain.FileEntry, dest string) string {
	if entry.SourcePath != "" {
		return entry.SourcePath
	}
	if ir.StagingDir == "" {
		return ""
	}
	return filepath.Join(ir.StagingDir, strings.TrimPrefix(dest, "/"))
}

// copyIntoStage implements copy-on-write for inputs outside staging.
// Only the private copy may be relocated or rewritten by the normalizer.
func copyIntoStage(source, target string) error {
	st, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() {
		return fmt.Errorf("input is not a regular file: %q", source)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return err
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(target)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(target)
		return err
	}
	if err := os.Chmod(target, st.Mode().Perm()); err != nil {
		return err
	}
	return nil
}

func writeOwnedConfig(source string, content []byte) error {
	st, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() {
		return fmt.Errorf("configuration is not a regular file: %q", source)
	}
	tmp, err := os.CreateTemp(filepath.Dir(source), ".mutate-config-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(st.Mode().Perm()); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), source)
}

func updateConfigFilePaths(ir *domain.PackageIR, entry domain.FileEntry, dest string, replacements map[string]string) (domain.FileEntry, error) {
	source := stageSource(ir, entry, dest)
	if source == "" {
		return entry, fmt.Errorf("missing source for configuration %q", dest)
	}
	original, err := os.ReadFile(source)
	if err != nil {
		return entry, err
	}
	keys := make([]string, 0, len(replacements))
	for key := range replacements {
		keys = append(keys, key)
	}
	// Longer paths take precedence over directory prefixes; order is stable.
	sort.Slice(keys, func(i, j int) bool {
		if len(keys[i]) != len(keys[j]) {
			return len(keys[i]) > len(keys[j])
		}
		return keys[i] < keys[j]
	})
	updated := original
	for _, key := range keys {
		updated = bytes.ReplaceAll(updated, []byte(key), []byte(replacements[key]))
	}
	if bytes.Equal(updated, original) {
		return entry, nil
	}
	if !insideStaging(ir.StagingDir, source) {
		if ir.StagingDir == "" {
			return entry, fmt.Errorf("cannot stage external configuration without StagingDir")
		}
		target := filepath.Join(ir.StagingDir, ".mutate-cow", strings.TrimPrefix(dest, "/"))
		if err := copyIntoStage(source, target); err != nil {
			return entry, fmt.Errorf("copy configuration %q: %w", dest, err)
		}
		source = target
	}
	if err := writeOwnedConfig(source, updated); err != nil {
		return entry, err
	}
	entry.SourcePath = source
	return entry, nil
}

func isTextConfig(path string) bool {
	return strings.HasSuffix(path, ".desktop") ||
		strings.HasSuffix(path, ".service") ||
		strings.HasSuffix(path, ".pc")
}

// NormalizePackage relocates destination paths without moving payload bytes.
// nFPM already reads each FileEntry.SourcePath, so copying/renaming large
// libraries solely to change their install destination wastes disk I/O.
// Only text configuration files that reference changed paths are rewritten
// using staging-owned copies when necessary.
func NormalizePackage(ir *domain.PackageIR, targetDistro resolver.Distro) error {
	if err := domain.ValidatePackageIR(ir); err != nil {
		return fmt.Errorf("validate before FHS normalization: %w", err)
	}

	libDir := GetTargetFHS(targetDistro, ir.Metadata.Architecture).PrimaryLibDir
	// Most native/portable packages need no relocation. Retain their
	// validated representation without allocating a second file map.
	hasRelocation := false
	for dest := range ir.Files {
		if relocatedLibraryPath(dest, libDir) != dest {
			hasRelocation = true
			break
		}
	}
	if !hasRelocation {
		return nil
	}

	newFiles := make(map[string]domain.FileEntry, len(ir.Files))
	replacements := make(map[string]string)
	for dest, entry := range ir.Files {
		newDest := relocatedLibraryPath(dest, libDir)
		if _, exists := newFiles[newDest]; exists {
			return fmt.Errorf("FHS normalization collision at %s", newDest)
		}
		if newDest != dest {
			if !entry.IsSymlink && !entry.IsDir {
				source := stageSource(ir, entry, dest)
				if source == "" {
					return fmt.Errorf("file %q has no source", dest)
				}
				// Check the source before changing the package representation.
				// Never follow a symlink when relocating regular files.
				st, err := os.Lstat(source)
				if err != nil {
					return fmt.Errorf("stat FHS source %q: %w", dest, err)
				}
				if !st.Mode().IsRegular() {
					return fmt.Errorf("FHS source is not a regular file: %q", source)
				}
				// An implicit staging source must become explicit because the
				// staging pathname itself does not change when its destination does.
				entry.SourcePath = source
			}
			entry.DestinationPath = newDest
			replacements[dest] = newDest
			for _, prefix := range []string{
				"/usr/lib64/", "/usr/lib/x86_64-linux-gnu/", "/usr/lib/aarch64-linux-gnu/",
			} {
				if strings.HasPrefix(dest, prefix) {
					replacements[prefix] = libDir + "/"
					break
				}
			}
		}
		newFiles[newDest] = entry
	}

	// Rewriting is the only operation that needs a private physical copy.
	// Keys are sorted to ensure deterministic output for a given package.
	dests := make([]string, 0, len(newFiles))
	for dest := range newFiles {
		dests = append(dests, dest)
	}
	sort.Strings(dests)
	for _, dest := range dests {
		entry := newFiles[dest]
		if entry.IsDir || entry.IsSymlink || !isTextConfig(dest) {
			continue
		}
		updated, err := updateConfigFilePaths(ir, entry, dest, replacements)
		if err != nil {
			return fmt.Errorf("update config %s: %w", dest, err)
		}
		newFiles[dest] = updated
	}
	ir.Files = newFiles
	return nil
}

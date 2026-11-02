package resolver

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/rfdetoni/mutate/internal/domain"
	"github.com/rfdetoni/mutate/internal/elfanalyzer"
)

// LibraryBundler bundles explicitly supplied ELF shared objects.
// It never claims to download unavailable libraries automatically.
type LibraryBundler struct{ AppName string }

func NewLibraryBundler(name string) *LibraryBundler { return &LibraryBundler{AppName: name} }

// BundleMissingLibraries fails closed without an explicit trusted source.
func (b *LibraryBundler) BundleMissingLibraries(ir *domain.PackageIR, missing []string, host Distro) error {
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("libraries %v are unavailable; provide --library-dir with matching ELF shared objects", missing)
}

func (b *LibraryBundler) BundleFromDir(ir *domain.PackageIR, missing []string, sourceDir string) error {
	if len(missing) == 0 {
		return nil
	}
	if sourceDir == "" {
		return fmt.Errorf("missing --library-dir")
	}
	if b.AppName == "" || strings.ContainsAny(b.AppName, "/\\\x00\n\r\t\"' ;$`") {
		return fmt.Errorf("invalid app name")
	}
	libDir := "/usr/lib/" + b.AppName + "/lib"
	binDir := "/usr/lib/" + b.AppName + "/bin"

	// Verify all libraries before mutating the package.
	type provided struct {
		name, source string
		mode         os.FileMode
	}
	var libs []provided
	for _, lib := range missing {
		if lib == "" || filepath.Base(lib) != lib || strings.Contains(lib, "\\") {
			return fmt.Errorf("invalid library name %q", lib)
		}
		path := filepath.Join(sourceDir, lib)
		st, err := os.Lstat(path)
		if err != nil {
			return fmt.Errorf("missing library %s: %w", lib, err)
		}
		if !st.Mode().IsRegular() {
			return fmt.Errorf("library must be a regular file: %q", path)
		}
		info, err := elfanalyzer.AnalyzeELF(path)
		if err != nil {
			return fmt.Errorf("invalid ELF library %s: %w", lib, err)
		}
		if info.Soname != "" && info.Soname != lib {
			return fmt.Errorf("SONAME %s does not satisfy %s", info.Soname, lib)
		}
		for _, entry := range ir.Files {
			if entry.Elf != nil && entry.Elf.Architecture != info.Architecture {
				return fmt.Errorf("architecture mismatch for %s: %s vs %s", lib, info.Architecture, entry.Elf.Architecture)
			}
		}
		libs = append(libs, provided{lib, path, st.Mode().Perm()})
	}
	// Only real ELF executables should receive LD_LIBRARY_PATH wrappers.
	var bins []string
	seen := make(map[string]bool)
	for path, entry := range ir.Files {
		if (strings.HasPrefix(path, "/bin/") || strings.HasPrefix(path, "/usr/bin/")) && entry.Elf != nil && !entry.IsSymlink {
			basename := filepath.Base(path)
			if seen[basename] {
				return fmt.Errorf("duplicate executable name %q", basename)
			}
			seen[basename] = true
			if entry.Mode&os.ModeSetuid != 0 || entry.Mode&os.ModeSetgid != 0 {
				return fmt.Errorf("cannot wrap privileged executable %s", path)
			}
			bins = append(bins, path)
		}
	}
	if len(bins) == 0 {
		return fmt.Errorf("no ELF executable to wrap for isolated libraries")
	}
	if err := os.MkdirAll(filepath.Join(ir.StagingDir, strings.TrimPrefix(libDir, "/")), 0755); err != nil {
		return err
	}
	for _, lib := range libs {
		dest := libDir + "/" + lib.name
		disk := filepath.Join(ir.StagingDir, strings.TrimPrefix(dest, "/"))
		in, err := os.Open(lib.source)
		if err != nil {
			return err
		}
		out, err := os.OpenFile(disk, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			in.Close()
			return err
		}
		_, cpErr := io.Copy(out, in)
		cErr := out.Close()
		in.Close()
		if cpErr != nil {
			return cpErr
		}
		if cErr != nil {
			return cErr
		}
		if err := os.Chmod(disk, lib.mode); err != nil {
			return err
		}
		ir.Files[dest] = domain.FileEntry{SourcePath: disk, DestinationPath: dest, Mode: lib.mode}
	}
	for _, path := range bins {
		orig := ir.Files[path]
		binDest := binDir + "/" + filepath.Base(path)
		disk := filepath.Join(ir.StagingDir, strings.TrimPrefix(binDest, "/"))
		if err := os.MkdirAll(filepath.Dir(disk), 0755); err != nil {
			return err
		}
		if err := os.Rename(orig.SourcePath, disk); err != nil {
			return fmt.Errorf("move executable %s: %w", path, err)
		}
		orig.SourcePath = disk
		orig.DestinationPath = binDest
		delete(ir.Files, path)
		ir.Files[binDest] = orig
		wrapper := elfanalyzer.GenerateWrapperScriptContent(elfanalyzer.WrapperOptions{AppName: b.AppName, OriginalBin: binDest, IsolatedLibDir: libDir})
		wrapperDisk := filepath.Join(ir.StagingDir, strings.TrimPrefix(path, "/"))
		if err := os.WriteFile(wrapperDisk, []byte(wrapper), 0755); err != nil {
			return err
		}
		ir.Files[path] = domain.FileEntry{SourcePath: wrapperDisk, DestinationPath: path, Mode: 0755}
	}
	return nil
}

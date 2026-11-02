package resolver

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/rfdetoni/mutate/internal/domain"
	"github.com/rfdetoni/mutate/internal/elfanalyzer"
)

// LibraryBundler handles isolation and bundling of libraries not available on host system.
type LibraryBundler struct {
	AppName string
}

func NewLibraryBundler(appName string) *LibraryBundler {
	return &LibraryBundler{
		AppName: appName,
	}
}

// BundleMissingLibraries isolates missing .so files into /usr/lib/<app-name>/lib and creates execution wrapper.
func (b *LibraryBundler) BundleMissingLibraries(ir *domain.PackageIR, missingLibs []string, hostDistro Distro) error {
	if len(missingLibs) == 0 {
		return nil
	}

	appLibDir := fmt.Sprintf("/usr/lib/%s/lib", b.AppName)
	stagingLibDir := filepath.Join(ir.StagingDir, appLibDir)
	if err := os.MkdirAll(stagingLibDir, 0755); err != nil {
		return fmt.Errorf("create bundle dir %s: %w", stagingLibDir, err)
	}

	// For each primary executable in /usr/bin or /bin, move real binary to /usr/lib/<app>/bin and place wrapper in /usr/bin
	executables := b.findExecutables(ir)

	for _, binPath := range executables {
		relBin := strings.TrimPrefix(binPath, "/")
		srcOnDisk := filepath.Join(ir.StagingDir, relBin)

		internalAppBin := fmt.Sprintf("/usr/lib/%s/bin/%s", b.AppName, filepath.Base(binPath))
		internalDiskPath := filepath.Join(ir.StagingDir, strings.TrimPrefix(internalAppBin, "/"))

		if err := os.MkdirAll(filepath.Dir(internalDiskPath), 0755); err != nil {
			return err
		}

		// Move real binary to internal location
		if err := os.Rename(srcOnDisk, internalDiskPath); err != nil {
			// If rename fails (e.g. cross-device or file entry missing on disk), copy
			if err := copyFile(srcOnDisk, internalDiskPath); err != nil {
				return fmt.Errorf("move executable %s to %s: %w", srcOnDisk, internalDiskPath, err)
			}
			os.Remove(srcOnDisk)
		}

		// Update IR files
		entry := ir.Files[binPath]
		entry.SourcePath = internalDiskPath
		entry.DestinationPath = internalAppBin
		ir.Files[internalAppBin] = entry

		// Generate wrapper script at original binPath
		wrapperOpts := elfanalyzer.WrapperOptions{
			AppName:        b.AppName,
			OriginalBin:    internalAppBin,
			IsolatedLibDir: appLibDir,
		}

		// Check if musl is needed on glibc host
		if entry.Elf != nil && entry.Elf.NeedsMusl && hostDistro != DistroAlpine {
			wrapperOpts.UseMuslLoader = true
			wrapperOpts.MuslLoaderPath = "/lib/ld-musl-x86_64.so.1"
			// Ensure musl dependency is present
			b.ensureMuslDependency(ir, hostDistro)
		}

		wrapperContent := elfanalyzer.GenerateWrapperScriptContent(wrapperOpts)
		if err := os.WriteFile(srcOnDisk, []byte(wrapperContent), 0755); err != nil {
			return fmt.Errorf("write wrapper script %s: %w", srcOnDisk, err)
		}

		ir.Files[binPath] = domain.FileEntry{
			SourcePath:      srcOnDisk,
			DestinationPath: binPath,
			Mode:            0755,
		}
	}

	return nil
}

func (b *LibraryBundler) findExecutables(ir *domain.PackageIR) []string {
	var execs []string
	for destPath, entry := range ir.Files {
		if strings.HasPrefix(destPath, "/usr/bin/") || strings.HasPrefix(destPath, "/bin/") {
			if !entry.IsDir && !entry.IsSymlink {
				execs = append(execs, destPath)
			}
		}
	}
	return execs
}

func (b *LibraryBundler) ensureMuslDependency(ir *domain.PackageIR, hostDistro Distro) {
	muslPkg := "musl"
	switch hostDistro {
	case DistroDebian:
		muslPkg = "musl"
	case DistroArch:
		muslPkg = "musl"
	case DistroFedora:
		muslPkg = "musl-libc"
	}

	for _, dep := range ir.Metadata.Dependencies {
		if dep == muslPkg {
			return
		}
	}
	ir.Metadata.Dependencies = append(ir.Metadata.Dependencies, muslPkg)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

// DownloadLibraryOptional allows fetching an isolated .so if an explicit remote URL is known.
func DownloadLibraryOptional(targetPath, downloadURL string) error {
	resp, err := http.Get(downloadURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad status downloading lib: %d", resp.StatusCode)
	}

	if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
		return err
	}

	out, err := os.Create(targetPath)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

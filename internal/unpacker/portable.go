package unpacker

import (
	"debug/elf"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/rfdetoni/mutate/internal/detector"
	"github.com/rfdetoni/mutate/internal/domain"
)

// PortablePreserver keeps a self-contained artifact intact in the native
// package rather than unpacking and reconstructing its internal files.
type PortablePreserver interface {
	Preserve(srcPath, stagingDir string) (*domain.PackageIR, error)
}

var portableNameChars = regexp.MustCompile(`[^a-zA-Z0-9+.-]`)

func appImageName(src string) (string, error) {
	filename := filepath.Base(src)
	base := filename
	if strings.EqualFold(filepath.Ext(filename), ".AppImage") {
		base = strings.TrimSuffix(filename, filepath.Ext(filename))
	}
	base = strings.ToLower(portableNameChars.ReplaceAllString(base, "-"))
	base = strings.Trim(base, "-.")
	if len(base) > 64 {
		base = strings.TrimRight(base[:64], "-.")
	}
	if base == "" || base == "." || base == ".." {
		return "", fmt.Errorf("invalid AppImage filename %q", filename)
	}
	return base, nil
}

// Preserve stores only the original AppImage and a freedesktop launcher.
// nFPM reads SourcePath directly: no redundant multi-gigabyte staging copy,
// ISO/SquashFS extraction, executable inspection or host execution occurs.
func (u *AppImageUnpacker) Preserve(srcPath, stagingDir string) (*domain.PackageIR, error) {
	st, err := os.Lstat(srcPath)
	if err != nil {
		return nil, fmt.Errorf("stat AppImage: %w", err)
	}
	if !st.Mode().IsRegular() {
		return nil, fmt.Errorf("AppImage must be a regular file, not a symlink or special file")
	}
	format, err := detector.DetectFile(srcPath)
	if err != nil {
		return nil, err
	}
	if format != domain.FormatAppImage {
		return nil, fmt.Errorf("file is not an AppImage")
	}
	name, err := appImageName(srcPath)
	if err != nil {
		return nil, err
	}
	f, err := elf.Open(srcPath)
	if err != nil {
		return nil, fmt.Errorf("read AppImage ELF header: %w", err)
	}
	var arch string
	switch f.Machine {
	case elf.EM_X86_64:
		arch = "amd64"
	case elf.EM_AARCH64:
		arch = "arm64"
	default:
		err = fmt.Errorf("unsupported AppImage architecture %s", f.Machine)
	}
	_ = f.Close()
	if err != nil {
		return nil, err
	}
	absolute, err := filepath.Abs(srcPath)
	if err != nil {
		return nil, err
	}
	ir := domain.NewPackageIR(stagingDir, domain.FormatAppImage)
	ir.Metadata = domain.Metadata{
		Name: name, Version: "1.0.0", Release: "1", Architecture: arch,
		Description: "Original AppImage preserved without extraction (runtime requirements unchanged)",
		License:     "Unknown",
	}
	appDest := "/opt/mutate/appimages/" + name + "/" + name + ".AppImage"
	ir.Files[appDest] = domain.FileEntry{
		SourcePath: absolute, DestinationPath: appDest, Mode: 0755, ModTime: st.ModTime(),
	}
	// A themed icon is used by default. A custom AppImage icon requires
	// optional filesystem inspection and must never execute the runtime.
	desktopDest := "/usr/share/applications/" + name + ".desktop"
	desktopDisk := filepath.Join(stagingDir, "usr/share/applications", name+".desktop")
	if err := os.MkdirAll(filepath.Dir(desktopDisk), 0755); err != nil {
		return nil, err
	}
	desktop := fmt.Sprintf("[Desktop Entry]\nType=Application\nName=%s\nExec=%s %%U\nIcon=application-x-executable\nTerminal=false\nCategories=Utility;\n", name, appDest)
	if err := os.WriteFile(desktopDisk, []byte(desktop), 0644); err != nil {
		return nil, fmt.Errorf("create desktop launcher: %w", err)
	}
	ir.Files[desktopDest] = domain.FileEntry{SourcePath: desktopDisk, DestinationPath: desktopDest, Mode: 0644}
	return ir, nil
}

// Android APKs already carry their own complete payload and are installed
// intact for Waydroid; no repackaging of APK resources is needed.
func (u *AndroidUnpacker) Preserve(srcPath, stagingDir string) (*domain.PackageIR, error) {
	return u.Unpack(srcPath, stagingDir)
}

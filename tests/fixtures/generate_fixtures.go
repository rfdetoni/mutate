package main

import (
	"archive/tar"
	"compress/gzip"
	_ "embed"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"

	"github.com/rfdetoni/mutate/internal/domain"
	"github.com/rfdetoni/mutate/internal/packager"
)

//go:embed sample-android.apk
var androidAPKFixture []byte

func main() {
	outDir := "tests/fixtures"
	if len(os.Args) > 1 {
		outDir = os.Args[1]
	}

	if err := os.MkdirAll(outDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating output dir: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Generating test fixtures in %s...\n", outDir)

	if err := generateNativePackages(outDir); err != nil {
		fmt.Fprintf(os.Stderr, "Error generating native packages: %v\n", err)
		os.Exit(1)
	}

	if err := generateSourceTarball(filepath.Join(outDir, "sample-source.tar.gz")); err != nil {
		fmt.Fprintf(os.Stderr, "Error generating source tarball: %v\n", err)
		os.Exit(1)
	}

	if err := generateAppImage(filepath.Join(outDir, "sample.AppImage")); err != nil {
		fmt.Fprintf(os.Stderr, "Error generating AppImage: %v\n", err)
		os.Exit(1)
	}

	if err := generateAndroidApk(filepath.Join(outDir, "sample-android.apk")); err != nil {
		fmt.Fprintf(os.Stderr, "Error generating Android APK: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("All 7 package fixtures generated successfully!")
}

func generateNativePackages(outDir string) error {
	tmpDir, err := os.MkdirTemp("", "mutate-fixtures-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	binDir := filepath.Join(tmpDir, "usr/bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		return err
	}

	binFile := filepath.Join(binDir, "sample-app")
	script := "#!/bin/sh\necho 'M.U.T.A.T.E. sample package payload'\n"
	if err := os.WriteFile(binFile, []byte(script), 0755); err != nil {
		return err
	}

	desktopDir := filepath.Join(tmpDir, "usr/share/applications")
	if err := os.MkdirAll(desktopDir, 0755); err != nil {
		return err
	}
	desktopFile := filepath.Join(desktopDir, "sample-app.desktop")
	desktop := "[Desktop Entry]\nType=Application\nName=SampleApp\nExec=/usr/bin/sample-app\n"
	if err := os.WriteFile(desktopFile, []byte(desktop), 0644); err != nil {
		return err
	}

	ir := domain.NewPackageIR(tmpDir, domain.FormatDEB)
	ir.Metadata = domain.Metadata{
		Name:         "sample-app",
		Version:      "1.0.0",
		Release:      "1",
		Architecture: "amd64",
		Description:  "Multi-format sample package for M.U.T.A.T.E. test suite",
		License:      "MIT",
	}
	ir.Files["/usr/bin/sample-app"] = domain.FileEntry{
		SourcePath:      binFile,
		DestinationPath: "/usr/bin/sample-app",
		Mode:            0755,
	}
	ir.Files["/usr/share/applications/sample-app.desktop"] = domain.FileEntry{
		SourcePath:      desktopFile,
		DestinationPath: "/usr/share/applications/sample-app.desktop",
		Mode:            0644,
	}

	// 1. DEB
	if err := packager.BuildPackageToFile(ir, "deb", filepath.Join(outDir, "sample.deb")); err != nil {
		return fmt.Errorf("build deb: %w", err)
	}

	// 2. RPM
	if err := packager.BuildPackageToFile(ir, "rpm", filepath.Join(outDir, "sample.rpm")); err != nil {
		return fmt.Errorf("build rpm: %w", err)
	}

	// 3. Arch (.pkg.tar.zst)
	if err := packager.BuildPackageToFile(ir, "arch", filepath.Join(outDir, "sample.pkg.tar.zst")); err != nil {
		return fmt.Errorf("build arch: %w", err)
	}

	// 4. Alpine (.apk)
	if err := packager.BuildPackageToFile(ir, "apk", filepath.Join(outDir, "sample-alpine.apk")); err != nil {
		return fmt.Errorf("build alpine apk: %w", err)
	}

	return nil
}

func generateSourceTarball(outPath string) error {
	f, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer f.Close()

	gzw := gzip.NewWriter(f)
	defer gzw.Close()
	tw := tar.NewWriter(gzw)
	defer tw.Close()

	files := map[string]string{
		"sample-src/Makefile":    "all:\n\t@echo 'Built'\ninstall:\n\tmkdir -p $(DESTDIR)/usr/bin\n\tcp sample-tool $(DESTDIR)/usr/bin/\n",
		"sample-src/sample-tool": "#!/bin/sh\necho 'Source built tool'\n",
	}

	for name, content := range files {
		mode := int64(0644)
		if filepath.Ext(name) == "" {
			mode = 0755
		}
		hdr := &tar.Header{
			Name: name,
			Mode: mode,
			Size: int64(len(content)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			return err
		}
	}

	return nil
}

func generateAppImage(outPath string) error {
	b := make([]byte, 8192)
	// ELF Header
	copy(b, []byte{0x7f, 'E', 'L', 'F'})
	b[4] = 2 // 64-bit
	b[5] = 1 // Little endian
	b[6] = 1 // ELF version
	// AppImage Magic Signature at offset 8: 'A' 'I' 0x02
	copy(b[8:11], []byte{'A', 'I', 2})
	binary.LittleEndian.PutUint16(b[16:18], 2)  // ET_EXEC
	binary.LittleEndian.PutUint16(b[18:20], 62) // x86-64 machine
	binary.LittleEndian.PutUint32(b[20:24], 1)  // version
	binary.LittleEndian.PutUint16(b[52:54], 64) // e_ehsize

	return os.WriteFile(outPath, b, 0755)
}

// Reuse the checked-in Android fixture instead of relying on a specific
// developer's Go module cache path. The generator is reproducible on CI.
func generateAndroidApk(outPath string) error {
	if len(androidAPKFixture) == 0 {
		return fmt.Errorf("embedded Android fixture is empty")
	}
	return os.WriteFile(outPath, androidAPKFixture, 0644)
}

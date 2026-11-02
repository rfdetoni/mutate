package packager

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/rfdetoni/mutate/internal/domain"
)

func TestBuildPackageDebAndRpm(t *testing.T) {
	stagingDir, err := os.MkdirTemp("", "staging-packager-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(stagingDir)

	binDir := filepath.Join(stagingDir, "usr/bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	binFile := filepath.Join(binDir, "hello")
	if err := os.WriteFile(binFile, []byte("#!/bin/sh\necho hello\n"), 0755); err != nil {
		t.Fatal(err)
	}

	ir := domain.NewPackageIR(stagingDir, domain.FormatRPM)
	ir.Metadata = domain.Metadata{
		Name:         "hello-test",
		Version:      "1.0.0",
		Release:      "1",
		Architecture: "amd64",
		Description:  "A test package",
		License:      "MIT",
	}
	ir.Files["/usr/bin/hello"] = domain.FileEntry{
		SourcePath:      binFile,
		DestinationPath: "/usr/bin/hello",
		Mode:            0755,
	}

	// 1. Build DEB
	var debBuf bytes.Buffer
	if err := BuildPackage(ir, "deb", &debBuf); err != nil {
		t.Fatalf("BuildPackage deb failed: %v", err)
	}
	if debBuf.Len() == 0 {
		t.Fatal("expected non-empty deb package buffer")
	}

	// 2. Build RPM
	var rpmBuf bytes.Buffer
	if err := BuildPackage(ir, "rpm", &rpmBuf); err != nil {
		t.Fatalf("BuildPackage rpm failed: %v", err)
	}
	if rpmBuf.Len() == 0 {
		t.Fatal("expected non-empty rpm package buffer")
	}

	// 3. Build Arch (.pkg.tar.zst)
	var archBuf bytes.Buffer
	if err := BuildPackage(ir, "arch", &archBuf); err != nil {
		t.Fatalf("BuildPackage arch failed: %v", err)
	}
	if archBuf.Len() == 0 {
		t.Fatal("expected non-empty arch package buffer")
	}

	// 4. Build APK
	var apkBuf bytes.Buffer
	if err := BuildPackage(ir, "apk", &apkBuf); err != nil {
		t.Fatalf("BuildPackage apk failed: %v", err)
	}
	if apkBuf.Len() == 0 {
		t.Fatal("expected non-empty apk package buffer")
	}
}

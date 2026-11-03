package packager

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/rfdetoni/mutate/internal/domain"
	"github.com/rfdetoni/mutate/internal/fhs"
	"github.com/rfdetoni/mutate/internal/resolver"
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

func TestRelocatedLibraryIsPackagedFromOriginalSource(t *testing.T) {
	stage := t.TempDir()
	external := filepath.Join(t.TempDir(), "libsample.so.1")
	if err := os.WriteFile(external, []byte("unmodified library bytes"), 0644); err != nil {
		t.Fatal(err)
	}
	ir := domain.NewPackageIR(stage, domain.FormatRPM)
	ir.Metadata = domain.Metadata{
		Name: "relocation-test", Version: "1.0.0", Release: "1",
		Architecture: "amd64", Description: "Relocated without copying", License: "MIT",
	}
	ir.Files["/usr/lib64/libsample.so.1"] = domain.FileEntry{
		SourcePath: external, DestinationPath: "/usr/lib64/libsample.so.1", Mode: 0644,
	}
	if err := fhs.NormalizePackage(ir, resolver.DistroDebian); err != nil {
		t.Fatal(err)
	}
	dest := "/usr/lib/x86_64-linux-gnu/libsample.so.1"
	if ir.Files[dest].SourcePath != external {
		t.Fatal("normalization copied or moved the external library")
	}
	var output bytes.Buffer
	if err := BuildPackage(ir, "deb", &output); err != nil {
		t.Fatalf("native packaging failed using original payload: %v", err)
	}
	if output.Len() == 0 {
		t.Fatal("missing package output")
	}
	original, err := os.ReadFile(external)
	if err != nil || string(original) != "unmodified library bytes" {
		t.Fatalf("package conversion changed input: %v %q", err, original)
	}
}

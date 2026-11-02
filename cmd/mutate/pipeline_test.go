package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"github.com/rfdetoni/mutate/internal/domain"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func makeUnsafeSource(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	file := filepath.Join(dir, "project.tar.gz")
	sentinel := filepath.Join(dir, "ran")
	makefile := "all:\n\t@touch " + sentinel + "\n"
	var data bytes.Buffer
	gz := gzip.NewWriter(&data)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "Makefile", Mode: 0644, Size: int64(len(makefile))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(makefile)); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, data.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
	return file, sentinel
}
func TestSourceBuildIsOptIn(t *testing.T) {
	file, sentinel := makeUnsafeSource(t)
	_, cleanup, err := prepareConversion(file, "deb", false)
	if cleanup != nil {
		cleanup()
	}
	if err == nil {
		t.Fatal("expected refusal without source build permission")
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatalf("source executed without permission: %v", err)
	}
}
func TestInstallDryRunNeverRunsSource(t *testing.T) {
	file, sentinel := makeUnsafeSource(t)
	cmd := newInstallCmd()
	cmd.SetArgs([]string{file, "--dry-run"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatalf("dry-run executed source: %v", err)
	}
}

func TestExplicitCrossDistroDependencyMapping(t *testing.T) {
	deb := createSampleDeb(t, "libc6")
	defer os.Remove(deb)
	mapping := filepath.Join(t.TempDir(), "map.json")
	if err := os.WriteFile(mapping, []byte(`{"libc6":"glibc"}`), 0600); err != nil {
		t.Fatal(err)
	}
	ir, cleanup, err := prepareConversionWithOptions(deb, "rpm", false, conversionOptions{DependencyMapPath: mapping})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if len(ir.Metadata.Dependencies) != 1 || ir.Metadata.Dependencies[0] != "glibc" {
		t.Fatalf("unexpected dependencies: %v", ir.Metadata.Dependencies)
	}
	if ir.Format != domain.FormatDEB {
		t.Fatal("source format lost")
	}
}
func TestCrossDistroDependencyMustBeMapped(t *testing.T) {
	deb := createSampleDeb(t, "libc6")
	defer os.Remove(deb)
	_, cleanup, err := prepareConversionWithOptions(deb, "rpm", false, conversionOptions{})
	if cleanup != nil {
		cleanup()
	}
	if err == nil || !strings.Contains(err.Error(), "--dependency-map") {
		t.Fatalf("expected mapping error; got %v", err)
	}
}

package main

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rfdetoni/mutate/internal/domain"
	"github.com/rfdetoni/mutate/internal/packager"
)

func syntheticPortableAppImage(t *testing.T) string {
	t.Helper()
	b := make([]byte, 4096)
	copy(b, []byte{0x7f, 'E', 'L', 'F'})
	b[4] = 2
	b[5] = 1
	b[6] = 1
	copy(b[8:11], []byte{'A', 'I', 2})
	binary.LittleEndian.PutUint16(b[16:18], 2)
	binary.LittleEndian.PutUint16(b[18:20], 62)
	binary.LittleEndian.PutUint32(b[20:24], 1)
	binary.LittleEndian.PutUint16(b[52:54], 64)
	src := filepath.Join(t.TempDir(), "portable.AppImage")
	if err := os.WriteFile(src, b, 0755); err != nil {
		t.Fatal(err)
	}
	return src
}

func TestDefaultAppImageBuildPreservesOriginal(t *testing.T) {
	src := syntheticPortableAppImage(t)
	ir, cleanup, err := prepareConversionWithOptions(src, "deb", false, conversionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if ir.Format != domain.FormatAppImage {
		t.Fatalf("unexpected format %s", ir.Format)
	}
	payload, ok := ir.Files["/opt/mutate/appimages/portable/portable.AppImage"]
	if !ok || payload.SourcePath != src {
		t.Fatalf("original not packaged directly: %+v", ir.Files)
	}
	if _, ok := ir.Files["/usr/share/applications/portable.desktop"]; !ok {
		t.Fatal("missing desktop")
	}
	out := filepath.Join(t.TempDir(), "portable.deb")
	if err := packager.BuildPackageToFile(ir, "deb", out); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(out); err != nil || st.Size() == 0 {
		t.Fatalf("package not built: %v", err)
	}
}

func TestAppImageExplicitExtractUsesOriginalUnpacker(t *testing.T) {
	src := syntheticPortableAppImage(t)
	_, cleanup, err := prepareConversionWithOptions(src, "deb", false, conversionOptions{Extract: true})
	if cleanup != nil {
		cleanup()
	}
	if err == nil || !strings.Contains(err.Error(), "SquashFS") {
		t.Fatalf("expected full extraction to validate SquashFS, got %v", err)
	}
}

func TestExtractFlagRejectsConventionalPackages(t *testing.T) {
	src := createSampleDeb(t)
	defer os.Remove(src)
	_, cleanup, err := prepareConversionWithOptions(src, "deb", false, conversionOptions{Extract: true})
	if cleanup != nil {
		cleanup()
	}
	if err == nil || !strings.Contains(err.Error(), "only applicable to AppImage") {
		t.Fatalf("wrong extract handling: %v", err)
	}
}

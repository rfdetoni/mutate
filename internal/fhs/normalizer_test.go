package fhs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rfdetoni/mutate/internal/domain"
	"github.com/rfdetoni/mutate/internal/resolver"
)

func TestNormalizePackage(t *testing.T) {
	stagingDir, err := os.MkdirTemp("", "staging-fhs-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(stagingDir)

	lib64Dir := filepath.Join(stagingDir, "usr/lib64")
	if err := os.MkdirAll(lib64Dir, 0755); err != nil {
		t.Fatal(err)
	}
	srcLib := filepath.Join(lib64Dir, "libcustom.so.1")
	if err := os.WriteFile(srcLib, []byte("fake-so"), 0755); err != nil {
		t.Fatal(err)
	}

	desktopDir := filepath.Join(stagingDir, "usr/share/applications")
	if err := os.MkdirAll(desktopDir, 0755); err != nil {
		t.Fatal(err)
	}
	desktopFile := filepath.Join(desktopDir, "app.desktop")
	desktopContent := "[Desktop Entry]\nName=App\nExec=/usr/bin/app\nPath=/usr/lib64/app\n"
	if err := os.WriteFile(desktopFile, []byte(desktopContent), 0644); err != nil {
		t.Fatal(err)
	}

	ir := domain.NewPackageIR(stagingDir, domain.FormatRPM)
	ir.Metadata.Architecture = "amd64"
	ir.Files["/usr/lib64/libcustom.so.1"] = domain.FileEntry{
		SourcePath:      srcLib,
		DestinationPath: "/usr/lib64/libcustom.so.1",
	}
	ir.Files["/usr/share/applications/app.desktop"] = domain.FileEntry{
		SourcePath:      desktopFile,
		DestinationPath: "/usr/share/applications/app.desktop",
	}

	// Target is Debian amd64 -> should normalize /usr/lib64 to /usr/lib/x86_64-linux-gnu
	err = NormalizePackage(ir, resolver.DistroDebian)
	if err != nil {
		t.Fatalf("NormalizePackage error: %v", err)
	}

	targetDest := "/usr/lib/x86_64-linux-gnu/libcustom.so.1"
	if _, ok := ir.Files[targetDest]; !ok {
		t.Fatalf("expected file normalized to %s", targetDest)
	}

	// Verify desktop file content replaced
	updatedData, err := os.ReadFile(desktopFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(updatedData), "/usr/lib/x86_64-linux-gnu/app") {
		t.Fatalf("expected desktop file path replaced, got:\n%s", string(updatedData))
	}
}

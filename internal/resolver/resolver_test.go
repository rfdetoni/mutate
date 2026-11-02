package resolver

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rfdetoni/mutate/internal/domain"
)

func TestDetectHostDistro(t *testing.T) {
	distro := DetectHostDistro()
	if distro == "" {
		t.Fatal("expected non-empty distro")
	}
	t.Logf("Detected host distro: %s", distro)
}

func TestDebianResolverSearch(t *testing.T) {
	// Create temporary mock dpkg info dir
	tmpDir, err := os.MkdirTemp("", "mock-dpkg-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	listFile := filepath.Join(tmpDir, "libssl3:amd64.list")
	content := "/usr/lib/x86_64-linux-gnu/libssl.so.3\n/usr/lib/x86_64-linux-gnu/libcrypto.so.3\n"
	if err := os.WriteFile(listFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	r := &DebianResolver{dpkgInfoDir: tmpDir}
	pkg, err := r.Resolve("libssl.so.3")
	if err != nil {
		t.Fatalf("unexpected error resolving libssl.so.3: %v", err)
	}
	if pkg != "libssl3" {
		t.Fatalf("expected libssl3, got %s", pkg)
	}
}

func TestArchResolverSearch(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "mock-pacman-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	pkgDir := filepath.Join(tmpDir, "openssl-3.0.1-1")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	filesFile := filepath.Join(pkgDir, "files")
	content := "%FILES%\nusr/\nusr/lib/\nusr/lib/libssl.so.3\n"
	if err := os.WriteFile(filesFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	r := &ArchResolver{pacmanLocalDir: tmpDir}
	pkg, err := r.Resolve("libssl.so.3")
	if err != nil {
		t.Fatalf("unexpected error resolving libssl.so.3: %v", err)
	}
	if pkg != "openssl" {
		t.Fatalf("expected openssl, got %s", pkg)
	}
}

func TestBundler(t *testing.T) {
	stagingDir, err := os.MkdirTemp("", "staging-bundler-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(stagingDir)

	binDir := filepath.Join(stagingDir, "usr/bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	binFile := filepath.Join(binDir, "myapp")
	if err := os.WriteFile(binFile, []byte("#!/bin/sh\necho real-app"), 0755); err != nil {
		t.Fatal(err)
	}

	ir := domain.NewPackageIR(stagingDir, domain.FormatRPM)
	ir.Files["/usr/bin/myapp"] = domain.FileEntry{
		SourcePath:      binFile,
		DestinationPath: "/usr/bin/myapp",
		Mode:            0755,
	}

	bundler := NewLibraryBundler("myapp")
	err = bundler.BundleMissingLibraries(ir, []string{"libold.so.1"}, DistroDebian)
	if err != nil {
		t.Fatalf("bundle error: %v", err)
	}

	// Verify wrapper exists at /usr/bin/myapp
	wrapperData, err := os.ReadFile(binFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytesContains(wrapperData, "LD_LIBRARY_PATH") {
		t.Fatal("expected wrapper script at /usr/bin/myapp to contain LD_LIBRARY_PATH")
	}

	// Verify real binary moved to /usr/lib/myapp/bin/myapp
	realBin := filepath.Join(stagingDir, "usr/lib/myapp/bin/myapp")
	if _, err := os.Stat(realBin); err != nil {
		t.Fatalf("expected real binary at %s: %v", realBin, err)
	}
}

func bytesContains(b []byte, s string) bool {
	return len(b) > 0 && string(b) != "" && string(b) != s && (len(s) <= len(b)) && (string(b) == s || containsSub(string(b), s))
}

func containsSub(a, b string) bool {
	for i := 0; i+len(b) <= len(a); i++ {
		if a[i:i+len(b)] == b {
			return true
		}
	}
	return false
}

package elfanalyzer

import (
	"os"
	"strings"
	"testing"

	"github.com/rfdetoni/mutate/internal/domain"
)

func TestIsELF(t *testing.T) {
	// 1. Test real ELF binary if available
	candidates := []string{"/usr/bin/ls", "/bin/sh", "/usr/bin/env"}
	var foundELF string
	for _, c := range candidates {
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
			foundELF = c
			break
		}
	}

	if foundELF != "" {
		if !IsELF(foundELF) {
			t.Fatalf("expected %s to be recognized as ELF", foundELF)
		}
	}

	// 2. Non-ELF file
	tmpFile, err := os.CreateTemp("", "not-elf-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.WriteString("this is plain text, not ELF")
	tmpFile.Close()

	if IsELF(tmpFile.Name()) {
		t.Fatalf("expected text file not to be recognized as ELF")
	}
}

func TestAnalyzeELF(t *testing.T) {
	candidates := []string{"/usr/bin/ls", "/bin/sh"}
	var foundELF string
	for _, c := range candidates {
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
			foundELF = c
			break
		}
	}
	if foundELF == "" {
		t.Skip("no system ELF binary found to test")
	}

	info, err := AnalyzeELF(foundELF)
	if err != nil {
		t.Fatalf("AnalyzeELF error on %s: %v", foundELF, err)
	}

	if info.Architecture == "" {
		t.Fatal("expected non-empty architecture")
	}

	// System dynamically linked ELF should have libraries and interpreter
	if len(info.DynamicLibraries) == 0 && info.Interpreter == "" {
		t.Logf("Notice: %s may be statically linked", foundELF)
	}
}

func TestAnalyzePackage(t *testing.T) {
	ir := domain.NewPackageIR("", domain.FormatDEB)
	ir.Files["/usr/lib/libtest.so.1"] = domain.FileEntry{
		DestinationPath: "/usr/lib/libtest.so.1",
	}

	candidates := []string{"/usr/bin/ls", "/bin/sh"}
	var foundELF string
	for _, c := range candidates {
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
			foundELF = c
			break
		}
	}
	if foundELF == "" {
		t.Skip("no ELF found")
	}

	ir.Files["/usr/bin/myapp"] = domain.FileEntry{
		SourcePath:      foundELF,
		DestinationPath: "/usr/bin/myapp",
	}

	res, err := AnalyzePackage(ir)
	if err != nil {
		t.Fatalf("AnalyzePackage error: %v", err)
	}

	if res.TotalELFs != 1 {
		t.Fatalf("expected 1 total ELF, got %d", res.TotalELFs)
	}

	if _, ok := res.InternalLibraries["libtest.so.1"]; !ok {
		t.Fatal("expected internal library libtest.so.1 to be indexed")
	}
}

func TestGenerateWrapperScript(t *testing.T) {
	opts := WrapperOptions{
		AppName:        "myapp",
		OriginalBin:    "/usr/lib/myapp/myapp-bin",
		IsolatedLibDir: "/usr/lib/myapp/lib",
		CustomEnv:      []string{"FOO=bar"},
	}

	script := GenerateWrapperScriptContent(opts)
	if !strings.Contains(script, "LD_LIBRARY_PATH") {
		t.Fatal("expected wrapper script to contain LD_LIBRARY_PATH")
	}
	if !strings.Contains(script, "/usr/lib/myapp/myapp-bin") {
		t.Fatal("expected wrapper script to invoke original binary")
	}
	if !strings.Contains(script, "export FOO=bar") {
		t.Fatal("expected wrapper script to export FOO=bar")
	}
}

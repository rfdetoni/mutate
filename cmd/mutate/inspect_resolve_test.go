package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rfdetoni/mutate/internal/domain"
	"github.com/rfdetoni/mutate/internal/elfanalyzer"
	"github.com/rfdetoni/mutate/internal/packager"
)

func TestInspectNoResolveKeepsFullELFAnalysis(t *testing.T) {
	// Use an actual Linux ELF binary in a native DEB, so the no-resolve path
	// is tested at the CLI/packaging boundary rather than by mocking results.
	var native string
	for _, candidate := range []string{"/bin/ls", "/usr/bin/ls", "/usr/bin/env"} {
		if st, err := os.Stat(candidate); err == nil && st.Mode().IsRegular() {
			info, err := elfanalyzer.AnalyzeELF(candidate)
			if err == nil && len(info.DynamicLibraries) > 0 {
				native = candidate
				break
			}
		}
	}
	if native == "" {
		t.Skip("no dynamic ELF binary on this test host")
	}
	stage := t.TempDir()
	ir := domain.NewPackageIR(stage, domain.FormatDEB)
	ir.Metadata = domain.Metadata{
		Name: "inspect-no-resolve", Version: "1.0.0", Release: "1",
		Architecture: "amd64", Description: "Integration fixture",
	}
	ir.Files["/usr/bin/fixture"] = domain.FileEntry{
		SourcePath: native, DestinationPath: "/usr/bin/fixture", Mode: 0755,
	}
	pkg := filepath.Join(t.TempDir(), "fixture.deb")
	if err := packager.BuildPackageToFile(ir, "deb", pkg); err != nil {
		t.Fatal(err)
	}
	cmd := newInspectCmd()
	var output, errors bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&errors)
	cmd.SetArgs([]string{pkg, "--no-resolve"})
	t.Setenv("PATH", t.TempDir()) // No system package-manager executable available.
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Total ELF Binaries: 1") {
		t.Fatalf("full ELF analysis was omitted: %s", output.String())
	}
	if !strings.Contains(output.String(), "[NOT RESOLVED (--no-resolve)]") {
		t.Fatalf("missing explicit provider status: %s", output.String())
	}
	if strings.Contains(output.String(), "[NOT FOUND on host") {
		t.Fatalf("resolver was unexpectedly invoked: %s", output.String())
	}
}

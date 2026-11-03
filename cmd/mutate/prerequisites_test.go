package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rfdetoni/mutate/internal/domain"
	"github.com/spf13/cobra"
)

func TestWarnMissingWaydroidInCLI(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	var stderr bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetErr(&stderr)
	informRuntimePrerequisites(cmd, domain.FormatAndroidAPK, "ignored.apk", false)
	if !strings.Contains(stderr.String(), "[WARN]") ||
		!strings.Contains(stderr.String(), "Waydroid") ||
		!strings.Contains(stderr.String(), "target system") {
		t.Fatalf("expected actionable Waydroid prerequisite notice: %q", stderr.String())
	}
}

func TestBuildPreservedAppImageShowsRuntimeNotice(t *testing.T) {
	src := syntheticPortableAppImage(t)
	out := filepath.Join(t.TempDir(), "test.deb")
	var stderr bytes.Buffer
	cmd := newBuildCmd()
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{src, "--to", "deb", "-o", out})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr.String(), "AppImage Type 2 preserved") ||
		!strings.Contains(stderr.String(), "current host") {
		t.Fatalf("expected runtime prerequisite warning during build: %q", stderr.String())
	}
}

func TestDryRunAppImageReportsPrerequisites(t *testing.T) {
	src := syntheticPortableAppImage(t)
	var stderr bytes.Buffer
	cmd := newInstallCmd()
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{src, "--dry-run"})
	if err := cmd.Execute(); err != nil {
		if strings.Contains(err.Error(), "unsupported host distribution") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	if !strings.Contains(stderr.String(), "AppImage Type 2 preserved") {
		t.Fatalf("dry-run skipped runtime prerequisite validation: %q", stderr.String())
	}
}

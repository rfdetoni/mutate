package domain

import (
	"strings"
	"testing"
)

func TestValidatePackageIRRejectsUnsafePackagePaths(t *testing.T) {
	for _, destination := range []string{"relative/file", "/usr/../etc/file", "/", "/usr/./bin/foo"} {
		ir := NewPackageIR("/tmp/staging", FormatDEB)
		ir.Files[destination] = FileEntry{SourcePath: "/tmp/source", DestinationPath: destination}
		err := ValidatePackageIR(ir)
		if err == nil || !strings.Contains(err.Error(), "invalid package destination") {
			t.Fatalf("accepted %q: %v", destination, err)
		}
	}
}

func TestValidatePackageIRRejectsInconsistentEntries(t *testing.T) {
	ir := NewPackageIR("/tmp/staging", FormatDEB)
	ir.Files["/usr/bin/example"] = FileEntry{SourcePath: "/tmp/source", DestinationPath: "/usr/bin/different"}
	if err := ValidatePackageIR(ir); err == nil {
		t.Fatal("mismatch accepted")
	}
	ir.Files["/usr/bin/example"] = FileEntry{IsSymlink: true, DestinationPath: "/usr/bin/example"}
	if err := ValidatePackageIR(ir); err == nil {
		t.Fatal("empty symlink accepted")
	}
}

func TestValidatePackageIRAllowsOriginalPortableArtifact(t *testing.T) {
	ir := NewPackageIR("/tmp/private-staging", FormatAppImage)
	ir.Files["/opt/mutate/appimages/example/example.AppImage"] = FileEntry{
		DestinationPath: "/opt/mutate/appimages/example/example.AppImage",
		SourcePath:      "/home/user/Downloads/example.AppImage",
	}
	if err := ValidatePackageIR(ir); err != nil {
		t.Fatal(err)
	}
}

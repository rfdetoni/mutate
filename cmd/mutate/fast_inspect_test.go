package main

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestFastInspectJSONReadsMetadataWithoutExtraction(t *testing.T) {
	src := createSampleDeb(t, "")
	defer os.Remove(src)
	cmd := newInspectCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{src, "--fast", "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var result struct {
		Format   string                `json:"format"`
		Fast     bool                  `json:"fast"`
		Metadata struct{ Name string } `json:"metadata"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("output is not JSON: %v %s", err, stdout.String())
	}
	if !result.Fast || result.Format != "deb" || result.Metadata.Name == "" {
		t.Fatalf("bad fast output: %+v", result)
	}
	if strings.Contains(stdout.String(), "ELF") || strings.Contains(stdout.String(), "Analyzing") {
		t.Fatalf("fast JSON polluted by log output: %s", stdout.String())
	}
}
func TestFastExtractFlagsConflict(t *testing.T) {
	src := syntheticPortableAppImage(t)
	cmd := newInspectCmd()
	cmd.SetArgs([]string{src, "--fast", "--extract"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "cannot be combined") {
		t.Fatalf("flags: %v", err)
	}
}
func TestOverrideAppImageMetadataViaBuild(t *testing.T) {
	src := syntheticPortableAppImage(t)
	ir, cleanup, err := prepareConversionWithOptions(src, "deb", false, conversionOptions{PackageName: "renamed-app", PackageVersion: "2.3.4"})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if ir.Metadata.Name != "renamed-app" || ir.Metadata.Version != "2.3.4" {
		t.Fatalf("metadata: %+v", ir.Metadata)
	}
	if _, ok := ir.Files["/usr/share/applications/renamed-app.desktop"]; !ok {
		t.Fatal("desktop shortcut ignored overridden identity")
	}
}

package unpacker

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rfdetoni/mutate/internal/domain"
)

func TestExtractionRejectsExcessEntries(t *testing.T) {
	stage := t.TempDir()
	budget := &extractionBudget{entries: maxExtractedEntries}
	err := extractArchiveEntry(stage, nil, "many", tar.TypeReg, "", 0644, time.Time{}, "", "", bytes.NewReader(nil), budget)
	if err == nil || !strings.Contains(err.Error(), "entries") {
		t.Fatalf("expected entry limit: %v", err)
	}
}
func TestExtractionRejectsTotalSize(t *testing.T) {
	stage := t.TempDir()
	budget := &extractionBudget{bytes: maxExtractedBytes - 2}
	err := extractArchiveEntry(stage, nil, "excess", tar.TypeReg, "", 0644, time.Time{}, "", "", bytes.NewReader([]byte("123")), budget)
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected total byte limit: %v", err)
	}
}
func TestExtractionRejectsOversizedMetadata(t *testing.T) {
	raw := bytes.NewReader(bytes.Repeat([]byte("x"), int(maxMetadataBytes)+1))
	if _, err := readMetadata(raw); err == nil {
		t.Fatal("large metadata accepted")
	}
	small, err := readMetadata(strings.NewReader("Package: ok"))
	if err != nil || string(small) != "Package: ok" {
		t.Fatalf("small metadata: %v %q", err, small)
	}
}
func TestMetadataFastInspectionOfNativeFormats(t *testing.T) {
	fixtures := map[domain.Format]string{
		domain.FormatDEB:       "sample.deb",
		domain.FormatRPM:       "sample.rpm",
		domain.FormatArch:      "sample.pkg.tar.zst",
		domain.FormatAlpineAPK: "sample-alpine.apk",
	}
	for format, name := range fixtures {
		t.Run(string(format), func(t *testing.T) {
			src := filepath.Join("..", "..", "tests", "fixtures", name)
			m, err := InspectMetadata(src, format)
			if err != nil {
				t.Fatal(err)
			}
			if m.Name != "sample-app" || m.Version == "" {
				t.Fatalf("metadata: %+v", m)
			}
		})
	}
}
func TestAppImageIdentityAndVersion(t *testing.T) {
	name, version, err := appImageIdentity("Foo-1.2.3-x86_64.AppImage")
	if err != nil || name != "foo" || version != "1.2.3" {
		t.Fatalf("identity: %q %q %v", name, version, err)
	}
	src := minimalType1(t, true)
	ir, err := NewAppImageUnpacker().PreserveWithMetadata(src, t.TempDir(), "my-custom-app", "5.6.7")
	if err != nil {
		t.Fatal(err)
	}
	if ir.Metadata.Name != "my-custom-app" || ir.Metadata.Version != "5.6.7" {
		t.Fatalf("overrides: %+v", ir.Metadata)
	}
}
func TestAPKPackageNamesDoNotCollide(t *testing.T) {
	left, right := sanitizeAppName("com.acme.app"), sanitizeAppName("org.other.app")
	if left == right || left != "com-acme-app" || right != "org-other-app" {
		t.Fatalf("collision: %q %q", left, right)
	}
}
func TestCMakeInstallDoesNotDuplicateDESTDIR(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0755); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(dir, "calls")
	bwrap := filepath.Join(bin, "bwrap")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> '" + log + "'\n"
	if err := os.WriteFile(bwrap, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	cmake := filepath.Join(bin, "cmake")
	if err := os.WriteFile(cmake, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	stage := filepath.Join(dir, "stage")
	work := filepath.Join(stage, "src")
	if err := os.MkdirAll(work, 0755); err != nil {
		t.Fatal(err)
	}
	if err := NewSourceUnpacker().CompileAndInstall(work, stage, BuildSystemCMake, "myapp"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	contents := string(data)
	if !strings.Contains(contents, "-DCMAKE_INSTALL_PREFIX=/usr") || !strings.Contains(contents, "--install") {
		t.Fatalf("CMake invocation missing: %s", contents)
	}
	if strings.Contains(contents, "--prefix "+stage) {
		t.Fatalf("double DESTDIR: %s", contents)
	}
}

func TestSandboxReadOnlyCacheIsExplicit(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0755); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(dir, "calls")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" > '" + log + "'\n"
	if err := os.WriteFile(filepath.Join(bin, "bwrap"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	cache := filepath.Join(dir, "cache")
	if err := os.MkdirAll(cache, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MUTATE_BUILD_CACHE_DIR", cache)
	stage := filepath.Join(dir, "stage")
	work := filepath.Join(stage, "src")
	if err := os.MkdirAll(work, 0755); err != nil {
		t.Fatal(err)
	}
	if err := runIsolatedBuild(work, stage, "/bin/true"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "--ro-bind "+cache+" /mutate-cache") {
		t.Fatalf("cache not read-only: %s", b)
	}
	if !strings.Contains(string(b), "--setenv GOPROXY off") {
		t.Fatalf("offline requirement lost: %s", b)
	}
}

func TestConfiguredExtractionLimits(t *testing.T) {
	t.Setenv("MUTATE_MAX_EXTRACT_BYTES", "2")
	t.Setenv("MUTATE_MAX_EXTRACT_ENTRIES", "1")
	budget, err := newExtractionBudget()
	if err != nil {
		t.Fatal(err)
	}
	if budget.limitBytes != 2 || budget.limitEntries != 1 {
		t.Fatalf("limits not applied: %+v", budget)
	}
	stage := t.TempDir()
	err = extractArchiveEntry(stage, nil, "oversized", tar.TypeReg, "", 0644, time.Time{}, "", "", bytes.NewReader([]byte("abc")), budget)
	if err == nil {
		t.Fatal("over-budget entry accepted")
	}
	t.Setenv("MUTATE_MAX_EXTRACT_BYTES", "invalid")
	if _, err := newExtractionBudget(); err == nil {
		t.Fatal("invalid byte budget accepted")
	}
}

package resolver

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestResolveLibrariesDebianBatch(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "libfirst:amd64.list"), []byte(
		"/usr/lib/x86_64-linux-gnu/libfirst.so.1\n/usr/lib/x86_64-linux-gnu/libsecond.so.2\n",
	), 0644); err != nil {
		t.Fatal(err)
	}
	r := &DebianResolver{dpkgInfoDir: dir}
	got := ResolveLibraries(r, []string{"libfirst.so.1", "libsecond.so.2"})
	want := map[string]string{"libfirst.so.1": "libfirst", "libsecond.so.2": "libfirst"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("batch mismatch: got=%v want=%v", got, want)
	}
}

func TestResolveLibrariesArchBatch(t *testing.T) {
	dir := t.TempDir()
	files := filepath.Join(dir, "my-library-3.0.1-1", "files")
	if err := os.MkdirAll(filepath.Dir(files), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(files, []byte("%FILES%\nusr/lib/libfoo.so.1\nusr/lib/libbar.so.2\n"), 0644); err != nil {
		t.Fatal(err)
	}
	r := &ArchResolver{pacmanLocalDir: dir}
	got := ResolveLibraries(r, []string{"libfoo.so.1", "libbar.so.2"})
	want := map[string]string{"libfoo.so.1": "my-library", "libbar.so.2": "my-library"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("batch mismatch: got=%v want=%v", got, want)
	}
}

func TestResolveLibrariesAlpineBatch(t *testing.T) {
	installed := filepath.Join(t.TempDir(), "installed")
	data := "P:libfoo\nV:1\nF:usr/lib\nR:libfoo.so.1\nR:libbar.so.2\n\n" +
		"P:bin-tool\nF:usr/bin\nR:libnotalibrary.so.1\n\n"
	if err := os.WriteFile(installed, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}
	r := &AlpineResolver{dbPath: installed}
	got := ResolveLibraries(r, []string{"libfoo.so.1", "libbar.so.2", "libnotalibrary.so.1"})
	want := map[string]string{"libfoo.so.1": "libfoo", "libbar.so.2": "libfoo"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("batch mismatch: got=%v want=%v", got, want)
	}
}

type countingResolver struct{ calls int }

func (r *countingResolver) HostDistro() Distro { return DistroOther }
func (r *countingResolver) Resolve(name string) (string, error) {
	r.calls++
	if name == "libfound.so.1" {
		return "provider", nil
	}
	return "", errors.New("missing")
}
func TestResolveLibrariesFallbackPreservesMissing(t *testing.T) {
	r := &countingResolver{}
	got := ResolveLibraries(r, []string{"libfound.so.1", "libabsent.so.1"})
	if got["libfound.so.1"] != "provider" {
		t.Fatalf("found provider lost: %v", got)
	}
	if _, ok := got["libabsent.so.1"]; ok {
		t.Fatalf("missing provider incorrectly resolved: %v", got)
	}
	if r.calls != 2 {
		t.Fatalf("fallback called %d times", r.calls)
	}
}

func TestResolveLibrariesNoInputsDoesNotTouchResolver(t *testing.T) {
	r := &countingResolver{}
	if result := ResolveLibraries(r, nil); result != nil {
		t.Fatalf("expected nil, got %v", result)
	}
	if r.calls != 0 {
		t.Fatal("unexpected resolver invocation")
	}
}

package catalog

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rfdetoni/mutate/internal/resolver"
)

func fakeAUR(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/rpc/v5/search/") {
			t.Errorf("unexpected AUR path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"version":5,"type":"search","results":[
    {"Name":"firefox-beta-bin","Version":"152.0-1","Description":"Firefox beta","OutOfDate":null},
    {"Name":"firefox-nightly","Version":"155.0-1","Description":"Firefox nightly","OutOfDate":1710000000}
  ]}`))
	}))
}

func TestCatalogNativeFirstAndCandidates(t *testing.T) {
	aur := fakeAUR(t)
	defer aur.Close()
	calls := 0
	run := func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls++
		if name != "apt-cache" {
			t.Fatalf("unexpected executable %s", name)
		}
		if len(args) > 0 && args[0] == "search" {
			return []byte("firefox-data - Browser language data\nfirefox - Web browser\n"), nil
		}
		if len(args) > 0 && args[0] == "policy" {
			return []byte("firefox-data:\n Installed: (none)\n Candidate: 1.4-2\nfirefox:\n Installed: 1.3\n Candidate: 1.6-1\n"), nil
		}
		return nil, fmt.Errorf("unexpected apt command %v", args)
	}
	result, err := Search(context.Background(), Options{
		Host: resolver.DistroDebian, Query: "firefox", IncludeAUR: true, Run: run, AURBase: aur.URL, Client: aur.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("expected batched policy, got %d calls", calls)
	}
	if len(result.Apps) != 4 {
		t.Fatalf("expected native and AUR results, got %+v", result.Apps)
	}
	if result.Apps[0].Name != "firefox" || result.Apps[0].Version != "1.6-1" {
		t.Fatalf("exact native candidate must rank first: %+v", result.Apps)
	}
	if !result.Apps[0].Installable || result.Apps[2].Installable {
		t.Fatal("nonnative packages must not be installed automatically")
	}
	if result.Apps[3].URL != "https://aur.archlinux.org/packages/firefox-nightly" ||
		!result.Apps[3].OutOfDate {
		t.Fatal("AUR metadata not normalized correctly")
	}
}

func TestCatalogPacmanAndAlpine(t *testing.T) {
	archRun := func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name != "pacman" {
			t.Fatalf("wrong manager: %s", name)
		}
		return []byte("extra/blender 4.5.3-2\n    3D creation suite\nextra/blender-lts 4.2.4-1\n    Extended support\n"), nil
	}
	apps, err := searchNative(context.Background(), resolver.DistroArch, "blender", archRun)
	if err != nil || len(apps) != 2 || apps[0].Version != "4.5.3-2" || apps[0].Repository != "extra" || apps[0].Description != "3D creation suite" {
		t.Fatalf("pacman parsing: %+v %v", apps, err)
	}
	apkRun := func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name != "apk" {
			t.Fatalf("wrong manager: %s", name)
		}
		return []byte("blender-4.3.2-r1 - 3D suite\nblender-doc-4.3.2-r1 - Documentation\n"), nil
	}
	apps, err = searchNative(context.Background(), resolver.DistroAlpine, "blender", apkRun)
	if err != nil || len(apps) != 2 || apps[0].Version != "4.3.2-r1" || apps[0].Name != "blender" {
		t.Fatalf("apk parsing: %+v %v", apps, err)
	}
}

func TestCatalogDNFOutput(t *testing.T) {
	run := func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name != "dnf" && name != "dnf5" {
			t.Fatalf("wrong manager %s", name)
		}
		return []byte("blender\t99.0-1\tWrong CPU\ttest\ti686\n" +
			"blender\t4.5.3-1.fc43\t3D creation suite\tupdates\tx86_64\n"), nil
	}
	apps, err := searchDNF(context.Background(), "blender", run)
	if err != nil || len(apps) != 1 || apps[0].Repository != "updates" || apps[0].Version != "4.5.3-1.fc43" {
		t.Fatalf("dnf parsing: %+v %v", apps, err)
	}
}

func TestCatalogQueryGuards(t *testing.T) {
	for _, q := range []string{"a", "-v", "../../etc/passwd", "$(whoami)", "\n\n", strings.Repeat("a", 61)} {
		if _, err := NormalizeQuery(q); err == nil {
			t.Fatalf("accepted invalid search %q", q)
		}
	}
	for _, name := range []string{"../../foo", "-rf", "foo/bar", "foo;rm", "x\""} {
		if ValidPackageName(name) {
			t.Fatalf("accepted invalid package name %q", name)
		}
	}
	if !ValidPackageName("firefox") || !ValidPackageName("libfoo3") {
		t.Fatal("rejected valid package")
	}
}

func TestCatalogAURFailureDoesNotHideNative(t *testing.T) {
	run := func(_ context.Context, name string, args ...string) ([]byte, error) {
		if args[0] == "search" {
			return []byte("firefox - web browser\n"), nil
		}
		return []byte("firefox:\n Candidate: 1.2-3\n"), nil
	}
	failure := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", 503)
	}))
	defer failure.Close()
	result, err := Search(context.Background(), Options{
		Host: resolver.DistroDebian, Query: "firefox", IncludeAUR: true, Run: run, AURBase: failure.URL, Client: failure.Client(),
	})
	if err != nil || len(result.Apps) != 1 || len(result.Warnings) != 1 {
		t.Fatalf("native fallback failed: %+v %v", result, err)
	}
}

func TestCatalogDoesNotCallAURWithoutOptIn(t *testing.T) {
	run := func(_ context.Context, name string, args ...string) ([]byte, error) {
		return []byte("firefox - web browser\n"), nil
	}
	result, err := Search(context.Background(), Options{
		Host: resolver.DistroDebian, Query: "firefox", IncludeAUR: false, Run: run, AURBase: "http://127.0.0.1:1",
	})
	if err != nil || len(result.Warnings) != 0 {
		t.Fatalf("unrequested network lookup: %+v %v", result, err)
	}
}

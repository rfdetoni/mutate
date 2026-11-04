package catalog

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestSearchFlatpakUsesOnlyOfficialConfiguredRemote(t *testing.T) {
	ctx := context.Background()
	run := func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name != "flatpak" {
			t.Fatalf("unexpected command %q", name)
		}
		if args[0] == "remotes" {
			if args[len(args)-1] == "--user" {
				return []byte("Name    URL\nflathub    https://dl.flathub.org/repo/\n"), nil
			}
			t.Fatal("user scope should be preferred")
		}
		if args[0] == "search" {
			if args[1] != "--user" || !strings.Contains(strings.Join(args, " "), "firefox") {
				t.Fatalf("bad search arguments: %v", args)
			}
			return []byte("Application ID    Name    Remotes\n" +
				"org.mozilla.firefox    Firefox    flathub\n" +
				"org.mozilla.firefox    Firefox    flathub\n" +
				"org.example.Tool    Sample app    other\n" +
				"bad/path    Unsafe    flathub\n"), nil
		}
		t.Fatalf("unexpected args: %v", args)
		return nil, nil
	}
	apps, err := SearchFlatpak(ctx, "firefox", run)
	if err != nil || len(apps) != 1 {
		t.Fatalf("unexpected apps %v, err %v", apps, err)
	}
	if apps[0].ID != "flatpak:flathub:org.mozilla.firefox" || !apps[0].Installable || apps[0].Native {
		t.Fatalf("wrong Flatpak entry %+v", apps[0])
	}
	if id, ok := FlatpakApplicationID(apps[0]); !ok || id != "org.mozilla.firefox" {
		t.Fatalf("flatpak ID rejected %q, %v", id, ok)
	}
}

func TestFlathubScopeRejectsRenamedOrUntrustedRemotes(t *testing.T) {
	ctx := context.Background()
	for _, remote := range []string{"flathub    http://dl.flathub.org/repo/",
		"flathub    https://evil.example/repo/",
		"flathub    https://dl.flathub.org.evil.example/repo/",
		"flathub    https://dl.flathub.org/repo/?redirect=evil",
		"flathub-copy    https://dl.flathub.org/repo/"} {
		run := func(_ context.Context, _ string, args ...string) ([]byte, error) {
			if args[0] != "remotes" {
				return nil, errors.New("unexpected search")
			}
			return []byte(remote + "\n"), nil
		}
		if _, err := FlathubScope(ctx, run); !errors.Is(err, ErrFlathubUnavailable) {
			t.Errorf("trusted unsafe remote %q: %v", remote, err)
		}
	}
}

func TestSearchFlatpakSystemScopeAndValidation(t *testing.T) {
	ctx := context.Background()
	run := func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if args[0] == "remotes" && args[len(args)-1] == "--user" {
			return nil, errors.New("not configured")
		}
		if args[0] == "remotes" {
			return []byte("flathub\thttps://dl.flathub.org/repo/\n"), nil
		}
		if args[0] == "search" {
			if args[1] != "--system" {
				t.Fatalf("wrong scope: %v", args)
			}
			return []byte("org.gimp.GIMP\tGIMP Image Editor\tflathub\n"), nil
		}
		return nil, errors.New("unexpected command")
	}
	apps, err := SearchFlatpak(ctx, "gimp", run)
	if err != nil || len(apps) != 1 {
		t.Fatalf("system search returned %+v, %v", apps, err)
	}
	for _, id := range []string{"-v", "evil;echo", "abc", "org.x", "org.example/tool"} {
		if ValidFlatpakID(id) {
			t.Errorf("accepted unsafe app ID %q", id)
		}
	}
	if _, ok := FlatpakApplicationID(App{ID: "aur:org.gimp.GIMP", Installable: true}); ok {
		t.Fatal("untrusted AUR package accepted")
	}
}

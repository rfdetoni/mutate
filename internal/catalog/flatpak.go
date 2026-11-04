package catalog

import (
    "context"
    "errors"
    "fmt"
    "net/url"
    "regexp"
    "strings"
)

// Flatpak is a portable fallback for applications in a configured, signed
// Flathub installation. It does not translate untrusted AUR/PKGBUILD scripts.
const flatpakRemote = "flathub"
const maxFlatpakResults = 30

var flatpakID = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_-]*(\.[a-zA-Z_][a-zA-Z0-9_-]*){2,}$`)
var flatpakColumns = regexp.MustCompile(`\t+| {2,}`)

var ErrFlathubUnavailable = errors.New("Flatpak/Flathub não configurado. Instale o Flatpak e adicione o remoto oficial flathub")

func ValidFlatpakID(id string) bool { return flatpakID.MatchString(id) }

// Only recognize the explicitly configured official Flathub endpoint. Merely
// naming a remote "flathub" must not authorize installation from another URL.
func validFlathubURL(raw string) bool {
    u, err := url.Parse(strings.TrimSpace(raw))
    if err != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
        return false
    }
    if u.Hostname() != "dl.flathub.org" && u.Hostname() != "flathub.org" {
        return false
    }
    return u.Port() == "" && (u.EscapedPath() == "/repo/" || u.EscapedPath() == "/repo")
}

// A remote may be configured for the current user or the whole system.
// Do not change global Flatpak settings or add remotes behind the user's back.
func FlathubScope(ctx context.Context, run Command) (string, error) {
    if run == nil { run = RunLocal }
    for _, scope := range []string{"--user", "--system"} {
        output, err := run(ctx, "flatpak", "remotes", "--columns=name,url", scope)
        if err != nil { continue }
        for _, line := range strings.Split(string(output), "\n") {
            fields := strings.Fields(line)
            if len(fields) == 2 && fields[0] == flatpakRemote && validFlathubURL(fields[1]) {
                return scope, nil
            }
        }
    }
    return "", ErrFlathubUnavailable
}

// SearchFlatpak consults only a preconfigured Flathub remote via the Flatpak
// CLI. Flatpak handles AppStream metadata and verified remote downloads.
func SearchFlatpak(ctx context.Context, query string, run Command) ([]App, error) {
    query, err := NormalizeQuery(query)
    if err != nil { return nil, err }
    if run == nil { run = RunLocal }
    scope, err := FlathubScope(ctx, run)
    if err != nil { return nil, err }
    output, err := run(ctx, "flatpak", "search", scope,
        "--columns=application:full,name:full,remotes:full", query)
    if err != nil { return nil, fmt.Errorf("consulta ao Flathub: %w", err) }
    apps := make([]App, 0, 10)
    seen := make(map[string]bool)
    for _, line := range strings.Split(string(output), "\n") {
        columns := flatpakColumns.Split(strings.TrimSpace(line), -1)
        if len(columns) != 3 { continue }
        id := strings.TrimSpace(columns[0])
        remote := strings.TrimSpace(columns[2])
        found := false
        for _, part := range strings.Split(remote, ",") {
            if strings.TrimSpace(part) == flatpakRemote { found = true; break }
        }
        if !found || !ValidFlatpakID(id) || seen[id] { continue }
        seen[id] = true
        name := strings.TrimSpace(columns[1])
        if name == "" { name = id }
        apps = append(apps, App{
            ID: "flatpak:flathub:" + id, Name: name,
            Description: "Aplicativo Flatpak (instalado pelo repositório Flathub)",
            Source: "Flathub", Repository: flatpakRemote,
            URL: "https://flathub.org/apps/" + url.PathEscape(id),
            Installable: true,
        })
        if len(apps) >= maxFlatpakResults { break }
    }
    return apps, nil
}

// Flatpak catalog IDs are only actionable when they came from a server-side
// result and the selected remote is still the official configured Flathub.
func FlatpakApplicationID(app App) (string, bool) {
    const prefix = "flatpak:flathub:"
    if app.Native || !app.Installable || app.Repository != flatpakRemote ||
        app.Source != "Flathub" || !strings.HasPrefix(app.ID, prefix) { return "", false }
    id := strings.TrimPrefix(app.ID, prefix)
    return id, ValidFlatpakID(id)
}

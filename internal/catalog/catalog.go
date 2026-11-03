// Package catalog searches host-enabled package repositories and the AUR.
// Native packages are preferred; nonnative entries are discovery-only.
package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rfdetoni/mutate/internal/resolver"
)

const (
	MaxQueryLength = 60
	MaxNative      = 30
	MaxAUR         = 20
	ScanLimit      = 200
)

var packageName = regexp.MustCompile(`^[a-z0-9][a-z0-9+._-]{0,127}$`)
var versionSuffix = regexp.MustCompile(`^(.+)-([0-9][0-9A-Za-z.+_~-]*(?:-r[0-9]+)?)$`)

// App identifies a discoverable package. Native results use the system's
// candidate from its enabled repositories, not a cross-distro version guess.
type App struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	Source      string `json:"source"`
	Repository  string `json:"repository,omitempty"`
	URL         string `json:"url,omitempty"`
	Native      bool   `json:"native"`
	Installable bool   `json:"installable"`
	OutOfDate   bool   `json:"out_of_date,omitempty"`
}
type Result struct {
	Query    string   `json:"query"`
	Host     string   `json:"host"`
	Apps     []App    `json:"apps"`
	Warnings []string `json:"warnings,omitempty"`
}
type Command func(ctx context.Context, name string, args ...string) ([]byte, error)
type Options struct {
	Host       resolver.Distro
	Query      string
	IncludeAUR bool
	Run        Command
	Client     *http.Client
	AURBase    string // injectable for offline tests
}

func ValidPackageName(name string) bool { return packageName.MatchString(name) }

// Validated, short literal search terms cannot become shell options or
// arbitrary regexes in apt/pacman. Every child process uses argv, never sh.
func NormalizeQuery(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if len(s) < 2 || len(s) > MaxQueryLength {
		return "", fmt.Errorf("a busca deve conter entre 2 e %d caracteres", MaxQueryLength)
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9') && r != ' ' && r != '-' && r != '_' && r != '.' && r != '+' {
			return "", fmt.Errorf("use letras, números, espaços, ponto, hífen ou +")
		}
	}
	if strings.HasPrefix(s, "-") {
		return "", fmt.Errorf("consulta inválida")
	}
	return s, nil
}

type limitedBuffer struct{ b []byte }

func (w *limitedBuffer) Write(p []byte) (int, error) {
	if len(p)+len(w.b) > 2<<20 {
		return 0, fmt.Errorf("resultados muito grandes; refine a busca")
	}
	w.b = append(w.b, p...)
	return len(p), nil
}
func RunLocal(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var out limitedBuffer
	cmd.Stdout = &out
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%s não conseguiu consultar os repositórios: %w", name, err)
	}
	return out.b, nil
}
func Search(ctx context.Context, opts Options) (Result, error) {
	query, err := NormalizeQuery(opts.Query)
	if err != nil {
		return Result{}, err
	}
	run := opts.Run
	if run == nil {
		run = RunLocal
	}
	base := opts.AURBase
	if base == "" {
		base = "https://aur.archlinux.org"
	}
	client := opts.Client
	if client == nil {
		client = &http.Client{Timeout: 6 * time.Second}
	}
	output := Result{Query: query, Host: string(opts.Host), Apps: []App{}}
	var native []App
	var aur []App
	var nativeErr, aurErr error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		nativeCtx, cancel := context.WithTimeout(ctx, 9*time.Second)
		defer cancel()
		native, nativeErr = searchNative(nativeCtx, opts.Host, query, run)
	}()
	if opts.IncludeAUR {
		wg.Add(1)
		go func() {
			defer wg.Done()
			aurCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
			defer cancel()
			aur, aurErr = searchAUR(aurCtx, query, client, base)
		}()
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if nativeErr != nil {
		output.Warnings = append(output.Warnings, "Repositórios locais: "+nativeErr.Error())
	}
	if aurErr != nil {
		output.Warnings = append(output.Warnings, "AUR: "+aurErr.Error())
	}
	// Native repositories first; latest candidate is the host package
	// manager decision, which has distro/repository pinning semantics.
	rank := func(a App) int {
		q := strings.ToLower(query)
		name := strings.ToLower(a.Name)
		if name == q {
			return 0
		}
		if strings.HasPrefix(name, q) {
			return 1
		}
		if strings.Contains(name, q) {
			return 2
		}
		return 3
	}
	sort.SliceStable(native, func(i, j int) bool {
		x, y := rank(native[i]), rank(native[j])
		if x != y {
			return x < y
		}
		return native[i].Name < native[j].Name
	})
	sort.SliceStable(aur, func(i, j int) bool {
		x, y := rank(aur[i]), rank(aur[j])
		if x != y {
			return x < y
		}
		return aur[i].Name < aur[j].Name
	})
	if len(native) > MaxNative {
		native = native[:MaxNative]
	}
	if len(aur) > MaxAUR {
		aur = aur[:MaxAUR]
	}
	output.Apps = append(output.Apps, native...)
	output.Apps = append(output.Apps, aur...)
	return output, nil
}

func searchNative(ctx context.Context, host resolver.Distro, query string, run Command) ([]App, error) {
	switch host {
	case resolver.DistroDebian:
		return searchAPT(ctx, query, run)
	case resolver.DistroFedora:
		return searchDNF(ctx, query, run)
	case resolver.DistroArch:
		return searchPacman(ctx, query, run)
	case resolver.DistroAlpine:
		return searchAPK(ctx, query, run)
	default:
		return nil, fmt.Errorf("distribuição sem provedor de catálogo nativo")
	}
}
func nativeApp(name, version, desc, repo, source string) App {
	return App{ID: "native:" + name, Name: name, Version: version, Description: desc, Repository: repo, Source: source, Native: true, Installable: true}
}
func searchAPT(ctx context.Context, query string, run Command) ([]App, error) {
	out, err := run(ctx, "apt-cache", "search", regexp.QuoteMeta(query))
	if err != nil {
		return nil, err
	}
	var items []App
	seen := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		split := strings.SplitN(line, " - ", 2)
		if len(split) != 2 {
			continue
		}
		name := strings.TrimSpace(split[0])
		if !ValidPackageName(name) || seen[name] {
			continue
		}
		items = append(items, nativeApp(name, "", strings.TrimSpace(split[1]), "configured APT repositories", "APT"))
		seen[name] = true
		if len(items) >= ScanLimit {
			break
		}
	}
	if len(items) == 0 {
		return items, nil
	}
	args := []string{"policy"}
	for _, item := range items {
		args = append(args, item.Name)
	}
	policy, err := run(ctx, "apt-cache", args...)
	if err != nil {
		return items, nil
	}
	candidates := parseAPTCandidates(string(policy))
	for i := range items {
		items[i].Version = candidates[items[i].Name]
	}
	return items, nil
}
func parseAPTCandidates(raw string) map[string]string {
	out := make(map[string]string)
	current := ""
	for _, line := range strings.Split(raw, "\n") {
		if line != "" && !strings.HasPrefix(line, " ") && strings.HasSuffix(line, ":") {
			current = strings.TrimSuffix(line, ":")
		} else if current != "" {
			fields := strings.Fields(strings.TrimSpace(line))
			if len(fields) >= 2 && fields[0] == "Candidate:" && fields[1] != "(none)" {
				out[current] = fields[1]
			}
		}
	}
	return out
}
func searchPacman(ctx context.Context, query string, run Command) ([]App, error) {
	out, err := run(ctx, "pacman", "--color", "never", "-Ss", regexp.QuoteMeta(query))
	if err != nil {
		return nil, err
	}
	var apps []App
	seen := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		if line == "" || line[0] == ' ' || line[0] == '\t' {
			continue
		}
		first := strings.Fields(line)
		if len(first) < 2 {
			continue
		}
		names := strings.SplitN(first[0], "/", 2)
		if len(names) != 2 || !ValidPackageName(names[1]) || seen[names[1]] {
			continue
		}
		seen[names[1]] = true
		apps = append(apps, nativeApp(names[1], first[1], "", names[0], "pacman"))
		if len(apps) >= ScanLimit {
			break
		}
	}
	// Descriptions follow the package header in pacman output.
	// Parse again to match original repository/name ordering.
	desc := map[string]string{}
	current := ""
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			if current != "" && desc[current] == "" {
				desc[current] = strings.TrimSpace(line)
			}
			continue
		}
		fields := strings.Fields(line)
		current = ""
		if len(fields) > 0 && strings.Contains(fields[0], "/") {
			current = strings.SplitN(fields[0], "/", 2)[1]
		}
	}
	for i := range apps {
		apps[i].Description = desc[apps[i].Name]
	}
	return apps, nil
}
func searchDNF(ctx context.Context, query string, run Command) ([]App, error) {
	executable := "dnf5"
	if _, err := exec.LookPath(executable); err != nil {
		executable = "dnf"
	}
	format := "%{name}\t%{version}-%{release}\t%{summary}\t%{repoid}"
	out, err := run(ctx, executable, "repoquery", "--available", "--latest-limit=1", "--queryformat="+format, "*"+query+"*")
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var apps []App
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.SplitN(line, "\t", 4)
		if len(fields) != 4 {
			continue
		}
		name := strings.TrimSpace(fields[0])
		if !ValidPackageName(name) || seen[name] {
			continue
		}
		seen[name] = true
		apps = append(apps, nativeApp(name, fields[1], fields[2], fields[3], "DNF"))
		if len(apps) >= ScanLimit {
			break
		}
	}
	return apps, nil
}
func searchAPK(ctx context.Context, query string, run Command) ([]App, error) {
	out, err := run(ctx, "apk", "search", "-v", "--description", query)
	if err != nil {
		return nil, err
	}
	var apps []App
	seen := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.SplitN(line, " - ", 2)
		if len(f) == 0 {
			continue
		}
		matches := versionSuffix.FindStringSubmatch(strings.TrimSpace(f[0]))
		if len(matches) != 3 || !ValidPackageName(matches[1]) || seen[matches[1]] {
			continue
		}
		desc := ""
		if len(f) > 1 {
			desc = f[1]
		}
		apps = append(apps, nativeApp(matches[1], matches[2], desc, "configured APK repositories", "apk"))
		seen[matches[1]] = true
		if len(apps) >= ScanLimit {
			break
		}
	}
	return apps, nil
}

type aurResponse struct {
	Type    string `json:"type"`
	Results []struct {
		Name        string `json:"Name"`
		Version     string `json:"Version"`
		Description string `json:"Description"`
		OutOfDate   *int64 `json:"OutOfDate"`
	} `json:"results"`
}

func searchAUR(ctx context.Context, query string, client *http.Client, base string) ([]App, error) {
	endpoint := strings.TrimRight(base, "/") + "/rpc/v5/search/" + url.PathEscape(query) + "?by=name-desc"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("servidor respondeu %s", resp.Status)
	}
	var payload aurResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&payload); err != nil {
		return nil, err
	}
	if payload.Type != "search" {
		return nil, fmt.Errorf("resposta de busca inválida")
	}
	var apps []App
	for _, entry := range payload.Results {
		if !ValidPackageName(entry.Name) {
			continue
		}
		apps = append(apps, App{
			ID:   "aur:" + entry.Name,
			Name: entry.Name, Version: entry.Version, Description: entry.Description,
			Source: "AUR", Repository: "Arch User Repository",
			URL:    "https://aur.archlinux.org/packages/" + url.PathEscape(entry.Name),
			Native: false, Installable: false, OutOfDate: entry.OutOfDate != nil,
		})
		if len(apps) >= ScanLimit {
			break
		}
	}
	return apps, nil
}

package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rfdetoni/mutate/internal/resolver"
)

const (
	maxRemoteResults = 35
	maxSources       = 16
)

type Source struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Kind      string    `json:"kind"`
	Endpoint  string    `json:"endpoint"`
	Enabled   bool      `json:"enabled"`
	Builtin   bool      `json:"builtin"`
	Status    string    `json:"status,omitempty"`
	Error     string    `json:"error,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

var builtinSources = []Source{
	{ID: "aur", Name: "Arch User Repository", Kind: "aur", Endpoint: "https://aur.archlinux.org", Enabled: true, Builtin: true},
	{ID: "arch", Name: "Arch Linux · Oficial", Kind: "arch", Endpoint: "https://archlinux.org/packages/search/json/", Enabled: true, Builtin: true},
	{ID: "debian", Name: "Debian · Sources", Kind: "debian", Endpoint: "https://sources.debian.org/api/search/", Enabled: true, Builtin: true},
}

type RepositoryManager struct {
	mu         sync.RWMutex
	sources    []Source
	cache      map[string][]App
	client     *http.Client
	configPath string
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
}

func DefaultConfigPath() (string, error) {
	root := os.Getenv("XDG_CONFIG_HOME")
	if !filepath.IsAbs(root) {
		var err error
		root, err = os.UserConfigDir()
		if err != nil {
			return "", err
		}
	}
	return filepath.Join(root, "mutate", "repositories.json"), nil
}

func NewRepositoryManager(configPath string, client *http.Client) (*RepositoryManager, error) {
	if configPath == "" {
		var err error
		configPath, err = DefaultConfigPath()
		if err != nil {
			return nil, err
		}
	}
	if client == nil {
		client = &http.Client{
			Timeout:       7 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	m := &RepositoryManager{configPath: configPath, client: client, cache: make(map[string][]App), ctx: ctx, cancel: cancel}
	m.sources = append([]Source(nil), builtinSources...)
	data, err := os.ReadFile(configPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		cancel()
		return nil, err
	}
	if err == nil {
		var state struct {
			Sources []Source `json:"sources"`
		}
		if json.Unmarshal(data, &state) != nil {
			cancel()
			return nil, fmt.Errorf("configuração de repositórios inválida")
		}
		for _, item := range state.Sources {
			if item.Builtin {
				for i := range m.sources {
					if m.sources[i].ID == item.ID {
						m.sources[i].Enabled = item.Enabled
					}
				}
				continue
			}
			if len(m.sources) >= maxSources {
				break
			}
			if err := validateSource(item); err != nil {
				continue
			}
			item.Builtin = false
			item.Status = ""
			item.Error = ""
			item.UpdatedAt = time.Time{}
			m.sources = append(m.sources, item)
		}
	}
	return m, nil
}

func (m *RepositoryManager) Close() { m.cancel(); m.wg.Wait() }

func (m *RepositoryManager) Sources() []Source {
	m.mu.RLock()
	defer m.mu.RUnlock()
	copySources := append([]Source(nil), m.sources...)
	for i := range copySources {
		if !copySources[i].Enabled {
			copySources[i].Status = "disabled"
		} else if copySources[i].Status == "" {
			copySources[i].Status = "idle"
		}
	}
	return copySources
}

func validateSource(s Source) error {
	if s.Name == "" || len(s.Name) > 80 {
		return fmt.Errorf("nome obrigatório, máximo 80 caracteres")
	}
	if s.Kind != "json" {
		return fmt.Errorf("repositórios adicionais devem usar API JSON")
	}
	if err := validateRemoteURL(s.Endpoint); err != nil {
		return err
	}
	if !strings.Contains(s.Endpoint, "{query}") {
		return fmt.Errorf("o endereço deve conter {query}")
	}
	return nil
}

func validateRemoteURL(raw string) error {
	// The literal {query} is a template placeholder and never comes from search input.
	parsed, err := url.Parse(strings.ReplaceAll(raw, "{query}", "mutate"))
	if err != nil {
		return fmt.Errorf("URL inválida: %w", err)
	}
	if parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" {
		return fmt.Errorf("use um endereço HTTPS público sem credenciais ou fragmentos")
	}
	host := strings.ToLower(parsed.Hostname())
	if strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") || host == "localhost" || !strings.Contains(host, ".") {
		return fmt.Errorf("o endereço deve apontar para um domínio público")
	}
	if address, err := netip.ParseAddr(host); err == nil {
		if address.IsLoopback() || address.IsPrivate() || address.IsLinkLocalUnicast() || address.IsUnspecified() {
			return fmt.Errorf("endereços internos não podem ser usados como repositórios")
		}
		return fmt.Errorf("use um nome DNS público, não endereço IP")
	}
	return nil
}

func (m *RepositoryManager) saveLocked() error {
	data, err := json.MarshalIndent(struct {
		Sources []Source `json:"sources"`
	}{m.sources}, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(m.configPath)
	if err = os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".repositories-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err != nil {
		_ = f.Close()
		return err
	}
	if _, err = f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), m.configPath)
}

func (m *RepositoryManager) Add(name, endpoint string) (Source, error) {
	name = strings.TrimSpace(name)
	endpoint = strings.TrimSpace(endpoint)
	s := Source{Name: name, Endpoint: endpoint, Kind: "json", Enabled: true}
	if err := validateSource(s); err != nil {
		return Source{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.sources) >= maxSources {
		return Source{}, fmt.Errorf("limite de %d repositórios", maxSources)
	}
	for _, item := range m.sources {
		if strings.EqualFold(item.Endpoint, endpoint) {
			return Source{}, fmt.Errorf("repositório já cadastrado")
		}
	}
	s.ID = fmt.Sprintf("custom-%d", time.Now().UnixNano())
	m.sources = append(m.sources, s)
	if err := m.saveLocked(); err != nil {
		m.sources = m.sources[:len(m.sources)-1]
		return Source{}, err
	}
	return s, nil
}

func (m *RepositoryManager) SetEnabled(id string, enabled bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.sources {
		if m.sources[i].ID != id {
			continue
		}
		old := m.sources[i]
		m.sources[i].Enabled = enabled
		m.sources[i].Status = ""
		m.sources[i].Error = ""
		if err := m.saveLocked(); err != nil {
			m.sources[i] = old
			return err
		}
		if !enabled {
			for key := range m.cache {
				if strings.HasPrefix(key, id+"\x00") {
					delete(m.cache, key)
				}
			}
		}
		return nil
	}
	return fmt.Errorf("repositório não encontrado")
}
func (m *RepositoryManager) Remove(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.sources {
		if m.sources[i].ID != id {
			continue
		}
		if m.sources[i].Builtin {
			return fmt.Errorf("repositórios nativos não podem ser removidos")
		}
		removed := m.sources[i]
		m.sources = append(m.sources[:i], m.sources[i+1:]...)
		if err := m.saveLocked(); err != nil {
			m.sources = append(m.sources, Source{})
			copy(m.sources[i+1:], m.sources[i:])
			m.sources[i] = removed
			return err
		}
		for key := range m.cache {
			if strings.HasPrefix(key, id+"\x00") {
				delete(m.cache, key)
			}
		}
		return nil
	}
	return fmt.Errorf("repositório não encontrado")
}
func (m *RepositoryManager) updateStatus(id, status, message string, apps []App, query string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.sources {
		if m.sources[i].ID != id || !m.sources[i].Enabled {
			return
		}
		m.sources[i].Status = status
		m.sources[i].Error = message
		m.sources[i].UpdatedAt = time.Now()
		if status == "ok" && query != "" {
			m.cache[id+"\x00"+query] = append([]App(nil), apps...)
		}
		return
	}
}

// StartupRefresh begins bounded concurrent source probes without blocking the
// first render. It never modifies host package managers or apt/pacman indexes.
func (m *RepositoryManager) StartupRefresh() {
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.Refresh(m.ctx)
	}()
}

func (m *RepositoryManager) Refresh(ctx context.Context) {
	sources := m.Sources()
	var wg sync.WaitGroup
	for _, source := range sources {
		if !source.Enabled {
			continue
		}
		wg.Add(1)
		go func(source Source) {
			defer wg.Done()
			m.updateStatus(source.ID, "updating", "", nil, "")
			op, done := context.WithTimeout(ctx, 7*time.Second)
			defer done()
			apps, err := m.searchOne(op, source, "firefox")
			if err != nil {
				m.updateStatus(source.ID, "error", err.Error(), nil, "")
			} else {
				m.updateStatus(source.ID, "ok", "", apps, "firefox")
			}
		}(source)
	}
	wg.Wait()
}

func (m *RepositoryManager) Search(ctx context.Context, host resolver.Distro, query string) (Result, error) {
	normalized, err := NormalizeQuery(query)
	if err != nil {
		return Result{}, err
	}
	result := Result{Query: normalized, Host: string(host), Apps: []App{}}
	type response struct {
		source Source
		apps   []App
		err    error
	}
	sources := m.Sources()
	responses := make(chan response, len(sources)+1)
	go func() {
		local, err := searchNative(ctx, host, normalized, RunLocal)
		responses <- response{source: Source{ID: "native", Name: "Repositórios do sistema"}, apps: local, err: err}
	}()
	count := 1
	for _, source := range sources {
		if !source.Enabled {
			continue
		}
		count++
		go func(s Source) {
			op, done := context.WithTimeout(ctx, 7*time.Second)
			defer done()
			apps, err := m.searchOne(op, s, normalized)
			responses <- response{source: s, apps: apps, err: err}
		}(source)
	}
	var local []App
	var external []App
	for i := 0; i < count; i++ {
		select {
		case <-ctx.Done():
			return Result{}, ctx.Err()
		case item := <-responses:
			if item.source.ID == "native" {
				if item.err != nil {
					result.Warnings = append(result.Warnings, "Repositórios nativos: "+item.err.Error())
				} else {
					local = item.apps
				}
				continue
			}
			if item.err != nil {
				m.mu.RLock()
				cached := append([]App(nil), m.cache[item.source.ID+"\x00"+normalized]...)
				m.mu.RUnlock()
				if len(cached) > 0 {
					result.Warnings = append(result.Warnings, item.source.Name+": indisponível; usando resultado anterior")
					external = append(external, cached...)
				} else {
					result.Warnings = append(result.Warnings, item.source.Name+": "+item.err.Error())
				}
				m.updateStatus(item.source.ID, "error", item.err.Error(), nil, "")
			} else {
				m.updateStatus(item.source.ID, "ok", "", item.apps, normalized)
				external = append(external, item.apps...)
			}
		}
	}
	sort.SliceStable(local, func(i, j int) bool { return rankApp(normalized, local[i]) < rankApp(normalized, local[j]) })
	sort.SliceStable(external, func(i, j int) bool {
		x, y := rankApp(normalized, external[i]), rankApp(normalized, external[j])
		if x != y {
			return x < y
		}
		if external[i].Name != external[j].Name {
			return external[i].Name < external[j].Name
		}
		return external[i].Source < external[j].Source
	})
	if len(local) > MaxNative {
		local = local[:MaxNative]
	}
	if len(external) > 80 {
		external = external[:80]
	}
	result.Apps = append(result.Apps, local...)
	result.Apps = append(result.Apps, external...)
	return result, nil
}
func rankApp(query string, item App) int {
	q := strings.ToLower(query)
	name := strings.ToLower(item.Name)
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

func (m *RepositoryManager) searchOne(ctx context.Context, source Source, query string) ([]App, error) {
	var endpoint string
	switch source.Kind {
	case "aur":
		return searchAUR(ctx, query, m.client, source.Endpoint)
	case "arch":
		endpoint = source.Endpoint + "?q=" + url.QueryEscape(query) + "&arch=" + url.QueryEscape(hostArch()) + "&arch=any"
	case "debian":
		endpoint = source.Endpoint + url.PathEscape(query) + "/"
	case "json":
		if err := validateSource(source); err != nil {
			return nil, err
		}
		endpoint = strings.ReplaceAll(source.Endpoint, "{query}", url.QueryEscape(query))
	default:
		return nil, fmt.Errorf("formato de repositório não suportado")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "Mutate/1.0 (repository discovery)")
	resp, err := m.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var items []App
	switch source.Kind {
	case "arch":
		var payload struct {
			Results []struct {
				Name        string `json:"pkgname"`
				Version     string `json:"pkgver"`
				Release     string `json:"pkgrel"`
				Description string `json:"pkgdesc"`
				Repository  string `json:"repo"`
				Arch        string `json:"arch"`
			} `json:"results"`
		}
		if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&payload); err != nil {
			return nil, err
		}
		for _, entry := range payload.Results {
			if !ValidPackageName(entry.Name) {
				continue
			}
			version := entry.Version
			if entry.Release != "" {
				version += "-" + entry.Release
			}
			items = append(items, App{
				ID:   source.ID + ":" + entry.Name,
				Name: entry.Name, Version: version, Description: entry.Description,
				Repository: entry.Repository, Source: source.Name,
				URL: "https://archlinux.org/packages/?name=" + url.QueryEscape(entry.Name),
			})
			if len(items) >= maxRemoteResults {
				break
			}
		}
	case "debian":
		var payload struct {
			Results struct {
				Exact *struct {
					Name string `json:"name"`
				} `json:"exact"`
				Other []struct {
					Name string `json:"name"`
				} `json:"other"`
			} `json:"results"`
		}
		if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&payload); err != nil {
			return nil, err
		}
		names := []string{}
		if payload.Results.Exact != nil {
			names = append(names, payload.Results.Exact.Name)
		}
		for _, item := range payload.Results.Other {
			names = append(names, item.Name)
		}
		for _, name := range names {
			if !ValidPackageName(name) {
				continue
			}
			items = append(items, App{
				ID: source.ID + ":" + name, Name: name, Source: source.Name,
				Description: "Pacote-fonte Debian · consulte versão e distribuição no catálogo",
				Repository:  "Debian Sources",
				URL:         "https://sources.debian.org/src/" + url.PathEscape(name) + "/",
			})
			if len(items) >= maxRemoteResults {
				break
			}
		}
	case "json":
		var payload struct {
			Results []struct {
				Name        string `json:"name"`
				Version     string `json:"version"`
				Description string `json:"description"`
				URL         string `json:"url"`
			} `json:"results"`
		}
		if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&payload); err != nil {
			return nil, err
		}
		for _, entry := range payload.Results {
			if !ValidPackageName(entry.Name) {
				continue
			}
			link := ""
			if u, err := url.Parse(entry.URL); err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil {
				link = u.String()
			}
			items = append(items, App{
				ID: source.ID + ":" + entry.Name, Name: entry.Name, Version: entry.Version,
				Description: entry.Description, URL: link, Source: source.Name, Repository: source.Name,
			})
			if len(items) >= maxRemoteResults {
				break
			}
		}
	}
	return items, nil
}

// Prevent accidental use of the local network as a source endpoint.
func ValidateCustomRemote(name, endpoint string) error {
	s := Source{Name: name, Endpoint: endpoint, Kind: "json"}
	if err := validateSource(s); err != nil {
		return err
	}
	// DNS check guards typical private/intranet endpoints. Redirects are disabled
	// in the default client. Results are metadata-only and never executed.
	u, _ := url.Parse(strings.ReplaceAll(endpoint, "{query}", "mutate"))
	lookupCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	addresses, err := net.DefaultResolver.LookupNetIP(lookupCtx, "ip", u.Hostname())
	if err != nil {
		return fmt.Errorf("não foi possível resolver o endereço do repositório: %w", err)
	}
	for _, addr := range addresses {
		if addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() || addr.IsUnspecified() {
			return fmt.Errorf("endereço DNS resolve para rede local")
		}
	}
	return nil
}

// GOARCH conversion kept at package boundary for remote architecture filter.
func hostArch() string {
	switch runtime.GOARCH {
	case "amd64":
		return "x86_64"
	case "arm64":
		return "aarch64"
	case "386":
		return "i686"
	}
	return runtime.GOARCH
}

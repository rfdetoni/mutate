package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rfdetoni/mutate/internal/catalog"
	"github.com/rfdetoni/mutate/internal/detector"
	"github.com/rfdetoni/mutate/internal/domain"
	"github.com/rfdetoni/mutate/internal/resolver"
	"github.com/rfdetoni/mutate/internal/unpacker"
)

const guiMaxUpload int64 = 2 << 30 // 2 GiB per package; limits transient disk use.

type guiPackage struct {
	Path     string          `json:"-"`
	Name     string          `json:"file_name"`
	Size     int64           `json:"size"`
	Format   domain.Format   `json:"format"`
	Metadata domain.Metadata `json:"metadata"`
}

type guiJob struct {
	ID                string    `json:"id"`
	Operation         string    `json:"operation"`
	State             string    `json:"state"`
	Phase             string    `json:"phase"`
	Message           string    `json:"message,omitempty"`
	OutputID          string    `json:"output_id,omitempty"`
	OutputName        string    `json:"output_name,omitempty"`
	ExternalLibraries []string  `json:"external_libraries,omitempty"`
	ELFs              int       `json:"elf_count,omitempty"`
	StartedAt         time.Time `json:"started_at"`
}

type guiHistory struct {
	Operation  string    `json:"operation"`
	Name       string    `json:"name"`
	Target     string    `json:"target,omitempty"`
	State      string    `json:"state"`
	OutputID   string    `json:"output_id,omitempty"`
	OutputName string    `json:"output_name,omitempty"`
	At         time.Time `json:"at"`
}

type guiOutput struct{ Path, Name string }

type guiApp struct {
	mu        sync.Mutex
	token     string
	address   string
	workspace string
	selected  *guiPackage
	job       *guiJob
	history   []guiHistory
	outputs   map[string]guiOutput
	catalog   map[string]catalog.App
 repositories *catalog.RepositoryManager
	busy      bool
	shutdown  func()
}

func newGUIApp() (*guiApp, error) {
	token, err := newSessionToken()
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp("", "mutate-gui-*")
	if err != nil {
		return nil, fmt.Errorf("create GUI workspace: %w", err)
	}
	if err := os.Chmod(dir, 0700); err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	repoManager, err := catalog.NewRepositoryManager("", nil)
 if err != nil {
  _ = os.RemoveAll(dir)
  return nil, fmt.Errorf("inicializar catálogos: %w", err)
 }
  return &guiApp{
  token: token, workspace: dir, outputs: make(map[string]guiOutput),
  catalog: make(map[string]catalog.App), repositories: repoManager,
 }, nil
}
func (a *guiApp) close() {
 if a.repositories != nil {a.repositories.Close()}
 _ = os.RemoveAll(a.workspace)
}

func guiJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func guiError(w http.ResponseWriter, status int, message string) {
	guiJSON(w, status, map[string]string{"error": message})
}
func (a *guiApp) handler() http.Handler {
	assets, _ := guiFileSystem()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/state", a.handleState)
	mux.HandleFunc("GET /api/catalog", a.handleCatalogSearch)
 mux.HandleFunc("GET /api/repositories", a.handleRepositoriesList)
 mux.HandleFunc("POST /api/repositories", a.handleRepositoriesCreate)
 mux.HandleFunc("POST /api/repositories/{id}/enabled", a.handleRepositoriesToggle)
 mux.HandleFunc("DELETE /api/repositories/{id}", a.handleRepositoriesDelete)
 mux.HandleFunc("POST /api/repositories/refresh", a.handleRepositoriesRefresh)
	mux.HandleFunc("POST /api/catalog/install", a.handleCatalogInstall)
	mux.HandleFunc("POST /api/upload", a.handleUpload)
	mux.HandleFunc("POST /api/action", a.handleAction)
	mux.HandleFunc("GET /api/download/{id}", a.handleDownload)
	mux.HandleFunc("POST /api/quit", a.handleQuit)
	mux.Handle("GET /", http.FileServer(http.FS(assets)))

	prefix := "/" + a.token
	inner := http.StripPrefix(prefix, mux)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self' data:; font-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")
		if a.address != "" && r.Host != a.address {
			guiError(w, http.StatusForbidden, "Invalid local host")
			return
		}
		if r.URL.Path == prefix {
			http.Redirect(w, r, prefix+"/", http.StatusTemporaryRedirect)
			return
		}
		if !strings.HasPrefix(r.URL.Path, prefix+"/") {
			http.NotFound(w, r)
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			if r.Header.Get("X-Mutate-Session") != a.token {
				guiError(w, http.StatusForbidden, "Invalid GUI session")
				return
			}
			if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+a.address {
				guiError(w, http.StatusForbidden, "Cross-origin requests are not allowed")
				return
			}
		}
		inner.ServeHTTP(w, r)
	})
}

func (a *guiApp) state() any {
	a.mu.Lock()
	var selected *guiPackage
	if a.selected != nil {
		copySelected := *a.selected
		copySelected.Metadata.Dependencies = append([]string(nil), a.selected.Metadata.Dependencies...)
		copySelected.Metadata.Recommends = append([]string(nil), a.selected.Metadata.Recommends...)
		selected = &copySelected
	}
	var job *guiJob
	if a.job != nil {
		copyJob := *a.job
		copyJob.ExternalLibraries = append([]string(nil), a.job.ExternalLibraries...)
		job = &copyJob
	}
	history := append([]guiHistory(nil), a.history...)
	busy := a.busy
	a.mu.Unlock()
	host := resolver.DetectHostDistro()
	return struct {
		Selected     *guiPackage  `json:"selected"`
		Job          *guiJob      `json:"job"`
		History      []guiHistory `json:"history"`
		Busy         bool         `json:"busy"`
		Host         string       `json:"host"`
		NativeTarget string       `json:"native_target"`
		MaxUpload    int64        `json:"max_upload"`
	}{selected, job, history, busy, string(host), nativeGUITarget(host), guiMaxUpload}
}
func (a *guiApp) handleState(w http.ResponseWriter, r *http.Request) {
	guiJSON(w, http.StatusOK, a.state())
}

func cleanUploadName(raw string) string {
	name := path.Base(strings.ReplaceAll(raw, "\\", "/"))
	if name == "." || name == "/" || name == ".." {
		return ""
	}
	if len(name) > 140 {
		name = name[len(name)-140:]
	}
	var out strings.Builder
	for _, ch := range name {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_' || ch == '-' || ch == '.' || ch == '+' {
			out.WriteRune(ch)
		} else {
			out.WriteByte('_')
		}
	}
	value := strings.TrimLeft(out.String(), ".")
	if value == "" || value == ".." {
		return ""
	}
	return value
}

func inspectGUIFile(filename string, format domain.Format) (domain.Metadata, error) {
	switch format {
	case domain.FormatDEB, domain.FormatRPM, domain.FormatArch, domain.FormatAlpineAPK, domain.FormatTarball:
		return unpacker.InspectMetadata(filename, format)
	default:
		stage, err := os.MkdirTemp("", "mutate-gui-preview-*")
		if err != nil {
			return domain.Metadata{}, err
		}
		defer os.RemoveAll(stage)
		ir, err := unpacker.Prepare(filename, format, stage, unpacker.PrepareOptions{})
		if err != nil {
			return domain.Metadata{}, err
		}
		return ir.Metadata, nil
	}
}

func (a *guiApp) handleUpload(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	if a.busy {
		a.mu.Unlock()
		guiError(w, http.StatusConflict, "Aguarde a operação em andamento")
		return
	}
	a.busy = true
	a.mu.Unlock()
	defer func() { a.mu.Lock(); a.busy = false; a.mu.Unlock() }()

	r.Body = http.MaxBytesReader(w, r.Body, guiMaxUpload+(4<<20))
	reader, err := r.MultipartReader()
	if err != nil {
		guiError(w, 400, "Envie um arquivo pelo seletor ou arraste-o para a área indicada")
		return
	}
	part, err := reader.NextPart()
	if err != nil || part.FormName() != "file" {
		guiError(w, 400, "Arquivo ausente")
		return
	}
	name := cleanUploadName(part.FileName())
	if name == "" {
		guiError(w, 400, "Nome de arquivo inválido")
		return
	}

	uploadDir, err := os.MkdirTemp(a.workspace, "upload-*")
	if err != nil {
		guiError(w, 500, "Não foi possível preparar o arquivo")
		return
	}
	valid := false
	defer func() {
		if !valid {
			_ = os.RemoveAll(uploadDir)
		}
	}()
	filename := filepath.Join(uploadDir, name)
	out, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		guiError(w, 500, "Não foi possível salvar o arquivo")
		return
	}
	size, copyErr := io.Copy(out, io.LimitReader(part, guiMaxUpload+1))
	closeErr := out.Close()
	if copyErr != nil || closeErr != nil {
		guiError(w, 400, "Falha ao receber arquivo")
		return
	}
	if size > guiMaxUpload {
		guiError(w, http.StatusRequestEntityTooLarge, "O arquivo excede 2 GiB")
		return
	}
	if size == 0 {
		guiError(w, 400, "O arquivo está vazio")
		return
	}

	format, err := detector.DetectFile(filename)
	if err != nil {
		guiError(w, 422, "Formato não reconhecido ou pacote inválido: "+err.Error())
		return
	}
	metadata, err := inspectGUIFile(filename, format)
	if err != nil {
		guiError(w, 422, "Não foi possível ler o pacote: "+err.Error())
		return
	}
	valid = true
	a.mu.Lock()
	previous := a.selected
	a.selected = &guiPackage{Path: filename, Name: name, Size: size, Format: format, Metadata: metadata}
	a.job = nil
	a.mu.Unlock()
	if previous != nil {
		_ = os.RemoveAll(filepath.Dir(previous.Path))
	}
	guiJSON(w, http.StatusOK, a.state())
}

func (a *guiApp) handleDownload(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	a.mu.Lock()
	file, ok := a.outputs[id]
	a.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", file.Name))
	http.ServeFile(w, r, file.Path)
}

func (a *guiApp) handleQuit(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	busy := a.busy
	a.mu.Unlock()
	if busy {
		guiError(w, 409, "Aguarde a conclusão da operação")
		return
	}
	guiJSON(w, 200, map[string]bool{"closing": true})
	if a.shutdown != nil {
		go a.shutdown()
	}
}

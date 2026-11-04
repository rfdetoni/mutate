package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"time"

	"github.com/rfdetoni/mutate/internal/catalog"
	"github.com/rfdetoni/mutate/internal/resolver"
)

func (a *guiApp) handleCatalogSearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if _, err := catalog.NormalizeQuery(query); err != nil {
		guiError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 16*time.Second)
	defer cancel()
	type fpResult struct { apps []catalog.App; err error }
	flatpakCh := make(chan fpResult, 1)
	go func() {
		if _, err := exec.LookPath("flatpak"); err != nil {
			flatpakCh <- fpResult{err: catalog.ErrFlathubUnavailable}
			return
		}
		op, done := context.WithTimeout(ctx, 9*time.Second)
		defer done()
		apps, err := catalog.SearchFlatpak(op, query, nil)
		flatpakCh <- fpResult{apps: apps, err: err}
	}()
	result, err := a.repositories.Search(ctx, resolver.DetectHostDistro(), query)
	if err != nil {
		guiError(w, http.StatusBadGateway, "Falha na consulta dos repositórios: "+err.Error())
		return
	}
	fp := <-flatpakCh
	if fp.err == nil {
		count := 0
		for count < len(result.Apps) && result.Apps[count].Native { count++ }
		all := make([]catalog.App, 0, len(result.Apps)+len(fp.apps))
		all = append(all, result.Apps[:count]...)
		all = append(all, fp.apps...)
		all = append(all, result.Apps[count:]...)
		result.Apps = all
	} else if !errors.Is(fp.err, catalog.ErrFlathubUnavailable) {
		result.Warnings = append(result.Warnings, "Flathub: "+fp.err.Error())
	}
	if len(result.Apps) == 0 && errors.Is(fp.err, catalog.ErrFlathubUnavailable) {
		result.Warnings = append(result.Warnings,
			"Configure Flatpak e o Flathub oficial para alternativas instaláveis.")
	}
	a.mu.Lock()
	// The last search authorizes only signed host packages and trusted Flatpak IDs.
	a.catalog = make(map[string]catalog.App, len(result.Apps))
	for _, entry := range result.Apps {
		_, flatpak := catalog.FlatpakApplicationID(entry)
		if entry.Installable && ((entry.Native && catalog.ValidPackageName(entry.Name)) || flatpak) {
			a.catalog[entry.ID] = entry
		}
	}
	a.mu.Unlock()
	guiJSON(w, http.StatusOK, result)
}

func (a *guiApp) handleCatalogInstall(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	var req struct {
		ID string `json:"id"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		guiError(w, 400, "Seleção inválida")
		return
	}
	a.mu.Lock()
	if a.busy {
		a.mu.Unlock()
		guiError(w, 409, "Aguarde a operação em andamento")
		return
	}
	entry, ok := a.catalog[req.ID]
	_, flatpak := catalog.FlatpakApplicationID(entry)
	if !ok || !entry.Installable || !((entry.Native && catalog.ValidPackageName(entry.Name)) || flatpak) {
		a.mu.Unlock()
		guiError(w, 403, "Pacote indisponível ou fonte não autorizada nesta sessão")
		return
	}
	id, err := newSessionToken()
	if err != nil {
		a.mu.Unlock()
		guiError(w, 500, "Não foi possível iniciar a instalação")
		return
	}
	a.busy = true
	a.job = &guiJob{
		ID: id, Operation: "catalog-install", State: "running",
		Phase: "Aguardando autorização de administrador", StartedAt: time.Now(),
	}
	a.mu.Unlock()
	go a.runCatalogInstall(entry, id)
	guiJSON(w, 202, map[string]string{"job_id": id})
}

func (a *guiApp) runCatalogInstall(entry catalog.App, id string) {
	var err error
	via := "repositório nativo"
	if appID, ok := catalog.FlatpakApplicationID(entry); ok {
		via = "Flathub/Flatpak"
		err = installFlatpakCatalogPackage(appID)
	} else {
		err = installNativeCatalogPackage(entry.Name, resolver.DetectHostDistro())
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.job == nil || a.job.ID != id {
		return
	}
	if err != nil {
		a.job.State = "error"
		a.job.Message = err.Error()
		a.job.Phase = "Não concluído"
	} else {
		a.job.State = "completed"
		a.job.Message = "Instalado via " + via + ": " + entry.Name
		a.job.Phase = "Instalado"
	}
	a.busy = false
	a.history = append([]guiHistory{{
		Operation: "catalog-install", Name: entry.Name, Target: string(resolver.DetectHostDistro()),
		State: a.job.State, At: time.Now(),
	}}, a.history...)
	if len(a.history) > 20 {
		a.history = a.history[:20]
	}
}

// Flatpak downloads and verifies remote packages; no AUR/source scripts run.
func installFlatpakCatalogPackage(appID string) error {
	if !catalog.ValidFlatpakID(appID) { return fmt.Errorf("ID Flatpak inválido") }
	if _, err := exec.LookPath("flatpak"); err != nil {
		return fmt.Errorf("Flatpak indisponível: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	scope, err := catalog.FlathubScope(ctx, nil)
	if err != nil { return err }
	cmd := exec.CommandContext(ctx, "flatpak", "install", scope, "--assumeyes",
		"--noninteractive", "--app", "flathub", appID)
	var output cappedGUIBuffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil { return fmt.Errorf("tempo limite da instalação Flatpak excedido") }
		return fmt.Errorf("instalação do Flathub: %s: %w", output.String(), err)
	}
	return nil
}

func installNativeCatalogPackage(name string, host resolver.Distro) error {
	if !catalog.ValidPackageName(name) {
		return fmt.Errorf("nome de pacote inválido")
	}
	var manager string
	var args []string
	switch host {
	case resolver.DistroDebian:
		manager = "apt-get"
		args = []string{"install", "-y", name}
	case resolver.DistroFedora:
		manager = "dnf5"
		if _, err := exec.LookPath(manager); err != nil {
			manager = "dnf"
		}
		args = []string{"install", "-y", name}
	case resolver.DistroArch:
		manager = "pacman"
		args = []string{"-S", "--noconfirm", name}
	case resolver.DistroAlpine:
		manager = "apk"
		args = []string{"add", name}
	default:
		return fmt.Errorf("distribuição não suportada")
	}
	// The system package manager still resolves dependencies and chooses
	// the current candidate from enabled repositories; versions in search
	// results are informational and never used as untrusted command flags.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	if err := runGUIPrivileged(ctx, manager, args...); err != nil {
		return fmt.Errorf("falha na instalação de %s: %w", name, err)
	}
	return nil
}

func (a *guiApp) handleRepositoriesList(w http.ResponseWriter, _ *http.Request) {
	guiJSON(w, http.StatusOK, map[string]any{"sources": a.repositories.Sources()})
}

func (a *guiApp) handleRepositoriesCreate(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	var payload struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		guiError(w, 400, "Dados inválidos")
		return
	}
	if err := catalog.ValidateCustomRemote(payload.Name, payload.URL); err != nil {
		guiError(w, 400, err.Error())
		return
	}
	source, err := a.repositories.Add(payload.Name, payload.URL)
	if err != nil {
		guiError(w, 400, err.Error())
		return
	}
	a.repositories.StartupRefresh()
	guiJSON(w, 201, source)
}

func (a *guiApp) handleRepositoriesToggle(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	var payload struct {
		Enabled *bool `json:"enabled"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil || payload.Enabled == nil {
		guiError(w, 400, "Informe o estado do repositório")
		return
	}
	if err := a.repositories.SetEnabled(r.PathValue("id"), *payload.Enabled); err != nil {
		guiError(w, 400, err.Error())
		return
	}
	if *payload.Enabled {
		a.repositories.StartupRefresh()
	}
	guiJSON(w, 200, map[string]bool{"updated": true})
}

func (a *guiApp) handleRepositoriesDelete(w http.ResponseWriter, r *http.Request) {
	if err := a.repositories.Remove(r.PathValue("id")); err != nil {
		guiError(w, 400, err.Error())
		return
	}
	guiJSON(w, 200, map[string]bool{"removed": true})
}

func (a *guiApp) handleRepositoriesRefresh(w http.ResponseWriter, _ *http.Request) {
	a.repositories.StartupRefresh()
	guiJSON(w, 202, map[string]bool{"refreshing": true})
}

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"time"

	"github.com/rfdetoni/mutate/internal/catalog"
	"github.com/rfdetoni/mutate/internal/resolver"
)

func (a *guiApp) handleCatalogSearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
    result, err := a.repositories.Search(ctx, resolver.DetectHostDistro(), query)
	if err != nil {
		if _, e := catalog.NormalizeQuery(query); e != nil {
			guiError(w, 400, e.Error())
			return
		}
		guiError(w, 502, "Falha na consulta dos repositórios: "+err.Error())
		return
	}
	a.mu.Lock()
	// Only results in the most recent search can be installed, and only
	// native packages obtained from the configured signed host repositories.
	a.catalog = make(map[string]catalog.App, len(result.Apps))
	for _, entry := range result.Apps {
		if entry.Native && entry.Installable && catalog.ValidPackageName(entry.Name) {
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
	if !ok || !entry.Native || !entry.Installable || !catalog.ValidPackageName(entry.Name) {
		a.mu.Unlock()
		guiError(w, 403, "O pacote não está disponível nos repositórios nativos desta sessão")
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
	err := installNativeCatalogPackage(entry.Name, resolver.DetectHostDistro())
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
		a.job.Message = "Instalado via repositório nativo: " + entry.Name
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
 guiJSON(w, http.StatusOK, map[string]any{"sources":a.repositories.Sources()})
}

func (a *guiApp) handleRepositoriesCreate(w http.ResponseWriter, r *http.Request) {
 r.Body=http.MaxBytesReader(w,r.Body,16<<10)
 var payload struct {
  Name string `json:"name"`
  URL string `json:"url"`
 }
 decoder:=json.NewDecoder(r.Body)
 decoder.DisallowUnknownFields()
 if err:=decoder.Decode(&payload);err!=nil{guiError(w,400,"Dados inválidos");return}
 if err:=catalog.ValidateCustomRemote(payload.Name,payload.URL);err!=nil{guiError(w,400,err.Error());return}
 source,err:=a.repositories.Add(payload.Name,payload.URL)
 if err!=nil{guiError(w,400,err.Error());return}
 guiJSON(w,201,source)
}

func (a *guiApp) handleRepositoriesToggle(w http.ResponseWriter,r *http.Request){
 r.Body=http.MaxBytesReader(w,r.Body,4<<10)
 var payload struct{Enabled *bool `json:"enabled"`}
 decoder:=json.NewDecoder(r.Body)
 decoder.DisallowUnknownFields()
 if err:=decoder.Decode(&payload);err!=nil||payload.Enabled==nil{
  guiError(w,400,"Informe o estado do repositório");return
 }
 if err:=a.repositories.SetEnabled(r.PathValue("id"),*payload.Enabled);err!=nil{
  guiError(w,400,err.Error());return
 }
 guiJSON(w,200,map[string]bool{"updated":true})
}

func (a *guiApp) handleRepositoriesDelete(w http.ResponseWriter,r *http.Request){
 if err:=a.repositories.Remove(r.PathValue("id"));err!=nil{
  guiError(w,400,err.Error());return
 }
 guiJSON(w,200,map[string]bool{"removed":true})
}

func (a *guiApp) handleRepositoriesRefresh(w http.ResponseWriter,_ *http.Request){
 a.repositories.StartupRefresh()
 guiJSON(w,202,map[string]bool{"refreshing":true})
}

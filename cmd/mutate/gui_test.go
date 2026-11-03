package main

import (
	"bytes"
	"encoding/json"
	"github.com/rfdetoni/mutate/internal/catalog"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func guiTestRequest(app *guiApp, method, endpoint string, body io.Reader) *http.Request {
	request := httptest.NewRequest(method, "http://127.0.0.1/"+app.token+endpoint, body)
	request.Host = "127.0.0.1"
	if method == "POST" {
		request.Header.Set("X-Mutate-Session", app.token)
		request.Header.Set("Origin", "http://127.0.0.1")
	}
	return request
}

func TestGUIRejectsWrongOriginAndSession(t *testing.T) {
	app, err := newGUIApp()
	if err != nil {
		t.Fatal(err)
	}
	defer app.close()
	app.address = "127.0.0.1"
	h := app.handler()
	for _, request := range []*http.Request{
		httptest.NewRequest("GET", "http://127.0.0.1/", nil),
		httptest.NewRequest("GET", "http://untrusted.invalid/"+app.token+"/", nil),
	} {
		recorder := httptest.NewRecorder()
		h.ServeHTTP(recorder, request)
		if recorder.Code == 200 {
			t.Fatalf("unauthorized request accepted: %s", request.URL)
		}
	}
	unauth := guiTestRequest(app, "POST", "/api/action", strings.NewReader("{}"))
	unauth.Header.Del("X-Mutate-Session")
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, unauth)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("missing session allowed: %d", recorder.Code)
	}
	foreign := guiTestRequest(app, "POST", "/api/action", strings.NewReader("{}"))
	foreign.Header.Set("Origin", "https://untrusted.invalid")
	recorder = httptest.NewRecorder()
	h.ServeHTTP(recorder, foreign)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("cross origin allowed: %d", recorder.Code)
	}
}

func TestGUIEmbeddedAssetsAndSecurityHeaders(t *testing.T) {
	app, err := newGUIApp()
	if err != nil {
		t.Fatal(err)
	}
	defer app.close()
	app.address = "127.0.0.1"
	for _, asset := range []string{"/", "/style.css", "/app.js"} {
		recorder := httptest.NewRecorder()
		app.handler().ServeHTTP(recorder, guiTestRequest(app, "GET", asset, nil))
		if recorder.Code != 200 {
			t.Fatalf("asset %q: %d: %s", asset, recorder.Code, recorder.Body.String())
		}
		if recorder.Header().Get("Content-Security-Policy") == "" {
			t.Fatal("missing CSP")
		}
		if recorder.Header().Get("Referrer-Policy") != "no-referrer" {
			t.Fatal("referrer policy missing")
		}
	}
}

func TestGUICleanNameRejectsTraversal(t *testing.T) {
	for _, candidate := range []string{"../../example.deb", "..\\..\\example.deb"} {
		if got := cleanUploadName(candidate); got != "example.deb" {
			t.Fatalf("unsafe file name %q mapped to %q", candidate, got)
		}
	}
	if got := cleanUploadName(".."); got != "" {
		t.Fatalf("accepted invalid filename %q", got)
	}
}

func guiUploadTestFile(t *testing.T, app *guiApp, h http.Handler) {
	t.Helper()
	filename := createSampleDeb(t)
	defer os.Remove(filename)
	payload, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	filePart, err := writer.CreateFormFile("file", "package.deb")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := filePart.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := guiTestRequest(app, "POST", "/api/upload", &buffer)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, req)
	if recorder.Code != 200 {
		t.Fatalf("upload: %d %s", recorder.Code, recorder.Body.String())
	}
	if app.selected == nil || app.selected.Metadata.Name != "sample" {
		t.Fatalf("metadata missing: %+v", app.selected)
	}
}

func guiSubmitTestAction(t *testing.T, app *guiApp, h http.Handler, payload string) guiJob {
	t.Helper()
	req := guiTestRequest(app, "POST", "/api/action", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, req)
	if recorder.Code != 202 {
		t.Fatalf("action not accepted: %d %s", recorder.Code, recorder.Body.String())
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		app.mu.Lock()
		finished := app.job != nil && app.job.State != "running"
		var result guiJob
		if app.job != nil {
			result = *app.job
		}
		app.mu.Unlock()
		if finished {
			return result
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("GUI operation timed out")
	return guiJob{}
}

func TestGUIUploadInspectConvertAndDownload(t *testing.T) {
	app, err := newGUIApp()
	if err != nil {
		t.Fatal(err)
	}
	defer app.close()
	app.address = "127.0.0.1"
	h := app.handler()
	guiUploadTestFile(t, app, h)
	inspection := guiSubmitTestAction(t, app, h, `{"action":"inspect","resolve":false}`)
	if inspection.State != "completed" {
		t.Fatalf("inspection failed: %s", inspection.Message)
	}
	converted := guiSubmitTestAction(t, app, h, `{"action":"convert","target":"deb"}`)
	if converted.State != "completed" || converted.OutputID == "" {
		t.Fatalf("conversion failed: %+v", converted)
	}
	req := guiTestRequest(app, "GET", "/api/download/"+converted.OutputID, nil)
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, req)
	if recorder.Code != 200 || recorder.Body.Len() == 0 {
		t.Fatalf("download failed: %d", recorder.Code)
	}
	if !strings.Contains(recorder.Header().Get("Content-Disposition"), ".deb") {
		t.Fatal("download filename missing")
	}
	if len(app.history) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(app.history))
	}
}

func TestGUIBusyAndInvalidUpload(t *testing.T) {
	app, err := newGUIApp()
	if err != nil {
		t.Fatal(err)
	}
	defer app.close()
	app.address = "127.0.0.1"
	app.mu.Lock()
	app.busy = true
	app.mu.Unlock()
	h := app.handler()
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, guiTestRequest(app, "POST", "/api/upload", strings.NewReader("bad")))
	if recorder.Code != 409 {
		t.Fatalf("busy upload not rejected: %d", recorder.Code)
	}
	app.mu.Lock()
	app.busy = false
	app.mu.Unlock()
	recorder = httptest.NewRecorder()
	request := guiTestRequest(app, "POST", "/api/action", strings.NewReader(`{"action":"convert","target":"deb"}`))
	h.ServeHTTP(recorder, request)
	if recorder.Code != 409 {
		t.Fatalf("conversion without input accepted: %d", recorder.Code)
	}
}

func TestGUIStateDoesNotLeakPaths(t *testing.T) {
	app, err := newGUIApp()
	if err != nil {
		t.Fatal(err)
	}
	defer app.close()
	app.address = "127.0.0.1"
	app.selected = &guiPackage{Path: filepath.Join(app.workspace, "secret"), Name: "safe.deb"}
	recorder := httptest.NewRecorder()
	app.handler().ServeHTTP(recorder, guiTestRequest(app, "GET", "/api/state", nil))
	var decoded map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(recorder.Body.String(), app.workspace) {
		t.Fatal("internal filesystem path leaked to UI")
	}
}

func TestGUIRejectsDependencyMapFilePaths(t *testing.T) {
	app, err := newGUIApp()
	if err != nil {
		t.Fatal(err)
	}
	defer app.close()
	app.address = "127.0.0.1"
	guiUploadTestFile(t, app, app.handler())
	request := guiTestRequest(app, "POST", "/api/action",
		strings.NewReader(`{"action":"convert","target":"deb","dependency_map":"/etc/passwd"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	app.handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("GUI accepted a host path as dependency JSON: %d %s",
			recorder.Code, recorder.Body.String())
	}
}

func TestGUICatalogRejectsUntrustedInstallAndInvalidQueries(t *testing.T) {
	app, err := newGUIApp()
	if err != nil {
		t.Fatal(err)
	}
	defer app.close()
	app.address = "127.0.0.1"
	app.catalog["aur:community-tool"] = catalog.App{
		ID: "aur:community-tool", Name: "community-tool", Source: "AUR",
		Installable: false, Native: false,
	}
	for _, id := range []string{"aur:community-tool", "native:missing"} {
		body := strings.NewReader(`{"id":"` + id + `"}`)
		req := guiTestRequest(app, "POST", "/api/catalog/install", body)
		req.Header.Set("Content-Type", "application/json")
		out := httptest.NewRecorder()
		app.handler().ServeHTTP(out, req)
		if out.Code != http.StatusForbidden {
			t.Fatalf("untrusted or unknown package %s allowed: %d %s", id, out.Code, out.Body.String())
		}
	}
	req := guiTestRequest(app, "GET", "/api/catalog?q=-v&aur=0", nil)
	out := httptest.NewRecorder()
	app.handler().ServeHTTP(out, req)
	if out.Code != http.StatusBadRequest {
		t.Fatalf("invalid catalog query accepted: %d %s", out.Code, out.Body.String())
	}
}

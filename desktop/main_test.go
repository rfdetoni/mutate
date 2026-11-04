package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewClientAcceptsOnlyAuthenticatedLoopback(t *testing.T) {
	token := strings.Repeat("a", 48)
	good := "http://127.0.0.1:39241/" + token + "/"
	c, err := newClient(good)
	if err != nil || c.token != token {
		t.Fatalf("valid session refused: %v", err)
	}
	for _, candidate := range []string{
		"https://127.0.0.1:39241/" + token + "/",
		"http://localhost:39241/" + token + "/",
		"http://192.168.1.1:39241/" + token + "/",
		"http://127.0.0.1:39241/",
		"http://127.0.0.1:39241/" + token + "/?evil=yes",
		"http://127.0.0.1:39241/" + token + "/../bad/",
	} {
		if _, err := newClient(candidate); err == nil {
			t.Errorf("accepted unsafe session %s", candidate)
		}
	}
}

func TestClientSignsMutationAndHandlesError(t *testing.T) {
	token := strings.Repeat("f", 48)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/"+token+"/api/action" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("X-Mutate-Session") != token {
			http.Error(w, "unauthorized", 403)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(409)
		_, _ = w.Write([]byte(`{"error":"pacote ausente"}`))
	}))
	defer server.Close()
	c, err := newClient(server.URL + "/" + token + "/")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.send(t.Context(), http.MethodPost, "action", map[string]string{"action": "inspect"}, nil); err == nil || !strings.Contains(err.Error(), "pacote ausente") {
		t.Fatalf("unexpected error: %v", err)
	}
}

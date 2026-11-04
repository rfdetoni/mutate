package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The native renderer uses the existing session-scoped Go application API.
// Only the CLI's loopback listener is allowed; no remote URLs are accepted.
type client struct {
	base  string
	token string
	http  *http.Client
}

func newClient(raw string) (*client, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Hostname() != "127.0.0.1" || u.Port() == "" {
		return nil, errors.New("sessão local inválida")
	}
	if _, _, err := net.SplitHostPort(u.Host); err != nil {
		return nil, err
	}
	token := strings.Trim(u.Path, "/")
	if len(token) != 48 || strings.Trim(token, "0123456789abcdef") != "" {
		return nil, errors.New("identificador de sessão inválido")
	}
	return &client{base: "http://" + u.Host + "/" + token + "/api/", token: token, http: &http.Client{}}, nil
}

func (c *client) request(ctx context.Context, method, endpoint string, body io.Reader, contentType string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, method, c.base+endpoint, body)
	if err != nil {
		return err
	}
	if method != http.MethodGet {
		req.Header.Set("X-Mutate-Session", c.token)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var fault struct {
			Error string `json:"error"`
		}
		if json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&fault) == nil && fault.Error != "" {
			return errors.New(fault.Error)
		}
		return fmt.Errorf("requisição não concluída (HTTP %d)", resp.StatusCode)
	}
	if dst == nil {
		return nil
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(dst)
}

func (c *client) get(ctx context.Context, path string, dst any) error {
	limited, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	return c.request(limited, http.MethodGet, path, nil, "", dst)
}
func (c *client) send(ctx context.Context, method, path string, payload any, dst any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return c.request(ctx, method, path, strings.NewReader(string(data)), "application/json", dst)
}

func (c *client) upload(ctx context.Context, file io.ReadCloser, name string) error {
	defer file.Close()
	reader, writer := io.Pipe()
	multi := multipart.NewWriter(writer)
	go func() {
		part, err := multi.CreateFormFile("file", filepath.Base(name))
		if err == nil {
			_, err = io.Copy(part, file)
		}
		if err == nil {
			err = multi.Close()
		}
		_ = writer.CloseWithError(err)
	}()
	// Local loopback transfers can be large; cancellation is tied to window exit.
	return c.request(ctx, http.MethodPost, "upload", reader, multi.FormDataContentType(), nil)
}

func (c *client) download(ctx context.Context, id string, dest io.Writer) error {
	if len(id) != 48 || strings.Trim(id, "0123456789abcdef") != "" {
		return errors.New("identificador do arquivo inválido")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"download/"+id, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download não disponível (HTTP %d)", resp.StatusCode)
	}
	_, err = io.Copy(dest, resp.Body)
	return err
}

func (c *client) downloadToPath(ctx context.Context, id, name string) (string, error) {
	// Only used as a fallback when the native save dialog is unavailable.
	name = filepath.Base(name)
	if name == "." || name == "" || name == "/" {
		return "", errors.New("nome de arquivo inválido")
	}
	dir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "Downloads", name)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return "", err
	}
	out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", err
	}
	if err := c.download(ctx, id, out); err != nil {
		out.Close()
		os.Remove(path)
		return "", err
	}
	if err := out.Close(); err != nil {
		return "", err
	}
	return path, nil
}

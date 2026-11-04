package main

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

// All visual assets ship inside the same CGO-free mutate executable.
//
//go:embed ui/*
var guiAssets embed.FS

func newGUICmd() *cobra.Command {
	var port int
	var noBrowser, register, browser bool
	var exportIcon string
	cmd := &cobra.Command{
		Use:   "gui",
		Short: "Open the integrated package installer and converter interface",
		Long:  "Open the M.U.T.A.T.E. graphical interface in the native Wails window. Use --browser to explicitly open a browser.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if port < 0 || port > 65535 {
				return fmt.Errorf("invalid local port %d", port)
			}
			if err := requireLinuxGUI(); err != nil {
				return err
			}
			if exportIcon != "" {
				return exportGUIIcon(exportIcon)
			}
			if register {
				if err := registerGUIDesktop(); err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), "Atalho M.U.T.A.T.E. instalado no menu de aplicativos.")
				return nil
			}
			// Missing Wails must never silently launch Chrome. Validate the
			// native window before allocating a session or binding a port.
			if !noBrowser && !browser {
				if _, err := nativeGUIExecutable(); err != nil {
					return fmt.Errorf("janela nativa Wails indisponível: %w. Reinstale o Mutate para instalar mutate-gui; para usar um navegador, execute 'mutate gui --browser'", err)
				}
			}
			app, err := newGUIApp()
			if err != nil {
				return err
			}
			defer app.close()
			app.repositories.StartupRefresh()
			ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
			if err != nil {
				return fmt.Errorf("open local GUI socket: %w", err)
			}
			defer ln.Close()
			app.address = ln.Addr().String()
			url := "http://" + app.address + "/" + app.token + "/"
			fmt.Fprintf(cmd.OutOrStdout(), "M.U.T.A.T.E. GUI: %s\n", url)
			fmt.Fprintln(cmd.OutOrStdout(), "Local-only session. Press Ctrl+C to close the service.")
			server := &http.Server{
				Handler:           app.handler(),
				ReadHeaderTimeout: 10 * time.Second,
				IdleTimeout:       90 * time.Second,
			}
			app.shutdown = func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = server.Shutdown(ctx)
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			go func() { <-ctx.Done(); app.shutdown() }()
			if !noBrowser {
				if browser {
					if err := openGUIBrowser(url); err != nil {
						fmt.Fprintf(cmd.ErrOrStderr(), "Unable to open browser: %v\nOpen %s manually.\n", err, url)
					}
				} else {
					native, err := startNativeGUI(url)
					if err != nil {
						return fmt.Errorf("falha ao iniciar a janela nativa Wails: %w. Use 'mutate gui --browser' apenas se desejar abrir no navegador", err)
					}
					defer func() {
						if native.Process != nil {
							_ = native.Process.Kill()
						}
					}()
					go func() {
						if err := native.Wait(); err != nil {
							fmt.Fprintf(cmd.ErrOrStderr(), "Janela Wails encerrada com erro: %v\n", err)
						}
						app.shutdown()
					}()
				}
			}
			err = server.Serve(ln)
			if errors.Is(err, http.ErrServerClosed) {
				return nil
			}
			return err
		},
	}
	cmd.Flags().IntVar(&port, "port", 0, "Local loopback port (0 picks an available port)")
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "Start the GUI service without opening a window")
	cmd.Flags().BoolVar(&browser, "browser", false, "Explicitly open the interface in a web browser instead of Wails")
	cmd.Flags().StringVar(&exportIcon, "export-icon", "", "Export bundled PNG application icon to a file")
	cmd.Flags().BoolVar(&register, "register", false, "Register M.U.T.A.T.E. in the current user's Linux application menu")
	return cmd
}

func requireLinuxGUI() error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("GUI launcher requires Linux")
	}
	return nil
}

// Registering is optional and always user-scoped; the installer does not
// silently modify system application menus.
func registerGUIDesktop() error {
	if os.Geteuid() == 0 {
		return fmt.Errorf("register the desktop shortcut as your normal user")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		return err
	}
	if strings.ContainsAny(exe, "\\\r\n\"") {
		return fmt.Errorf("unsupported executable path for desktop shortcut")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dataHome := os.Getenv("XDG_DATA_HOME")
	if !filepath.IsAbs(dataHome) {
		dataHome = filepath.Join(home, ".local", "share")
	}
	dir := filepath.Join(dataHome, "applications")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	iconPath := filepath.Join(dataHome, "icons", "hicolor", "64x64", "apps", "mutate.png")
	if err := exportGUIIcon(iconPath); err != nil {
		return err
	}
	content := "[Desktop Entry]\nType=Application\nName=M.U.T.A.T.E.\n" +
		"Comment=Conversor e instalador de pacotes Linux\n" +
		"Exec=\"" + exe + "\" gui\n" +
		"Icon=mutate\nTerminal=false\nCategories=Utility;System;\nStartupNotify=true\n"
	return os.WriteFile(filepath.Join(dir, "mutate.desktop"), []byte(content), 0644)
}

func newSessionToken() (string, error) {
	var raw [24]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("create secure GUI session: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}

func openGUIBrowser(url string) error {
	if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		return fmt.Errorf("no graphical desktop detected")
	}
	// Prefer a dedicated app window. Always pass the URL as one argv element,
	// never through a shell. This also avoids needing GTK/WebKit CGO bindings.
	for _, name := range []string{"chromium", "chromium-browser", "google-chrome", "google-chrome-stable", "brave-browser", "microsoft-edge"} {
		if executable, err := exec.LookPath(name); err == nil {
			browser := exec.Command(executable, "--app="+url, "--no-first-run")
			if err := browser.Start(); err == nil {
				go func() { _ = browser.Wait() }()
				return nil
			}
		}
	}
	for _, name := range []string{"xdg-open", "gio"} {
		executable, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		argv := []string{url}
		if name == "gio" {
			argv = []string{"open", url}
		}
		browser := exec.Command(executable, argv...)
		if err := browser.Start(); err == nil {
			go func() { _ = browser.Wait() }()
			return nil
		}
	}
	return fmt.Errorf("no desktop browser launcher available")
}

func guiFileSystem() (fs.FS, error) {
	return fs.Sub(guiAssets, "ui")
}

func exportGUIIcon(filename string) error {
	data, err := guiAssets.ReadFile("ui/mutate.png")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
		return err
	}
	return os.WriteFile(filename, data, 0644)
}

// Locate the sibling shell before starting a GUI session.
func nativeGUIExecutable() (string, error) {
	if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		return "", fmt.Errorf("nenhuma sessão gráfica detectada (DISPLAY/WAYLAND_DISPLAY)")
	}
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	return nativeGUIFromCLI(executable)
}

func nativeGUIFromCLI(executable string) (string, error) {
	native := filepath.Join(filepath.Dir(executable), "mutate-gui")
	info, err := os.Stat(native)
	if err != nil {
		return "", fmt.Errorf("mutate-gui não encontrado ao lado de %s: %w", executable, err)
	}
	if !info.Mode().IsRegular() || info.Mode()&0111 == 0 {
		return "", fmt.Errorf("mutate-gui não é um executável regular: %s", native)
	}
	return native, nil
}

func startNativeGUI(url string) (*exec.Cmd, error) {
	native, err := nativeGUIExecutable()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(native, url)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd, nil
}

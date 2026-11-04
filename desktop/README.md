# Mutate desktop (Gio UI)

The graphical application is rendered with **Gio UI** directly through the Linux GPU
(EGL/GLES and X11 or Wayland). No Chromium, WebKit, GTK widget toolkit,
HTML, CSS or JavaScript is used by the native window.

The CGO-free `mutate` CLI owns conversion, inspection, trusted catalog
operations, repositories and local session state. The native `mutate-gio`
window connects only to the private, token-authenticated 127.0.0.1 service
created by `mutate gui`. Browser mode is separately available only with
`mutate gui --browser`.

## Build on Ubuntu / Debian

```sh
sudo apt install gcc pkg-config libwayland-dev libx11-dev libx11-xcb-dev \
  libxkbcommon-x11-dev libgles2-mesa-dev libegl1-mesa-dev libffi-dev \
  libxcursor-dev libvulkan-dev
cd desktop
go mod tidy
CGO_ENABLED=1 go build -o ../mutate-gio .
```

For X11-only systems, `-tags=nowayland` is supported; for Wayland-only
systems, `-tags=nox11`. The installer detects which is available.
The desktop and CLI binaries must be installed in the **same directory**.
On Linux amd64, tagged GitHub releases package both binaries and SHA-256 checksums;
without a release, the installer builds them from source.

## Native UX

The UI keeps the approved compact three-column layout and violet palette.
Seven sections: Converter, Biblioteca, Repositórios, Inspecionar, Instalar,
Histórico, Configurações. Native file chooser and save dialog use XDG Portals,
with an explicit file-path fallback; actions are asynchronous and show real
operation status and errors. Installation always requires confirmation.
Only content panes scroll; no scroll containers are added without need.

The legacy browser assets remain embedded in the CLI strictly as an
**opt-in** compatibility interface.

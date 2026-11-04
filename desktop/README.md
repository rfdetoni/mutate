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

The installer checks for the Vulkan C header (`vulkan/vulkan.h`) as well as
EGL/GLES and X11/Wayland. If only Vulkan headers are missing on Ubuntu,
it installs `libvulkan-dev` without reinstalling the entire graphics stack.
If the package is already installed but the compiler cannot read its headers,
the installer prints the actual compiler error and skips reinstalling packages.
Review the diagnostic and check the compiler include paths on the host.
The native source installer prefers `/usr/bin/gcc` (the distribution compiler)
over any custom `gcc` shim earlier in `PATH` (including Zig wrappers).
It uses that same executable for the Vulkan header probe and the CGO build.
To intentionally use a custom compiler, set `MUTATE_GUI_CC` to the absolute
executable path; the same override is used in both steps.

For X11-only systems, `-tags=nowayland` is supported; for Wayland-only
systems, `-tags=nox11`. The installer detects which is available.
The desktop and CLI binaries must be installed in the **same directory**.
On Linux amd64, tagged GitHub releases package both binaries and SHA-256 checksums;
without a release, the installer builds them from source.

## Native UX

The UI keeps the approved compact three-column layout with a graphite/dark palette,
muted gray surfaces, high-contrast text and violet accents. The center package
chooser and activity modules expand to the full available workspace width. Session polling is reduced to
2.5 seconds, and the idle window sets the default cursor; this is not a
substitute for testing the actual pointer behavior on all Linux desktops.
Seven sections: Converter, Biblioteca, Repositórios, Inspecionar, Instalar,
Histórico, Configurações. Native file chooser and save dialog use XDG Portals,
with an explicit file-path fallback; actions are asynchronous and show real
operation status and errors. Installation always requires confirmation.
Converter, Inspecionar and Instalar use a static central workspace: wheel and
touchpad gestures no longer scroll the form. Shorter windows use denser spacing.
Library, Repositories, History and Settings keep scrolling. The inspector
continues to scroll independently when necessary.

The legacy browser assets remain embedded in the CLI strictly as an
**opt-in** compatibility interface.

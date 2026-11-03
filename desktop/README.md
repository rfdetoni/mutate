# Native Wails desktop shell

This optional **Wails v3** application opens the existing local authenticated
M.U.T.A.T.E. GUI in a GTK/WebKit window, with no Chrome/Chromium dependency.
The CLI is still built separately with `CGO_ENABLED=0` and owns all package
operations. Desktop exits when its window closes.

```sh
# Debian/Ubuntu 24.04+:
sudo apt install build-essential pkg-config libgtk-4-dev libwebkitgtk-6.0-dev

cd desktop
CGO_ENABLED=1 go build -o ../mutate-gui .
```

Install the compiled `mutate-gui` next to the `mutate` CLI binary. Then
`mutate gui` detects it and launches a native window. The main installer automatically detects and installs missing GUI build
libraries via APT, DNF, pacman or zypper when building from source. It
requests sudo access if necessary and never modifies system package
configuration files or performs Arch partial upgrades. When dependencies
cannot be installed (unsupported distro, no admin privileges or headless
session), it leaves the CLI functional and uses the existing browser fallback
with a warning. Disable automatic dependency installation with
`--no-gui-deps` or `MUTATE_INSTALL_GUI_DEPS=0`.

Ubuntu 22.04 / Debian 12 legacy systems may use WebKit2GTK 4.1 + GTK3 and
`-tags gtk3` while Wails v3.0.x supports that compatibility build.
Do not set `CGO_ENABLED=0` for the native shell.

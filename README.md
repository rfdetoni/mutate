# M.U.T.A.T.E.
**Multi-format Universal Transpiler & Application Translation Engine**

[![Language](https://img.shields.io/badge/Language-Go%201.26-blue.svg)](https://go.dev/)
[![CGO](https://img.shields.io/badge/CGO-Zero%20(Pure%20Go)-green.svg)](https://golang.org/)
[![License](https://img.shields.io/badge/License-GPL%20v3-orange.svg)](LICENSE)
[![Translations](https://img.shields.io/badge/Docs-Portugu%C3%AAs%20(Brasil)-blue)](README.pt-BR.md)

CLI tool: `mutate` (with shorthand alias `mut`).

---

## Overview

Unlike the legacy `alien` tool (which merely repacks file trees and ignores runtime and binary incompatibilities), **M.U.T.A.T.E.** is a modern, 100% pure Go (`CGO_ENABLED=0`) package transpiler and binary ELF adapter.

M.U.T.A.T.E. inspects ELF binaries, resolves missing shared libraries (`.so`) directly against host distribution databases, isolates missing or conflicting libraries in private application directories with wrapper runners, normalizes Filesystem Hierarchy Standard (FHS) directory layouts, transpiles package maintainer scripts via AST parsing, and generates valid native packages (`.deb`, `.rpm`, `.pkg.tar.zst`, `.apk`) without relying on external system packaging tools (`dpkg-deb`, `rpmbuild`, or `makepkg`).

---

## Key Features

- **Zero CGO (`CGO_ENABLED=0`):** Completely static, self-contained binary with zero external runtime dependencies.
- **Deep ELF Inspection:** Parses ELF headers using `debug/elf` to extract dynamic interpreter (`ld-linux` glibc vs `ld-musl`), architectures (`x86_64`, `aarch64`), `DT_NEEDED` libraries, `RPATH`, and `RUNPATH`.
- **Host Native Dependency Resolution:**
  - **Debian / Ubuntu / Mint:** High-speed in-memory filesystem scanning of `/var/lib/dpkg/info/*.list` + fallback to `dpkg -S`.
  - **Arch Linux / Manjaro:** Local database lookups in `/var/lib/pacman/local/*/files` + fallback to `pacman -F`.
  - **Fedora / RHEL / CentOS:** Local RPM database inspection via `/usr/lib64` + `rpm -qf` / `dnf provides`.
- **Explicit Library Isolation & Bundling:** Automatically isolates incompatible or missing host libraries into `/usr/lib/<app>/lib` and creates transparent execution wrappers. Adds `musl` loader support on `glibc` hosts when required.
- **FHS Directory Normalization:** Seamlessly bridges path discrepancies between distributions (e.g. `/usr/lib64` <-> `/usr/lib/x86_64-linux-gnu` <-> `/usr/lib`) and patches `.desktop`, `.service`, and `.pc` files.
- **AST Shell Script Transpilation:** Parses maintainer scripts (`preinst`, `postinst`, `prerm`, `postrm`) into Abstract Syntax Trees with `mvdan.cc/sh/v3/syntax`. Converts legacy service managers (`chkconfig`, `service`) to `systemctl`, adapts distro-specific `useradd` flags, and cleans redundant `ldconfig` invocations.
- **Android APK Bridging:** Extracts Android APKs, parses binary XML (`AndroidManifest.xml`), and creates native desktop entries and Waydroid / ATL execution launchers.
- **Source Tarball Building:** Detects and compiles Go, Cargo, CMake, Make, and Meson projects into isolated staging environments.

---

## Supported Package Formats

| Format | Extension | Unpack | Build |
|---|---|:---:|:---:|
| **Debian / Ubuntu** | `.deb` | ✅ | ✅ |
| **Red Hat / Fedora** | `.rpm` | ✅ | ✅ |
| **Arch Linux** | `.pkg.tar.zst` | ✅ | ✅ |
| **Alpine Linux** | `.apk` | ✅ | ✅ |
| **Android APK** | `.apk` (with `AndroidManifest.xml`) | ✅ | ➔ Waydroid launcher |
| **Source Tarball** | `.tar.gz`, `.tar.xz`, `.tar.zst` | ✅ | Opt-in build (`--allow-source-build`) |
| **AppImage** | `.AppImage` (Type 2 ELF) | Detect only | Not yet supported |

---

## Installation

### Using the Install Script
```bash
git clone https://github.com/rfdetoni/mutate.git
cd mutate
./install.sh
```

### From Source (Go 1.26.4+)
```bash
git clone https://github.com/rfdetoni/mutate.git
cd mutate
CGO_ENABLED=0 go build -ldflags="-s -w" -o mutate ./cmd/mutate
sudo install -m 0755 mutate /usr/local/bin/mutate
sudo ln -sf /usr/local/bin/mutate /usr/local/bin/mut
```

---

## CLI Usage

### 1. Inspect a Package
Analyze metadata, ELF binaries, dynamic libraries (`DT_NEEDED`), and discover which host packages satisfy them:
```bash
mutate inspect package.rpm
# or using the shorthand alias:
mut inspect package.deb
```

### 2. Build / Transpile to Another Native Format
Transpile any supported package format into `.deb`, `.rpm`, `.arch`, or `.apk` without installing:
```bash
# Convert RPM to Debian
mutate build package.rpm --to deb -o mypackage.deb

# Convert DEB to Arch Linux (.pkg.tar.zst)
mut build package.deb --to arch

# Convert DEB to Fedora RPM
mut build package.deb --to rpm

# Convert Android APK to native Debian package
mut build app.apk --to deb
```

### Conversion safety and current limitations

- **Inspect does not execute source code.** Source compilation is never implicit.
- Source builds require `--allow-source-build`. This explicitly runs build tools on the host **without an OS sandbox**; only use trusted sources.
- Missing shared objects fail with a clear error unless explicitly supplied via `--library-dir /path/to/libs`. Supplied libraries must be valid ELF files with matching SONAME and architecture.
- Cross-distribution conversions involving untranslated package dependencies or lifecycle scripts **fail closed** rather than silently emitting an incorrect package. To resolve target packages, run on the target distribution; same-format repackaging can preserve existing dependency metadata.
- Alpine package handling does not imply glibc/musl binary compatibility. Generic ZIPs and AppImages cannot currently be converted.
- `install --dry-run` prints the intended operation and does not build, compile, or install. It is a plan, not a compatibility guarantee.

### 3. Direct Installation on Host
Transpiles the package to the current host machine's native format and installs it using the host package manager (`apt`, `dnf`, `pacman`, or `apk`):
```bash
# Dry-run check
mutate install package.rpm --dry-run

# Actual installation
mutate install package.rpm
```

---

## Architecture

```
mutate/
├── cmd/
│   └── mutate/                     # CLI entrypoint & subcommands (inspect, build, install)
├── internal/
│   ├── domain/                     # Intermediate Representation (PackageIR, Metadata, FileEntry, ElfInfo)
│   ├── detector/                   # Pure magic byte identification (RPM, DEB, ZST, APK, AppImage)
│   ├── unpacker/                   # Format unpackers (RPM, DEB, Arch, Alpine, Android, Source)
│   ├── elfanalyzer/                # debug/elf scanner & LD_LIBRARY_PATH wrapper generator
│   ├── resolver/                   # Host distro detector & reverse .so -> package resolvers
│   ├── fhs/                        # FHS directory normalizer & config patcher (.desktop, .service)
│   ├── scripts/                    # Shell script AST parser & transpiler (mvdan.cc/sh)
│   └── packager/                   # Native pure Go builder (nfpm/v2) & package installer
├── install.sh                      # POSIX installer script
├── go.mod
└── LICENSE
```

---

## Running Tests

```bash
go test -v ./...
```

---

## License

This project is licensed under the GPL-3.0 License. See [LICENSE](LICENSE) for details.

### AppImage type 2 conversion

`mutate build program.AppImage --to deb -o program.deb` uses the optional `unsquashfs` command from `squashfs-tools`. It extracts a SquashFS v4 payload *without executing the AppImage runtime*, installs the AppDir under `/opt/<name>`, and creates a launcher for `AppRun`. Type 1 AppImage is not supported. Always test runtime compatibility on the target distribution.

### Sandboxed source builds

`mutate build project.tar.gz --to deb --allow-source-build` requires Linux Bubblewrap (`bwrap`) with user namespaces. Build tools execute in a mount, process and network namespace with only the staging tree writable. No fallback to unsandboxed builds is allowed. Offline dependencies must be vendored or available in the sandbox.

### Cross-distribution dependency mapping

Provide a verified JSON map with `--dependency-map deps.json` when source and destination formats differ, and for shared libraries not resolvable on the target host. Dynamic library keys start with `so:`:

```json
{"libc6": "glibc", "so:libc.so.6": "glibc", "so:libssl.so.3": "openssl-libs"}
```

Cross-format lifecycle scripts are transformed only when their POSIX syntax can be conservatively translated. Arch installation functions are split into lifecycle stages; unsupported upgrade hooks and source-specific package-manager commands are rejected rather than silently changed. Mapping package names does not prove ABI/runtime compatibility.

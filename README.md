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
- **Explicit Library Isolation & Bundling:** Validates explicitly supplied ELF libraries via `--library-dir` and bundles them with execution wrappers. Missing libraries are never downloaded automatically; ABI mismatches must be handled separately.
- **FHS Directory Normalization:** Seamlessly bridges path discrepancies between distributions (e.g. `/usr/lib64` <-> `/usr/lib/x86_64-linux-gnu` <-> `/usr/lib`) and patches `.desktop`, `.service`, and `.pc` files.
- **AST Shell Script Transpilation:** Parses maintainer scripts (`preinst`, `postinst`, `prerm`, `postrm`) into Abstract Syntax Trees with `mvdan.cc/sh/v3/syntax`. Converts legacy service managers (`chkconfig`, `service`) to `systemctl`, adapts distro-specific `useradd` flags, and cleans redundant `ldconfig` invocations.
- **Android APK Bridging:** Preserves the original APK without re-encoding or duplicating it into staging, parses the manifest and creates desktop entries and Waydroid launchers.
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
| **AppImage** | `.AppImage` (Types 1 and 2 ELF) | Optional via `--extract` | ✅ original file + `.desktop` by default |

---

## Installation

### Universal Linux install (curl)

Works on Debian/Ubuntu, Fedora/RHEL, Arch, Alpine (musl), and other Linux distributions with POSIX `sh`, `tar`, `curl` or `wget` and a supported Go architecture:

```sh
curl -fsSL https://raw.githubusercontent.com/rfdetoni/mutate/main/install.sh | sh
```

The default installation path is `~/.local/bin` for normal users or `/usr/local/bin` when already writable/root. Add `~/.local/bin` to `PATH` if necessary. The installer detects Linux CPU architecture, uses a SHA-256-verified prebuilt GitHub release if available and otherwise downloads the repository source archive and builds it. When Go >= the version in `go.mod` is absent, it can download a **temporary, SHA-256-verified official Go toolchain** without modifying the system toolchain. The source build requires network access to Go modules.

```sh
# Native system installation with noninteractive sudo privileges
curl -fsSL https://raw.githubusercontent.com/rfdetoni/mutate/main/install.sh | sh -s -- --system

# Force compilation rather than using a release
curl -fsSL https://raw.githubusercontent.com/rfdetoni/mutate/main/install.sh | sh -s -- --source

# Custom location
curl -fsSL https://raw.githubusercontent.com/rfdetoni/mutate/main/install.sh | sh -s -- --dir "$HOME/bin"

# Force a tagged binary release (fails if the asset is unavailable)
curl -fsSL https://raw.githubusercontent.com/rfdetoni/mutate/main/install.sh | sh -s -- --release --version v1.0.0
```

`wget -qO- https://raw.githubusercontent.com/rfdetoni/mutate/main/install.sh | sh` also works. Run as a regular user; **do not pipe network scripts to `sudo sh`**. `--system` requests privilege only for the final copy when passwordless sudo is configured. Until a tagged binary release is published, the default automatically uses verified Go bootstrap and compilation from source if needed. For auditability, download and review `install.sh` before running it.

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
- Source builds require `--allow-source-build`, Bubblewrap and user namespaces; compilation runs inside an offline sandbox. Use trusted sources and dependencies.
- Missing shared objects fail with a clear error unless explicitly supplied via `--library-dir /path/to/libs`. Supplied libraries must be valid ELF files with matching SONAME and architecture.
- Cross-distribution conversions involving untranslated package dependencies or lifecycle scripts **fail closed** rather than silently emitting an incorrect package. To resolve target packages, run on the target distribution; same-format repackaging can preserve existing dependency metadata.
- Self-contained artifacts recognized today: AppImage Types 1/2 and Android APK (requires Waydroid). Regular DEB/RPM/Arch/Alpine packages and source tarballs are **not** presumed self-contained. Generic ZIPs and nonstandard AppImages (Type 0) remain unsupported.
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

### Unit & Integration Tests
```bash
go test -v ./...
```

### Multi-OS VM / Container Test Matrix
Tests installation via `curl -fsSL https://raw.githubusercontent.com/rfdetoni/mutate/main/install.sh | sh -s -- --system` and validates all 7 package formats across Debian, Ubuntu, Fedora, Arch Linux, and Alpine:

```bash
# Run matrix test across all 5 OS environments (fast local mode)
make test-matrix-local
# or
./tests/os_matrix/run_matrix.sh --mode local

# Run online curl installation test
./tests/os_matrix/run_matrix.sh --mode online

# Run test on a specific OS only (alpine, debian, ubuntu, fedora, arch)
./tests/os_matrix/run_matrix.sh --os alpine
./tests/os_matrix/run_matrix.sh --os fedora
```

---

## License

This project is licensed under the GPL-3.0 License. See [LICENSE](LICENSE) for details.

### Self-contained applications: preserve by default

`mutate build program.AppImage --to deb -o program.deb` stores the **original AppImage** unchanged under `/opt/mutate/appimages/<name>/<name>.AppImage` and installs a `.desktop` shortcut in `/usr/share/applications`. This is the default for **both Type 1 and Type 2**, requires **no extraction utility**, and does not copy large binary files into temporary staging. The original runtime, dependencies, FUSE requirements and update behavior are unchanged. A generic desktop icon is used unless a separate integration is provided.

`mutate build program.AppImage --to deb --extract` opts into the old workflow: Type 1 uses `bsdtar` for ISO9660/Rock Ridge and Type 2 uses `unsquashfs` for SquashFS. `inspect` and `install` also accept `--extract` to enable deep AppImage inspection/conversion. External utilities are only required in this mode.

Android APKs are already kept intact as `.apk` files with a Waydroid launcher and desktop shortcut. APKs require Waydroid and are **not** native Linux executables. These two source formats implement the same portable preservation contract. Other native package formats and source tarballs keep their existing unpack/convert behavior because an archive is not necessarily a portable application.

### Sandboxed source builds

`mutate build project.tar.gz --to deb --allow-source-build` requires Linux Bubblewrap (`bwrap`) with user namespaces. Build tools execute in a mount, process and network namespace with only the staging tree writable. No fallback to unsandboxed builds is allowed. Offline dependencies must be vendored or available in the sandbox.

### Cross-distribution dependency mapping

Provide a verified JSON map with `--dependency-map deps.json` when source and destination formats differ, and for shared libraries not resolvable on the target host. Dynamic library keys start with `so:`:

```json
{"libc6": "glibc", "so:libc.so.6": "glibc", "so:libssl.so.3": "openssl-libs"}
```

Cross-format lifecycle scripts are transformed only when their POSIX syntax can be conservatively translated. Arch installation functions are split into lifecycle stages; unsupported upgrade hooks and source-specific package-manager commands are rejected rather than silently changed. Mapping package names does not prove ABI/runtime compatibility.

### Runtime prerequisite notices

On receiving an Android APK, `mutate inspect`, `build` and `install` check whether `waydroid` is in the current host's PATH and explain that Waydroid must be installed and configured on the machine running the APK. For AppImage Type 2 in preserve mode, they check `/dev/fuse` and common `libfuse.so.2` locations. Missing FUSE support produces an advisory warning and distro-specific guidance. Older AppImage runtimes may need FUSE 2, while newer runtimes may work with FUSE 3. Type 1 does not automatically imply a FUSE 2 requirement. `--extract` does not require FUSE to mount the original AppImage.

These checks do not execute the source, install dependencies or block package creation. The current host may differ from the destination host; a detected executable/library does not prove that the relevant service or runtime is correctly configured.

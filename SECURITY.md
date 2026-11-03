# Security policy

M.U.T.A.T.E. parses third-party package archives and can install packages as root. Treat inputs as untrusted.

## Safe defaults

- Archive extraction rejects path traversal, symlink parents, and unsupported special file types.
- Package symlinks are stored as metadata rather than followed in staging.
- `inspect` never runs source build scripts.
- `install --dry-run` only reports the planned command.
- Source builds require `--allow-source-build` and Linux Bubblewrap with user namespaces. Network and host write access are disabled by default; kernel exploits and toolchain vulnerabilities remain possible.
- Automatic remote library downloading is disabled. Supply ELF libraries explicitly through `--library-dir`.
- Cross-format dependencies and installer hooks that cannot be translated reliably are rejected.
- AppImage Type 1 extraction uses `bsdtar` (libarchive), with default path-traversal and symlink-ancestor protections enabled; Type 2 uses `unsquashfs`. Both parse untrusted filesystem data: use a low-privilege account with filesystem quotas. The embedded AppImage runtime is never executed.
- A generated package can still execute maintainer scripts on the user's machine when installed. Inspect packages before installing; never trust conversion as a security guarantee.

## Known limits

- Bubblewrap is required for source build isolation. This is namespace isolation, not a full virtual machine; the host kernel is shared.
- Runtime ABI, transitive dynamic dependencies, glibc/musl compatibility, and source/target service semantics cannot be guaranteed by ELF header inspection alone.
- Archives may consume significant disk space; run with limited permissions and filesystem quotas for untrusted content.

Report security issues privately to the repository owner rather than filing a public exploit report.

## Preserved portable artifacts

- AppImage Type 1/2 preservation is now the default. It copies the original executable into the native package and installs a freedesktop launcher; it does **not** execute it during build or inspect.
- The AppImage's own runtime, FUSE requirements, upstream updates, and security characteristics remain unchanged; desktop launch executes untrusted code only when the user opens it.
- The extractor path remains explicitly available through `--extract`. Use low-privilege accounts and quotas for parsing untrusted ISO9660 or SquashFS.
- Android APKs are stored intact for Waydroid. Waydroid must be installed; they cannot be run as native Linux binaries.
- Native package archives and source tarballs must not be automatically treated as self-contained executable applications.

## Runtime prerequisite checks

The runtime preflight reports whether Waydroid is available on PATH or whether common legacy FUSE 2 libraries and `/dev/fuse` can be observed. It does not execute AppImages or APKs, install dependencies, probe an Android container or guarantee that the destination machine supports the artifact. In particular, an AppImage Type 2 may use a newer runtime with built-in FUSE 3 support; the absence of a FUSE 2 library is advisory, not a hard failure.

## Curl installer

- `install.sh` is POSIX shell, supports user-local installs, and does not require running an entire remote script as root. System installs use the caller's existing write access or noninteractive sudo only for target installation.
- Official GitHub release archive downloads require a matching SHA-256 manifest; mismatches stop the installation. Without a release, the installer downloads source from the official repository and compiles. The upstream source ref defaults to the mutable `main` branch; use an audited checkout/ref when pinning is required.
- If a compatible Go toolchain is absent, the installer downloads a toolchain for supported Linux architectures from `go.dev`, then checks its SHA-256 against the official `go.dev/dl/?mode=json&include=all` metadata before unpacking. This is a temporary installation under a private directory, not a system-level Go install.
- A fetched source build downloads Go modules using the standard Go module checksum mechanisms. Review remote shell scripts and upstream sources before execution. The installer never automatically installs packages through a distro's package manager.

## Extraction limits and offline build cache

- Native archive extraction has per-archive defaults of 250,000 entries and 16 GiB expanded data, with 8 GiB per entry and 4 MiB metadata caps. Set `MUTATE_MAX_EXTRACT_BYTES` or `MUTATE_MAX_EXTRACT_ENTRIES` to reduce or raise the global budgets.
- External AppImage extraction by `bsdtar`/`unsquashfs` is not completely constrained by the internal Go budget; use low-privilege accounts and filesystem quotas.
- `MUTATE_BUILD_CACHE_DIR` is opt-in and read-only inside Bubblewrap; only designate caches with nonsensitive, prevalidated content.
- Android package identities now use the full package name to prevent collisions.

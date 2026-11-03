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

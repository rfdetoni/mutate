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

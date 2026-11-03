# M.U.T.A.T.E. · GUI workflow contract

The existing CLI and the core Go conversion pipeline remain authoritative
for package detection, dependency resolution, sandbox requirements and
packaging. The GUI is another entry point in the **same** CGO-free executable.

## Launch and boundaries
`mutate gui` serves embedded static assets on `127.0.0.1` with a
cryptographically random path-specific session token. Chromium `--app`
is preferred for a native-looking window, with the system browser as
fallback. `--no-browser` is supported for tests or remote/headless
launch. The URL works only while the GUI process is running.

All state-changing API requests use the session header and same-origin
validation. Host is restricted to the bound loopback address. The
application does not use external CDNs, network APIs or third-party
telemetry. It stores session files in a 0700 temporary directory and
removes them on exit. Uploaded package size is capped at 2 GiB.

## Workflow
1. File selection: drag-and-drop or browser file picker; metadata preview
   is available after format verification. Invalid formats keep the
   previous package selected and show a recoverable error.
2. Inspection: optional full ELF scan and host library resolution.
   A running operation disables duplicate submissions. Status reports
   real phases; no estimated percentage is invented.
3. Conversion: user chooses DEB, RPM, Arch or Alpine. Existing
   `prepareConversionWithOptions` enforces mapping and compatibility.
   Successful output is downloaded by the user from a private temporary
   file; the source is never modified.
4. Installation: after an explicit accessible confirmation, the package
   is converted to the host's native format; the OS authorizes execution
   through PolicyKit (`pkexec`) or an already privileged process.
   Errors retain the converted file for manual download. The GUI never
   collects administrator credentials.
5. History: the last twenty completed/failed operations in this process,
   no persistent profile or background service.
6. Settings: session-scoped switches for host resolution, AppImage
   extraction and trusted offline source compilation; optional JSON
   dependency map.

## Async resilience
A single backend operation owns the active workspace. A second upload or
action returns HTTP 409 and offers a clear retry after completion.
`GET /api/state` is read-only and returns immutable snapshots.
The frontend polls only while work is ongoing. Failures preserve selected
files and expose an actionable error without fake success. All file
references are identified by opaque IDs, never by arbitrary API paths.

## Canonical UI ownership
Navigation: `app.js` / `.nav-item`; form/radio: native HTML inputs;
dialog: native `<dialog>`; status notification: single `#toast` live
region; application scrollbar baseline: `style.css`; activity/error:
single `#status-panel`. No framework-level duplicate primitives.

## Out of scope
No package signature verification, sandboxed execution of installed apps,
remote multi-user web service or automatic elevation without user consent.
Cross-distro conversions still require explicit dependency mappings when
the Go motor cannot guarantee compatibility.

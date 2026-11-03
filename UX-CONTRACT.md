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
Desktop scroll ownership: the app frame fits the viewport (including margins);
sidebar, main and inspector scroll only for overflowing content. At widths up
to 900px, stacked views return to natural document scrolling.
Navigation: `app.js` / `.nav-item`; form/radio: native HTML inputs;
dialog: native `<dialog>`; status notification: single `#toast` live
region; application scrollbar baseline: `style.css`; activity/error:
single `#status-panel`. No framework-level duplicate primitives.

## Out of scope
No package signature verification, sandboxed execution of installed apps,
remote multi-user web service or automatic elevation without user consent.
Cross-distro conversions still require explicit dependency mappings when
the Go motor cannot guarantee compatibility.

## Application library and privileged installs
- Search is explicit (submit button or Enter), not live-on-keystroke.
  AUR network lookup is visible via a user-controllable checkbox.
- Native providers: APT, DNF, pacman and APK, chosen by the host's
  distribution. Only configured repository candidates are presented
  as immediately installable. An AUR result is discovery-only, with
  a link to its upstream page; no PKGBUILD execution.
- Each result shows name, candidate version, description and origin.
  Search is capped at 30 native + 20 AUR results; incomplete native
  lookup and failed AUR requests produce source-specific warnings.
- The API accepts installation only for a native result from the
  most recent in-session search. It validates the name, holds the
  one-active-operation lock and invokes the distro package manager
  with an argv array (never a shell).
- Privileged installation is confirmed in the product's native dialog
  before calling the API. PolicyKit performs elevation; package-manager
  dependencies and maintainer scripts can change the system. An error
  remains visible and history records the outcome.
- Source package-manager versions are not compared lexicographically
  across distros: the installed candidate is resolved by the host
  manager at installation time to honor repository priorities.

## Native desktop and source management
The CGO-free `mutate` binary serves the authenticated session; optional
`mutate-gui` is a Wails v3 GTK/WebKit shell installed beside it.
`mutate gui` prefers the native window, falling back to browser only when
the native build/runtime is unavailable. A native window exit shuts down
its local server and temporary data. No browser brand appears in the UI.

The Repositories section is discovery-only. Built-in AUR, official Arch
and Debian Sources APIs are enabled initially. User-provided HTTPS JSON
catalogs must declare a `{query}` template and must be explicitly
saved. Add/toggle/delete persist atomically under XDG config; built-ins
can be disabled but not removed. Remote refresh runs concurrently on
startup and never blocks the first paint. Show each source's update or
error state, preserve native results on remote failure, and never install
remote results without a separate compatibility/safety process. Only the
current OS's configured package manager may install packages.

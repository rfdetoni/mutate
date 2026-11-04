# M.U.T.A.T.E. · Design system

## North star
A compact Linux desktop utility, not a marketing website. The approved
reference is the compact three-column package installer: restrained
violet focus color, layered charcoal surfaces, subtle gray dividers, a generous
package chooser, and a persistent package inspector.
Avoid glass, gradients, neon, heavy shadows, faux progress and large cards.

## Token ownership
**Native runtime canonical source:** `desktop/ui.go` palette values and layout helpers.
The optional browser UI retains independent light CSS tokens in `cmd/mutate/ui/style.css`.
Do not accidentally apply desktop-only theme changes to the browser compatibility mode.

| Role | Runtime token | Value |
|---|---|---|
| Canvas | `canvas` | `#191c22` |
| Surface | `appSurface` | `#1e222a` |
| Sidebar / inspector | `mutedSurface` | `#222730` |
| Input / notice | `panelSurface` | `#282d36` |
| Primary text | `ink` | `#edf1f8` |
| Secondary | `softInk` | `#b4bdcb` |
| Muted | `mutedInk` | `#919bab` |
| Divider | `line` | `#3c434f` |
| Accent text | `violet` | `#9a8bf9` |
| Accent button | `violetButton` | `#6551b7` |
| Accent background | `violetLight` | `#3a3157` |
| Positive text | `positive` | `#42b980` |
| Positive button | `positiveButton` | `#1b724f` |
| Error state | `negative` | `#f47382` |
| Control radius | Gio widget styles | `8–10dp` |

## Layout and density
Desktop: application titlebar (69px), narrow 182px navigation, flexible
drop-zone/content area, fixed 332px inspector. Gio desktop has a minimum width of 840dp. Package workflows show a
persistent native inspector on the right; other workflows use a wide panel.
At narrow sizes, scrolling is confined to content panes, not the shell. Full-width drop-zone is visually dominant.
Package chooser and activity cards grow with the center workspace up to 680dp.
Activity state is a compact bordered module, not a dashboard full of cards.
The desktop frame must never exceed the viewport once outer gutters are included.
The sidebar, main content and inspector scroll independently only when their
contents overflow; at 900px and below, stacked layouts use natural document
scrolling so forms and result lists remain fully reachable.
Use 1px borders and no resting shadows. Dialogs may use a small neutral
shadow to communicate blocking context.

## Typography
Use system-native `Noto Sans`, `Segoe UI`, `Liberation Sans`
and sans-serif fallback to keep the statically embedded UI offline and
support Portuguese. Titles 21–27px; body 13–14px; helper labels 10–12px;
technical values may use `JetBrains Mono` / `Liberation Mono`.
No downloadable fonts or branded font distribution.

## Motion, states and accessibility
Motion only communicates current work (indeterminate progress while a job
is truly running); no invented percentages. The native window sets the default
pointer cursor while idle, and refreshes session state every 2.5 seconds to
avoid unnecessary work when no action is taking place. Focus rings use the violet
accent. Disabled buttons look disabled. Errors stay inside the activity
area and preserve the selected file. Respect reduced motion, keyboard
focus, and 200% zoom. Install confirmation uses a Gio confirmation state before invoking the privileged
operation. The legacy browser-only interface retains its native HTML dialog and live region.

## Components
- `drop-zone`: large, keyboard-activatable, accepts one file by
  clicking or dropping.
- `package-properties`: native definition list from actual backend
  metadata; no fabricated package details.
- `format-control`: accessible native radio group; four supported targets.
- `activity-steps`: system-driven progress, status text, error recovery.
- `action-btn`: one action per intent; violet for conversion, green for
  privileged installation, neutral for inspection.
- `history-list`: session-only; downloadable converted outputs.

## Application library
The Library section uses the same full-width main content as History and
Settings. Search is a single horizontal row above an editorial-style list
separated by hairline dividers, not a grid of inflated app cards. Every
row shows application name, short description, origin (native repository
versus community AUR), candidate version and a contextual action. Native
packages use the existing positive/neutral tokens; AUR remains visually
subordinate and does not pretend to be an official trusted repository.

## Approved application icon and simplified chrome
The approved icon is the violet package-and-rotation-arrow mark embedded at
`cmd/mutate/ui/mutate.png`; its transparent PNG corners must be preserved.
The Gio desktop uses the system window decoration; the header
shows only the app name and functional close control. Do not add technical
footer copy such as "CLI + GUI", "local tool" or "local session". Keep
repository management a flat row list with status, enable switch and one
compact creation form. The same violet/neutral tokens own both screens.

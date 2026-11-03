# M.U.T.A.T.E. · Design system

## North star
A compact Linux desktop utility, not a marketing website. The approved
reference is the flat three-column package installer mockup: restrained
violet focus color, white surfaces, thin gray dividers, a generous but
unembellished drag-and-drop zone, and a persistent package inspector.
Avoid glass, gradients, neon, heavy shadows, faux progress and large cards.

## Token ownership
**Runtime canonical source:** `cmd/mutate/ui/style.css` `:root` CSS variables.
This document mirrors the accepted values and behavior; change both together.

| Role | Runtime token | Value |
|---|---|---|
| Canvas | `--canvas` | `#f3f4f6` |
| Surface | `--surface` | `#ffffff` |
| Subtle surface | `--surface-muted` | `#f8f9fa` |
| Primary text | `--ink` | `#20232a` |
| Secondary | `--ink-soft` | `#525965` |
| Muted | `--ink-muted` | `#747b86` |
| Divider | `--line` | `#e1e4e8` |
| Accent | `--accent` | `#6754ae` |
| Accent background | `--accent-light` | `#f1eef9` |
| Positive state | `--positive` | `#267b55` |
| Error state | `--negative` | `#b23b45` |
| Control radius | `--radius` | `7px` |

## Layout and density
Desktop: application titlebar (69px), narrow 182px navigation, flexible
drop-zone/content area, fixed 332px inspector. At 900px the inspector
moves below the content; at 640px the navigation becomes a compact
horizontal scroll row. Full-width drop-zone is visually dominant.
Activity state is a compact bordered module, not a dashboard full of cards.
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
is truly running); no invented percentages. Focus rings use the violet
accent. Disabled buttons look disabled. Errors stay inside the activity
area and preserve the selected file. Respect reduced motion, keyboard
focus, and 200% zoom. The install confirmation uses the native `dialog`
element; close or Escape returns focus. Screen-reader status uses an
`aria-live` activity region.

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
The approved icon is the violet package-and-rotation-arrow mark, embedded at
`cmd/mutate/ui/mutate.png` and in the native Wails shell. The title bar
shows only the app name and functional close control. Do not add technical
footer copy such as "CLI + GUI", "local tool" or "local session". Keep
repository management a flat row list with status, enable switch and one
compact creation form. The same violet/neutral tokens own both screens.

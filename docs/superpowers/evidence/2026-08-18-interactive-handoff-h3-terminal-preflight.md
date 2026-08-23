# H3 Automatic Terminal Presentation — Darwin Preflight Authority

**Status:** Task-1 preflight complete for the approved Darwin/macOS experimental H3 lane. This document freezes the initial terminal provider/activity mechanisms that later H3 tasks may implement; it is not final H3 completion evidence.

```text
H3_PREFLIGHT_ALLOWED=true
H3_PLATFORM=darwin
H3_PROVIDER_GHOSTTY=promoted_preflight
H3_PROVIDER_APPLE_TERMINAL=not_promoted
H3_BROAD_AUTO_LAUNCH_CLAIM=false
H3_LINUX=NOT_RUN
H3_H4_COMBINED=NOT_RUN_COUNTERPART_ABSENT
```

## 1. Prerequisite binding

- H2 completion HEAD at H3 start: `1434ac75f71cb8df99b71d208aaf82cbbc87d78e` (`test: verify manual human agent handoff`).
- H2 completion evidence: `docs/superpowers/evidence/2026-08-18-interactive-handoff-h2-manual-authority.md`.
- H2 evidence SHA-256: `6787b0ba1835ceaba43c05202226619c039c9738e3fd35d5494e00203dfbea7d`.
- Required gate observed exactly: `H3_ALLOWED=true`.
- H4 completion evidence is absent at this checkpoint. H3 therefore must compose with H2 without assuming H4 privacy/readiness providers exist.

Fresh startup baseline before this preflight:

```text
HEAD   = 1434ac75f71cb8df99b71d208aaf82cbbc87d78e
branch = design/human-agent-interactive-session-handoff
tree   = clean
devctl check = PASS
receipt = .build/receipts/20260819T145619.432885000Z-check.json
```

## 2. Native host facts

```text
platform       = darwin/arm64
macOS          = 26.6.1 (25G76)
CGO release invariant = disabled
```

The repository's V1 release contract requires `CGO_ENABLED=0` unless a separately measured/platform-planned exception is approved. H3 does not introduce an AppKit/cgo dependency. The Darwin recent-terminal source is instead bound to the native `/usr/bin/lsappinfo` LaunchServices interface qualified below.

## 3. Initial terminal provider matrix

| Provider | Identity | Installed | Running at preflight | H3 status | Reason |
|---|---|---:|---:|---|---|
| Ghostty | `com.mitchellh.ghostty` | yes | yes | `promoted_preflight` | exact bundle identity plus argv-safe macOS `open --args` launch path is documented by installed Ghostty and `/usr/bin/open` |
| Apple Terminal | `com.apple.Terminal` | yes | no | `not_promoted` | no argv-safe child-command launcher contract was proven; H3 must not synthesize AppleScript/shell text merely to claim a second provider |
| iTerm2 | — | no | no | `NOT_RUN` | not installed |
| WezTerm | — | no | no | `NOT_RUN` | not installed |
| Alacritty | — | no | no | `NOT_RUN` | not installed |
| Warp | — | no | no | `NOT_RUN` | not installed |
| Linux terminal providers | — | — | — | `NOT_RUN` | Linux H0 remains unqualified/unadvertised |

Because only one terminal launcher is promoted on this host, H3 may expose the proven current-machine subset but **must not claim broad auto-launch UX**. The master design requires Ghostty plus at least one additional common terminal adapter before that broader claim.

## 4. Frozen Ghostty provider identity and launch interface

```text
provider_id        = ghostty
provider_version   = 1
platform            = darwin
bundle_id           = com.mitchellh.ghostty
bundle_version      = 1.3.1
bundle_build        = 15212
bundle_executable   = ghostty
launch_adapter      = darwin_open_args
```

Exact local facts:

```text
ghostty_executable_sha256 = 3460be6d0c80504ffafe0dbb06f60cb1e8fb680a564a97a1aa5b95b48b8e30ac
ghostty_plist_sha256      = f2f87c9c255cf0c98c8f9290de8604106d8ce15f211a2317e33ece755100e720
/usr/bin/open_sha256      = 0b049698ead58dd40a56ef34fafe626c6d41d3ff6c7e1bbe19c8d03e07b4b493
```

Installed Ghostty `+help` states that macOS CLI launch should use `open -na Ghostty.app` and that `--args` may pass configuration/command arguments; it also documents `-e <command>` as the child-command form. `/usr/bin/open` documents `-b` for exact bundle identifier, `-n` for a new application instance, and `--args` for argv delivery to the application's `main()`.

H3 freezes the provider-side launch argv shape as:

```text
/usr/bin/open
-n
-b
com.mitchellh.ghostty
--args
-e
<installed-shellbeam-executable>
session
attach
--handoff-id
<validated-handoff-id>
```

The production launcher must construct this as an argv slice. It may not use `/bin/sh -c`, AppleScript `do script`, source-checkout paths, Homebrew prefixes, or model-provided command text. `<installed-shellbeam-executable>` comes from runtime executable identity (for example `os.Executable()` plus validation), and `<validated-handoff-id>` has already passed the H2 identifier contract.

Task 5 still owes a real GUI launch/attach smoke before `ghostty` becomes an advertised final H3 provider. Task-1 promotion means “allowed to implement and native-qualify,” not “final capability PASS.”

## 5. Darwin active/recent/running application evidence

The frozen Darwin application evidence adapter is:

```text
activity_provider_id      = darwin_launchservices_lsappinfo
activity_provider_version = 1
binary                    = /usr/bin/lsappinfo
binary_sha256             = 2f491e4e113ec94bf55827cb9f018043bfcb98765cca4387b85a6ef6cc7bbddd
```

The local `lsappinfo(8)` manpage identifies it as a CoreApplicationServices/LaunchServices app-state interface and documents:

```text
front
info
find
listen +<notification-code> ...
wait / forever
```

It documents `becameFrontmost` as a LaunchServices notification emitted when an application becomes frontmost. The qualified shared event source is therefore conceptually:

```text
/usr/bin/lsappinfo listen +becameFrontmost forever
```

A real native probe subscribed once, activated existing Ghostty with public `/usr/bin/open -a Ghostty`, then returned focus to Firefox. The listener emitted two actual events, including exact bundle IDs:

```text
kLSNotifyBecameFrontmost ... CFBundleIdentifier="com.mitchellh.ghostty" ... affectedASN="Ghostty" ...
kLSNotifyBecameFrontmost ... CFBundleIdentifier="org.mozilla.firefox" ... affectedASN="Firefox" ...
```

This proves the source is event-driven; H3 must use at most one shared listener for this provider, never one poller/ticker per handoff/session/terminal. The adapter accepts only recognized supported terminal bundle IDs. A browser/nonterminal activation may update current foreground truth but **must not erase the last supported terminal record**; freshness expiry handles that later.

On-demand active/running discovery may use bounded `lsappinfo front`, `info`, and `find bundleid=<known-provider-bundle-id>` queries. Arbitrary executable paths returned by LaunchServices are observation only and never become launch authority.

If `/usr/bin/lsappinfo` is absent, its output shape is unparseable, or a required query/subscription fails, the activity source degrades unavailable. H3 must not fall back to an `osascript` polling loop, periodic process/window scanning, or raw `TERM_PROGRAM` execution authority.

## 6. Raw preflight artifacts

Ignored local artifacts under `.build/interactive-handoff-h3/` contain the exact probe/help material:

```text
ghostty-help.txt
apple-terminal-open-help.txt
provider-matrix.txt
lsappinfo-help.txt
lsappinfo-usage.txt
lsappinfo-listen-open.raw
lsappinfo-event-probe.txt
```

Key local artifact hashes at freeze time:

```text
ghostty-help.txt                ba6712384ea0b60335ac4446e7b61ff3ccfb94171e038ee8e4273dc75dbbf79d
apple-terminal-open-help.txt    072f2d80995413fe36b6c9114567b3666938cb1e28b90930288a89d875ef7353
```

## 7. Scope locks for implementation tasks

Task 2 onward is bound by these preflight decisions:

1. Current execution and capability work is Darwin-only; Linux stays explicit `NOT_RUN`/unadvertised.
2. `ghostty` is the only launcher provider allowed into the initial production registry unless this tracked preflight is amended first with equivalent native evidence for another provider.
3. Apple Terminal is not a fallback merely because it is installed.
4. There is no static `preferred_terminal`.
5. Recent terminal activity uses the shared event-driven LaunchServices lane above; no cgo/AppKit and no polling loop.
6. Raw environment/process facts may help bridge affinity validation but cannot authorize an arbitrary application path.
7. H3 does not add shell-aware readiness or secret privacy and may not assume H4 exists.
8. Manual H2 attach remains the degraded fallback whenever automatic resolution/launch is unavailable or ambiguous.

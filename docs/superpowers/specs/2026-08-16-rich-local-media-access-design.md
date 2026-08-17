# ShellBeam Rich Local Media Access Design

**Status:** Core architecture approved; Phase A compatibility tracer is approved to run; production authorization remains blocked on both Phase A PASS and the Section 8 strict-JSON parser/toolchain gate. Historical implementation commits made before that conjunction PASS are not retroactively authorized and require conformance audit after the checker returns exit `0`.
**Date:** 2026-08-17
**Scope:** Bounded local image acquisition and native MCP rich-content delivery through ShellBeam
**Preferred product surface:** one existing MCP tool, `local_shell`, with a modern `read_media` action
**Conditional fallback:** one dedicated read-only MCP tool only if the one-tool candidate fails a normative Section 21 scorecard hard gate and the dedicated-tool candidate passes the same full scorecard
**Depends on:** ShellBeam V1 Design, Agent Execution Layer Design, Workspace/Worktree/Git Identity
**Implementation plan:** `docs/superpowers/plans/2026-08-17-rich-local-media-access.md` exists, but its production Tasks 1–10 remain normatively gated by the machine conjunction. The parser-boundary amendment is tracked in `docs/superpowers/plans/2026-08-17-rich-local-media-parser-boundary-amendment.md`.

## 1. Decision

ShellBeam will support an explicit local-data-egress operation that lets a connected MCP client/model receive one local image without requiring the user to manually attach the file.

The preferred public surface is:

```json
{
  "action": "read_media",
  "workspace_id": "ws_...",
  "path": "artifacts/settings.png"
}
```

or:

```json
{
  "action": "read_media",
  "cwd": "/tmp",
  "path": "settings.png"
}
```

The initial production contract is deliberately narrow:

- media kind: `image` only;
- accepted encoded formats: PNG, JPEG, and WebP;
- one exact regular file per call;
- the entire selected encoded file is transferred, not only decoded pixels;
- no generic binary transfer;
- no GIF, SVG, BMP, TIFF, AVIF, JPEG XL, HEIC/HEIF, RAW, PDF, Office document, audio, video, archive, directory, socket, device, or FIFO payload;
- no resize, transcode, OCR, EXIF stripping, semantic interpretation, thumbnailing, or media cache;
- no durable media state;
- no public HTTP file server, data URL fallback, undocumented ChatGPT API, or cloud upload service.

The user-facing promise is:

> Let ChatGPT use a selected local image without the user manually uploading it.

This is **not** a local-only privacy guarantee. Successful `read_media` sends the original selected encoded bytes through the configured MCP transport to the connected client/model. Embedded EXIF/GPS/ICC/comment/application metadata and valid trailing bytes may therefore leave the machine with the image.

## 2. Why this belongs in ShellBeam

ShellBeam already gives ChatGPT local execution. Text output cannot faithfully carry visual artifacts: a command may produce `screenshot.png`, but the model may learn only that the path exists. The user then has to locate and attach the file manually.

The V1 architecture explicitly reserved bounded artifact access when screenshots or other binary outputs demonstrate that text workflows are insufficient. Local image inspection is the first concrete use case.

This feature stays at the Web-to-local boundary:

```text
ChatGPT / MCP client
        |
        | local_shell read_media
        v
MCP bridge
  schema / disclosure / rich-content translation
        |
        | owner-only local IPC
        v
ShellBeam daemon
  effective local authority
        |
        v
media application service
  addressing / admission / limits / errors
        |
        v
localfs media reader
  descriptor traversal / bounded acquisition / image-header validation
        |
        v
bounded raw bytes + metadata
        |
        | bounded IPC JSON
        v
MCP bridge
        |
        +--> TextContent summary
        +--> ImageContent
        +--> StructuredContent metadata
        v
ChatGPT / MCP client
```

The model interprets the image. ShellBeam does not become a browser, image editor, media processor, OCR engine, or reasoning system.

## 3. Primary workflows

### 3.1 Frontend visual verification

```text
ChatGPT
  -> start local dev server through ShellBeam
  -> run Playwright / agent-browser / existing browser CLI
  -> screenshot is written locally
  -> read_media
  -> visually inspect
  -> edit
  -> screenshot again
```

ShellBeam does not own browser automation.

### 3.2 Generated visual artifacts

```text
local renderer
  -> chart / diagram / slide image
  -> read_media
  -> model visually checks output
```

### 3.3 Documents by external rendering

PDF/document support is not part of `read_media` V1. A caller may use an already-installed renderer to create bounded PNG/JPEG/WebP page images and then call `read_media` one image at a time.

## 4. Protocol and platform facts

The design relies on these external contracts:

1. MCP tool results may contain mixed content including image content.
2. MCP `ImageContent` carries encoded image bytes plus a MIME type and optional annotations.
3. ShellBeam already uses the official MCP Go SDK; the SDK owns the JSON/base64 representation of `ImageContent`.
4. The official Go SDK exposes raw tool arguments to low-level handlers, leaving argument validation to ShellBeam.
5. The strict JSON behavior required by this design is a product/security contract independent of the concrete parser implementation: invalid UTF-8, duplicate names, unknown members, wrong-case members, and trailing JSON must be rejected deterministically.
6. In Go 1.26, `encoding/json/v2` and `encoding/json/jsontext` are **experimental**, are outside the Go 1 compatibility promise, and exist only when the whole build uses `GOEXPERIMENT=jsonv2`; they are not an ordinary Go 1.26 standard-library path.
7. Go 1.27 is not yet released as of this revision. Its draft release notes plan to make `encoding/json/v2` and `encoding/json/jsontext` available by default, but the design SHALL treat that as future evidence until an official Go 1.27 GA toolchain exists and ShellBeam's compatibility gates pass.
8. Go's `image.DecodeConfig` is the intended pre-decode resource check for arbitrary images.
9. `golang.org/x/image/webp` supplies WebP `DecodeConfig`.
10. Go `os.Root` is traversal-resistant but deliberately follows symlinks that remain inside the root; that does not satisfy this design's V1 invariant of rejecting every media-path symlink.
11. Ordinary files generally do not support `os.File.SetDeadline`; the media acquisition budget therefore cannot be advertised as a hard kernel-level interruption guarantee.
12. ChatGPT developer-mode metadata changes require Refresh and a new conversation for affected evaluation.
13. OpenAI currently recommends focused operations with accurate tool annotations, so retaining one broad `local_shell` tool is a product compatibility choice, not a protocol requirement.

MCP support plus model vision support does **not** prove that current ChatGPT Web passes custom-MCP returned `ImageContent` into model vision. Section 21 makes that empirical proof mandatory.

## 5. Goals

`read_media` SHALL:

- remove manual attachment friction for one explicitly selected local image;
- return native MCP `ImageContent`, never base64 stuffed into text or structured JSON;
- preserve bridge-as-translator architecture;
- keep local filesystem acquisition in the daemon side of the trust boundary;
- provide deterministic closed addressing and format contracts;
- bound request, path, file, pixel, concurrency, IPC, and memory work;
- reject every symlink component below the pinned media base in V1;
- reject special files before any blocking content read;
- avoid durable copies, hashes, caches, retention workers, or blob stores;
- expose full-file-egress disclosure accurately;
- make bridge/daemon version skew fail closed;
- leave ordinary `start`, `poll`, `write`, `kill`, and inspection hot paths unchanged when media is unused;
- remain easy to disable by capability intersection.

## 6. Non-goals

V1 does not:

- provide a generic local filesystem read API;
- transfer arbitrary binary files;
- read multiple files in one call;
- recurse directories;
- follow symlinks;
- stream media;
- pass file descriptors through IPC;
- create resource URIs or MCP resources;
- expose a local HTTP media server;
- strip metadata;
- decode full-resolution pixels merely to validate an image;
- resize or transcode;
- support GIF, HEIC, PDF, audio, or video;
- infer which local file the user meant;
- scan a workspace for images;
- treat images as execution evidence automatically;
- persist the payload into receipts, telemetry, repro, events, output logs, activities, or project manifests;
- promise syscall cancellation on remote/FUSE/NFS filesystems;
- defend against an adversary with arbitrary control of the same OS account;
- add a sandbox.

## 7. Public address contract

A request uses exactly one address form.

### 7.1 Workspace form

```json
{
  "action": "read_media",
  "workspace_id": "ws_01...",
  "path": "artifacts/settings.png"
}
```

Rules:

- `workspace_id` is required;
- `cwd` is absent;
- `path` is relative to the authoritative registered workspace root;
- workspace lookup uses the existing workspace authority;
- media-path no-follow semantics begin below the pinned workspace base.

### 7.2 Explicit absolute-cwd form

```json
{
  "action": "read_media",
  "cwd": "/tmp",
  "path": "settings.png"
}
```

Rules:

- `cwd` is an absolute directory path;
- `workspace_id` is absent;
- `path` is relative to `cwd`;
- this remains an explicit current-user escape hatch like absolute-cwd command execution.

### 7.3 Logical path grammar

`path` is valid UTF-8 and 1-1024 bytes.

Validation occurs on the raw logical string **before** normalization. ShellBeam splits on `/` and rejects:

- an absolute path;
- an empty path;
- NUL;
- backslash;
- empty components, including `a//b`;
- `.` components;
- `..` components;
- a trailing slash;
- more than 64 components.

ShellBeam does not call `filepath.Clean` and then decide whether the original logical path was safe. Inputs such as `a/../b`, `a/./b`, `a//b`, and `a/` are invalid even if a cleaning function could normalize them.

`cwd`, when present, is valid UTF-8, absolute, contains no NUL, and is at most 1024 bytes.

V1 supports macOS and Linux. Windows path semantics are out of scope.

### 7.4 Complete user-visible address (`display_address`)

`path` alone does not identify the selected file. The user-visible identity is the complete public address chosen by the caller. ShellBeam defines a derived `display_address` with exactly one of these shapes:

Workspace form:

```json
{
  "address_kind": "workspace",
  "workspace_id": "ws_01...",
  "path": "artifacts/settings.png"
}
```

CWD form:

```json
{
  "address_kind": "cwd",
  "cwd": "/tmp",
  "path": "settings.png"
}
```

Normative rules:

- workspace form contains the exact accepted `workspace_id` plus the exact caller-supplied raw logical `path`;
- CWD form contains the exact caller-supplied `cwd` plus the exact caller-supplied raw logical `path`;
- the values are captured after strict JSON/lexical validation but before workspace lookup, `EvalSymlinks`, base pinning, canonicalization, or media-file traversal;
- no normalized, canonical, resolved, descriptor-derived, or daemon-internal filesystem path may replace either public component;
- the caller-supplied absolute `cwd` is public because it is already part of the explicit API address; a separately resolved/canonical absolute path remains private;
- `display_address` is the address used by pre-result UI disclosure, compatibility goldens, structured result metadata, and address-correlation tests;
- two requests with the same `path` but different `workspace_id` or `cwd` have different `display_address` values and identify different candidate files.

A correct relative `path` under the wrong base is therefore an **address mismatch**, not a partial success.

## 8. Strict JSON contract

Media access is security-sensitive local-data egress. Malformed JSON must not be silently repaired.

For modern protocol-v2 tool arguments:

- invalid UTF-8 is rejected;
- duplicate object member names are rejected;
- unknown members are rejected;
- member-name matching is case-sensitive;
- trailing JSON is rejected.

These rejection properties are **normative**. They do not depend on a specific Go release or parser package. ShellBeam SHALL own them behind one reviewed strict-decoding boundary, conceptually `strictJSONDecode`, used by the modern tool-argument path and the IPC-v2 media envelope after the outer byte ceiling has been enforced. Legacy protocol-v1 behavior remains unchanged.

### 8.1 Parser/toolchain selection gate

The production parser/toolchain mode is selected only after evidence; it is not an undeclared developer environment choice.

**Preferred stable path:** after Go 1.27 is officially GA, evaluate an ordinary supported Go 1.27+ build using standard-library `encoding/json/v2` / `encoding/json/jsontext` with `RejectUnknownMembers(true)` (or equivalent frozen options). This path wins only if the complete Section 26.1 compatibility matrix passes, including the existing valid-v2 corpus, legacy v1 behavior, full repository tests, and supported release builds.

**Approved Go 1.26 library-boundary fallback:** while an official Go 1.27 GA upgrade is unavailable, ShellBeam may use one explicitly pinned JSON-v2 library behind the ShellBeam-owned strict-decoding boundary without enabling the whole-build JSON-v2 experiment. The selected fallback identity is frozen as:

```text
candidate = go1.26-pinned-json-library-boundary
module = github.com/go-json-experiment/json
version = v0.0.0-20260623181947-01eb4420fa68
GOEXPERIMENT = <empty>
```

This candidate is eligible only when every modern security-sensitive decode passes through the reviewed `jsonstrict.Decode` boundary, protocol-v1 behavior remains unchanged, the module version is exact, global `GOEXPERIMENT=jsonv2` is absent, and the complete Section 26.1 compatibility matrix passes. Dev, CI, test, packaging, checkpoint, and release paths must run the same guard and exact module identity. The pinned library is a deliberate reviewed dependency, not an undeclared parser substitution.

**Rejected whole-build Go 1.26 experiment on the current dependency graph:** the earlier `go1.26-jsonv2-experiment` candidate was evaluated and failed the mandatory non-media/full-build matrix because enabling `GOEXPERIMENT=jsonv2` changes JSON APIs process-wide and breaks `go.lsp.dev/protocol v1.0.1`. It is therefore not an eligible production candidate under this revision. Reopening it requires another explicit design amendment plus a fresh complete Section 26.1 matrix; a local dependency patch or shell-only experiment flag cannot authorize it.

No V1 fallback may silently introduce:

- an unpinned or unreviewed external JSON implementation;
- a hand-written duplicate-name/UTF-8 JSON tokenizer;
- a global JSON experiment hidden in shell state;
- a build where local development, CI, tests, packaging, checkpoint, and release artifacts use different parser modes.

If migrating the complete existing v2 semantic decoder would change a documented valid payload, the selected approved parser/toolchain mode may be used as a strict syntactic preflight while preserving existing semantic decoding for unaffected non-media branches. The exact split must preserve all five normative rejection properties for media/public-v2 inputs and must pass the frozen valid-payload corpus.

### 8.2 Planning gate

Phase A is intentionally independent of this parser decision and may run now. **No production implementation plan or production implementation task is authorized until both are true.** A plan and several implementation commits now exist historically before this conjunction was satisfied; that history does not weaken the gate. Those commits may be retained only after the conjunction checker returns exit `0` and a retroactive conformance audit proves them against the approved plan.

The authorization conditions are:

1. Phase A passes its complete Section 21 scorecard; and
2. one parser/toolchain candidate in Section 8.1 passes the Section 26.1 gate and is frozen as the reproducible production build contract.

If neither the stable Go 1.27+ candidate nor the approved Go 1.26 pinned-library-boundary candidate passes, rich-local-media remains unadvertised and production planning stops.

## 9. Preferred public tool surface and conditional fallback

### 9.1 Preferred: one `local_shell` tool

The preferred design keeps one MCP tool and adds a closed `read_media` action.

Benefits:

- existing ShellBeam discovery surface remains compact;
- no second tool namespace;
- one connection and one capability catalog;
- model can use execution and image retrieval in the same local bridge.

Cost:

- MCP tool annotations apply to the whole `local_shell` tool;
- `local_shell` remains conservatively destructive/open-world because other actions execute arbitrary local commands;
- ChatGPT may therefore show confirmation behavior inappropriate for a read-only media action;
- OpenAI guidance generally favors focused operations.

The one-tool decision is **conditional**, not immutable.

### 9.2 Fallback: dedicated `read_local_image`

Before implementation planning, the compatibility tracer evaluates the preferred topology against the normative Section 21 scorecard. The dedicated focused tool is tested only when the one-tool candidate fails at least one hard gate, including any direct/indirect/follow-up selection threshold, zero-egress negative threshold, max-payload threshold, or required disclosure/confirmation configuration gate.

The design SHALL switch to the dedicated tool only when the one-tool candidate fails at least one normative hard gate **and** the dedicated-tool candidate passes the same full scorecard. If both fail, the feature stops or the contract is revised; no subjective “better UX” comparison authorizes production.

A dedicated read tool would be conservatively annotated:

```text
readOnlyHint: true
destructiveHint: false
openWorldHint: true
```

`openWorldHint` remains true because successful use sends local bytes to an external connected service/model.

The fallback is not approved automatically; the compatibility evidence must trigger it.

### 9.3 Required client-visible media disclosure metadata

Every candidate topology SHALL expose client-visible tool metadata that states the media operation sends the **original selected local file bytes** to the connected MCP client/model and that those encoded bytes may include **embedded metadata** such as EXIF/GPS/ICC/comments/application-specific metadata/trailing bytes. It must not describe the operation as sending only rendered pixels.

For the preferred one-tool topology, the `local_shell` tool description/instructions plus the `read_media` action metadata must make this media-specific egress behavior discoverable despite the tool also containing command-execution actions. For the dedicated fallback, the `read_local_image` tool description carries the same disclosure directly.

The exact metadata snapshot used by ChatGPT is compatibility evidence. Any change to this disclosure wording, tool description, action description/schema, or annotations requires the Section 21.9 Refresh + new-conversation discipline before the candidate can be scored.

## 10. Success result contract

On success, the daemon/application result contains raw bytes and bounded metadata only:

```text
schema_version
kind = image
display_address = exact caller-visible workspace-or-cwd address
mime_type
format
byte_size
width
height
data []byte
```

The public result carries the exact `display_address` defined in Section 7.4. It never substitutes a canonical/resolved filesystem path for that address.

The MCP adapter returns:

```text
Content[0] = TextContent
Content[1] = ImageContent
StructuredContent = small metadata only
```

Example summary:

```text
read_media: image/png 1440x900, 482304 bytes
```

`ImageContent`:

```text
Data: raw encoded file bytes
MIMEType: image/png
Annotations: selected by the compatibility gate
```

Structured content includes no base64/blob bytes.

The official MCP Go SDK owns `ImageContent` wire encoding. ShellBeam never constructs `data:image/...` strings or a custom MCP image envelope.

## 11. Image-content annotations

MCP image content supports optional audience annotations.

The compatibility tracer SHALL compare at least:

1. annotations omitted;
2. `audience = ["user", "assistant"]`.

Each annotation candidate is run through the same normative Section 21 scorecard. If exactly one candidate passes every hard gate, use that candidate. If both pass, prefer annotations omitted as the smaller contract. If neither passes, stop or revise the media surface. Record the result in compatibility evidence before any implementation plan is written.

No production assumption about `audience` is made solely from the MCP specification.

## 12. Format contract

V1 accepts encoded:

```text
image/png
image/jpeg
image/webp
```

Format authority comes from bounded image-header decoding, not from filename extension and not from a caller-provided MIME string.

The implementation registers only the required decoders and uses `image.DecodeConfig` over the captured bytes. It maps the returned format through a closed table:

```text
png  -> image/png
jpeg -> image/jpeg
webp -> image/webp
```

Any other registered/returned format is rejected even if another imported package accidentally registers it globally.

A `.txt` file containing a valid PNG may be accepted; a `.png` file containing non-PNG bytes is rejected.

V1 intentionally rejects GIF, including static GIF, to avoid adding frame/animation classification solely for another format.

## 13. Hard and candidate limits

Initial candidate limits:

| Resource | Candidate limit |
| --- | ---: |
| files per call | 1 |
| `path` bytes | 1024 |
| `cwd` bytes | 1024 |
| path components | 64 |
| raw encoded image bytes | 7 MiB (7,340,032 bytes) |
| width | 16,384 px |
| height | 16,384 px |
| total pixels | 40,000,000 |
| concurrent media acquisition workers | 2 |
| acquisition budget | 5 s cooperative/best-effort |
| IPC media envelope overhead | 64 KiB beyond exact base64 length |

The 7 MiB image limit is the **current candidate release ceiling** after the real tunnel rejected the former 8 MiB candidate. The observed Secure MCP Tunnel request/response body ceiling is 10,485,760 bytes; 8 MiB raw expands beyond that ceiling after base64 plus the MCP/JSON envelope.

For 7 MiB raw bytes:

```text
raw_image_bytes = 7,340,032
base64.StdEncoding.EncodedLen(7 MiB) = 9,786,712 bytes
envelope_budget = 65,536 bytes
outer media response ceiling = 9,786,712 + 65,536
                            = 9,852,248 bytes
tunnel_body_cap_observed = 10,485,760 bytes
```

Historical rejected-candidate evidence: the later live-tunnel 8 MiB attempt failed with HTTP 413 `request_body_too_large`; earlier timed-out/stale-prompt 8 MiB attempts remain `NOT_RUN`.

The media byte budget is independent of ordinary text/output response limits. Enabling media does not expand normal `poll`/inspection/output-view response ceilings.

Width × height validation uses overflow-safe integer arithmetic.

## 14. Ownership split

### MCP facade

Owns:

- public action/tool metadata;
- user-facing disclosure;
- effective tool schema;
- MCP content annotations;
- `ImageContent` translation;
- ChatGPT compatibility evidence.

### Bridge

Owns:

- private consumer-capability negotiation with compatible daemons;
- intersection of bridge-supported and negotiated-daemon capabilities;
- client-visible effective catalog;
- pre-decode IPC response ceiling;
- strict media-envelope validation after bounded read;
- translation only.

The bridge never opens a local media path.

### Daemon/application media service

Owns:

- admission and two-worker capacity before any filesystem-backed resolution;
- workspace/address resolution while holding the media slot;
- candidate hard limits;
- deadline/budget orchestration;
- public domain error classification;
- immutable result shaping.

### Localfs media adapter

Owns:

- pinned base descriptor;
- component-wise descriptor traversal;
- exact no-follow flags;
- final special-file-safe open;
- pre/post `fstat`;
- bounded content read;
- image-header validation.

No new durable authority exists.

## 15. Capability negotiation, intersection, and version skew

Daemon capability alone is insufficient because the bridge owns MCP schema/rendering. A bridge-side intersection also cannot make `old bridge + new daemon` fail-closed by itself, because the old bridge does not know the new intersection rule.

V1 therefore uses **explicit private IPC consumer negotiation** before rich-local-media can become client-visible.

### 15.1 Private opt-in negotiation

The ordinary legacy-compatible IPC `inspect.server` response from a media-capable daemon SHALL omit rich-local-media support. Media support is disclosed only through a new private IPC action:

```text
capabilities.negotiate
```

This action is internal bridge/daemon protocol, never an MCP `local_shell` action.

A new bridge sends a bounded declaration of capabilities it can safely translate and expose, for example:

```text
consumer = mcp_bridge
consumer_capability_schema = 1
media:
  schema_versions: [1]
  kinds: [image]
  mime_types: [image/png, image/jpeg, image/webp]
  max_image_bytes: 7340032
  max_width: 16384
  max_height: 16384
  max_pixels: 40000000
```

The daemon returns only capabilities supported by both the daemon implementation and the declared consumer contract. Omission of the negotiation call, malformed/unsupported consumer capability schema, or an empty compatible media set means media is unavailable.

The negotiation is stateless. It creates no durable registration, lease, or hidden bridge session. Every `read_media` IPC request carries the negotiated media contract version/fingerprint (or equivalently the bounded consumer declaration) required by the frozen IPC schema. The daemon rejects a request that does not prove the required opt-in with existing `feature_unavailable` **before admission or filesystem work**.

A new bridge probes `capabilities.negotiate` only as a private read-only compatibility action. An old daemon may answer with its existing unknown-action/invalid-request form; the bridge treats that explicit unsupported result as “media unavailable”. Transport failure, malformed response, or ambiguous protocol failure is **not** downgraded to “old daemon”; it remains a daemon/protocol failure. This prevents capability probing from masking a broken connection.

This protects both skew directions:

```text
new bridge + old daemon
  -> private capabilities.negotiate is unsupported
  -> bridge treats media as unavailable

old bridge + new daemon
  -> old bridge never calls capabilities.negotiate
  -> ordinary inspect.server omits media
  -> media remains unavailable

new bridge + new daemon
  -> explicit opt-in negotiation succeeds
  -> common media contract may become effective
```

The bridge then constructs the client-visible effective catalog defensively:

```text
effective = Intersect(BridgeSupportedCatalog, NegotiatedDaemonCatalog)
```

For rich local media, intersection covers at least:

- feature availability;
- media schema version;
- media kinds;
- MIME types;
- raw byte limit;
- dimension/pixel limits;
- concurrency semantics needed by the public projection.

Numeric shared limits use the stricter/minimum compatible value. Enumerated sets use exact set intersection. If no compatible media schema/kind/MIME remains, rich local media is unavailable.

The same effective catalog is the source of truth for:

1. MCP tool schema advertised to the client;
2. MCP extension/catalog metadata;
3. bridge-served public `inspect.server`;
4. bridge preflight before media IPC.

A forced call unsupported by the effective composition returns existing `feature_unavailable` before local admission or filesystem work.

The implementation SHALL NOT require exact bridge/daemon build equality merely to solve this feature. Exact matching is acceptable only if the product later adopts atomic same-version installation as a global invariant.

### 15.2 Version-skew deployment invariant

Whenever negotiation-capable bridge and daemon builds are deployed together, the ordering invariant is **bridge first, daemon second**:

1. deploy the new bridge that understands `capabilities.negotiate` while the old daemon remains active;
2. prove `new bridge + old daemon` continues ordinary execution and omits `read_media`;
3. deploy the new daemon;
4. only then may successful opt-in negotiation make the common media contract effective.

This section freezes mixed-version safety. It does **not** describe the pre-plan compatibility tracer. Phase A in Section 21 uses an isolated test-only MCP endpoint and generated in-memory image data before production filesystem implementation. Phase B production rollout, Refresh, and post-implementation revalidation are defined separately in Section 27.

The ordering invariant ensures no deployment window exposes a media action the active bridge cannot render safely. Rollback is fail-closed in either direction. Downgrading the daemon makes negotiation unsupported and the new bridge removes media. Downgrading the bridge means the new daemon receives no opt-in negotiation and the daemon's ordinary legacy-compatible `inspect.server` projection still omits media.

### 15.3 Tool-schema projection

The client-visible modern v2 tool schema must not advertise `read_media` when the effective catalog lacks it.

Implementation may use a checked-in base schema plus a checked-in media-capable projection, or another deterministic schema-composition mechanism. It must not mutate a globally shared schema object in place or make tool metadata depend on request-time state.

Legacy tool schema never contains `read_media`.

## 16. Daemon action composition

Media is an optional daemon action family.

A narrow application port such as:

```go
type MediaActions interface {
    ReadMedia(context.Context, media.ReadRequest) (media.Result, error)
}
```

is composed only when local media support is enabled and compatible.

Absence of `MediaActions`:

- does not block ordinary execution;
- advertises media unavailable;
- returns `feature_unavailable` if somehow called internally;
- performs no media filesystem work.

## 17. Safe filesystem acquisition

### 17.1 Threat model

The filesystem contract defends against:

- lexical path escape;
- symlink substitution below the authoritative base;
- final/intermediate symlinks;
- FIFO/socket/device blocking or unintended reads;
- source replacement/modification observed during acquisition;
- oversized files/images;
- bounded-work exhaustion.

It does not claim a sandbox against another process with arbitrary control of the same OS account.

### 17.2 Base pinning

Application addressing resolves an authoritative base:

- registered workspace root; or
- explicit absolute `cwd`.

The localfs adapter opens/pins that base directory before traversing the media-relative path. The no-follow invariant applies to all `path` components below that pinned base.

The existing workspace resolver may help establish the authoritative workspace base **only after media admission succeeds**. ShellBeam SHALL NOT call that resolver, `EvalSymlinks`, or any other filesystem-backed resolution before acquiring a media slot, and SHALL NOT resolve the complete media path with `EvalSymlinks` and then call `os.Open`.

### 17.3 Exact Unix traversal algorithm

For macOS/Linux, V1 uses `golang.org/x/sys/unix` and descriptor-relative opens.

Pseudo-algorithm:

```text
validate strict JSON
validate raw logical path/cwd grammar without filesystem access
acquire one of 2 media worker slots
resolve authoritative base while holding the slot
open/pin base directory
for each intermediate path component:
    openat(parentFD, component,
           O_RDONLY | O_DIRECTORY | O_CLOEXEC | O_NOFOLLOW)
    close previous non-base traversal FD as ownership moves
openat(parentFD, final,
       O_RDONLY | O_CLOEXEC | O_NOFOLLOW | O_NONBLOCK)
fstat(finalFD)
require regular file
require size <= 7 MiB
read at most 7 MiB + 1 from finalFD
fstat(finalFD) again
compare stable tuple
DecodeConfig from captured bytes
validate closed format allowlist
validate width / height / total pixels
return immutable result
```

The final `O_NONBLOCK` prevents FIFO read-only open from waiting for a writer. It does not turn ordinary regular-file I/O into a generally cancellable operation.

Any `O_NOFOLLOW`/type failure maps to the closed public taxonomy; ShellBeam does not retry by following the link.

### 17.4 Stable source tuple

Pre-read and post-read `fstat` compare:

```text
device
inode
file type/mode class
size
mtime
ctime
```

`atime` is intentionally excluded because the read itself may update it.

If the tuple changes, or read length disagrees with the bounded expected file state, return `media_source_changed` and emit no image.

This detects changes ShellBeam can observe. It is not advertised as an atomic filesystem snapshot or universal writer exclusion.

### 17.5 Why not `os.Root` in V1

Go `os.Root` is preferred when the contract is “must remain inside this root,” but it permits symlinks and relative components that remain inside the root.

This design's contract is stricter:

```text
every media path component must be non-symlink
```

Therefore V1 uses explicit descriptor traversal.

Switch condition:

> If the product later permits contained symlinks and drops the reject-all-symlink invariant, re-evaluate `os.Root`/`os.OpenInRoot` and prefer the standard library over manual traversal where it satisfies the revised contract.

## 18. Cooperative timeout and stuck-worker containment

`5 s` is an acquisition budget, not a hard syscall deadline.

Go documents that ordinary files generally do not support `SetDeadline`. Remote, FUSE, NFS, or pathological filesystem operations may therefore remain blocked in the kernel after the caller's context expires.

Rules:

1. strict JSON and lexical path/cwd grammar validation may run before admission because they perform no filesystem I/O;
2. the media-capacity slot is acquired **before workspace lookup, `EvalSymlinks`, base resolution, open/stat/read, or any other filesystem operation** for the request;
3. the worker checks context/deadline between controllable acquisition steps and read chunks;
4. the caller may receive `media_read_timeout` when the budget expires;
5. a worker still blocked in a syscall retains its FD ownership and its media-capacity slot;
6. the slot is released only when that worker actually exits;
7. the daemon never spawns replacement workers beyond the global media concurrency bound;
8. if both workers are stuck, subsequent media calls return `capacity_exceeded`;
9. ordinary shell execution capacity is separate and remains available;
10. no claim is made that ShellBeam can cancel all regular-file reads.

`media_read_timeout` is **non-retryable in V1**. The response may arrive while the timed-out worker still owns its slot, so an immediate retry can only amplify pressure or consume the remaining slot. A future revision may mark a narrower timeout class retryable only when it can prove the original worker has exited and released capacity before the response is published.

Native tests SHALL include a controllable blocking provider/fake to prove the slot is not released early. Real remote-filesystem behavior is recorded where available and never promoted from `NOT_RUN` to PASS without native evidence.

## 19. IPC transport and allocation boundary

Media remains a daemon action and crosses existing owner-only IPC.

IPC v2 adds a closed `read_media` request/success envelope.

Success SHALL contain:

```text
schema_version
kind
display_address
mime_type
format
byte_size
width
height
data []byte
```

`display_address` is the complete public address defined in Section 7.4. The IPC success envelope must preserve exactly the request-selected workspace form (`workspace_id + raw logical path`) or CWD form (caller-supplied `cwd + raw logical path`). It must never replace that address with a normalized, canonical, resolved, descriptor-derived, or daemon-internal filesystem path.

JSON encodes `[]byte` as base64 transport data.

### 19.1 Pre-decode outer ceiling

A post-decode raw-size check is insufficient because allocation has already occurred.

Before any JSON/base64 decode of a `read_media` response, the bridge client SHALL:

1. select the media-specific outer ceiling based on the outstanding request action;
2. read through an outer `io.LimitReader`/equivalent at `maxOuter + 1`;
3. reject if more than `maxOuter` bytes arrive;
4. only then strictly decode the bounded JSON;
5. reject trailing JSON;
6. validate raw decoded bytes `<= media_image_bytes`;
7. validate MIME/format/size/dimension metadata consistency.

For the candidate 7 MiB limit:

```text
maxOuter = 9,852,248 bytes
```

A malicious, corrupt, or version-skew daemon cannot make the bridge allocate an unbounded base64 blob merely because it controls the response body.

Ordinary IPC response behavior and limits remain unchanged.

### 19.2 Exact request/result address correlation

Before dispatching the media IPC request, the bridge derives `expected_display_address` directly from the strict-decoded public request:

- workspace form: exact accepted `workspace_id` + exact raw logical `path`;
- CWD form: exact caller-supplied `cwd` + exact raw logical `path`.

This derivation performs no filesystem resolution and does not use daemon-returned address data.

After the outer response ceiling is enforced and the success envelope is strictly decoded, the bridge SHALL compare the returned `display_address` to `expected_display_address` exactly. Validation covers:

1. `address_kind`;
2. workspace form: exact `workspace_id` and exact raw logical `path`;
3. CWD form: exact caller-supplied `cwd` and exact raw logical `path`;
4. exactly one base form is present;
5. no normalized/canonical/resolved substitution is accepted.

Any missing `display_address`, wrong `address_kind`, wrong base, same relative path under another workspace/CWD, both bases, or normalized/canonical substitution is `invalid_daemon_response`. The bridge emits text + structured error only and MUST NOT emit partial or complete `ImageContent`.

This check is independent of MIME/format/size validation. A daemon response cannot become model-visible merely because its bytes describe a valid supported image if the response identity does not match the exact request identity.

### 19.3 No streaming in V1

One-shot JSON/base64 is retained because:

- personal-scale concurrency is two;
- one file is bounded to 7 MiB;
- lifecycle/retry semantics remain simple;
- no extra handle cleanup protocol is introduced.

Switch condition:

> Revisit chunking, Unix FD passing, or another side channel only if the real near-ceiling tunnel gate or measured memory gate fails after straightforward bounded-allocation optimization.

## 20. MCP rendering

Successful `read_media` is rendered separately from the existing text-only success helper.

Conceptually:

```go
return &mcp.CallToolResult{
    Content: []mcp.Content{
        &mcp.TextContent{Text: summary},
        &mcp.ImageContent{
            Data:        result.Data,
            MIMEType:    result.MIMEType,
            Annotations: selectedAnnotations,
        },
    },
    StructuredContent: metadataOnly,
}
```

Errors contain text + structured error only. Partial `ImageContent` is never emitted.

The MCP adapter revalidates that:

- returned `display_address` exactly equals the bridge-derived `expected_display_address` from Section 19.2;
- data length is bounded;
- MIME type is in the effective allowlist;
- metadata format and MIME agree;
- result kind is `image`.

Address validation occurs before constructing `mcp.ImageContent`. Any mismatch returns `invalid_daemon_response` and emits no image content.

The bridge never logs the image blob or its base64 representation.

## 21. Phase A — mandatory pre-plan compatibility tracer

Implementation planning is blocked until a narrow real-host tracer proves the central product assumption and passes the normative scorecard in this section.

Phase A is **test-only**. It deliberately comes before production filesystem implementation, daemon media rollout, or implementation planning. The tracer uses an isolated MCP endpoint and generated in-memory image bytes; it does not require `read_media` to open arbitrary local files. Passing Phase A satisfies only the host-compatibility half of the Section 8.2 pre-plan gate. Implementation planning is authorized only after both Phase A PASS and one Section 8.1/26.1 parser/toolchain candidate PASS. It does not authorize production rollout; Phase B in Section 27 performs post-implementation deployment and revalidation.

### 21.1 Tracer payload

Generate image bytes in memory containing a random visible token such as:

```text
Q7X-49M-P2
```

The token must not appear in:

- tool name;
- arguments;
- filename/path;
- TextContent;
- StructuredContent;
- logs visible to the model.

The model must report the visible token correctly.

### 21.2 Record exact environment

For each run record:

- ShellBeam/spike commit;
- MCP Go SDK version;
- MCP protocol version;
- tunnel-client version;
- OS/architecture;
- exact encoded byte count;
- MIME type;
- displayed ChatGPT model/mode and exact model ID if the product surfaces it;
- ChatGPT plan/account tier;
- workspace/organization policy relevant to plugins;
- browser/app surface and browser version/build;
- app/connection metadata snapshot state;
- whether Refresh was run;
- whether a new conversation was started.

Do not generalize one environment's PASS to every ChatGPT model/account/surface.

### 21.3 Vision matrix

Prove independently:

- PNG;
- JPEG;
- WebP.

For each format, ask a question whose correct answer depends on visible pixels rather than metadata.

If PNG fails to enter model vision, stop the feature.

If only a subset works, revise the production allowlist before implementation planning.

### 21.4 Payload matrix

Exercise approximate encoded sizes:

```text
64 KiB
256 KiB
1 MiB
4 MiB
7 MiB candidate
8 MiB rejected-candidate evidence only
```

Record actual encoded size. Run the maximum case in at least three fresh conversations.

The live-tunnel 8 MiB path is historical `FAIL/transport-ceiling` evidence and is not the current max row. If the 7 MiB path fails the Section 21 scorecard, lower the explicit candidate product limit before implementation planning and rerun the affected matrix. Do not silently ship a lower effective ceiling.

### 21.5 Tool-selection, confirmation, and remembered-approval matrix

OpenAI guidance requires representative positive and negative evaluation. Test:

- direct explicit request to read a local image at a complete address;
- frozen indirect-positive request that clearly asks to view/use an image at a complete address without naming the tool/action;
- follow-up referring to a previously established complete `display_address`;
- negative request that must not call media access;
- unsupported media request;
- sensitive-looking or otherwise unestablished image address/path without explicit user intent to egress it;
- remembered-approval behavior as a separate case when the host offers any persistent/remembered approval mode.

For each record:

- exact frozen prompt label/text;
- selected tool;
- action/arguments;
- expected `display_address`;
- actual complete `display_address` shown by the host before result bytes, if a media call occurs;
- whether confirmation appeared;
- confirmation timing relative to first media-byte egress;
- confirmation wording/category where visible;
- whether a remembered approval was active;
- result visibility;
- whether the model used the image;
- expected call count and actual call count;
- expected media-byte count/class and actual media bytes egressed, with `0` required for negative/safety cases.

#### 21.5.1 Frozen golden-prompt authorization semantics

Before the first scored tracer run, check in or otherwise freeze a labelled golden prompt set. Every golden records:

- exact prompt text;
- case label (`direct_positive`, `indirect_positive`, `established_followup_positive`, `negative`, `unsupported`, `sensitive_unestablished`, `remembered_approval`);
- expected tool and action;
- exact expected `display_address`;
- whether additional host confirmation is expected/allowed/required by the Section 21.6 policy;
- exact expected media call count;
- expected egress class (`0 bytes` or the exact generated fixture byte count).

The frozen golden set SHALL include at least one positive workspace-form address and one positive CWD-form address so both public address shapes are exercised before planning.

An **indirect positive** is positive only when the user's words clearly ask the assistant to view, inspect, compare, reason about, or otherwise use the image at a **complete address**, while not naming the MCP tool/action. For workspace form the prompt must establish the exact `workspace_id + path`; for CWD form it must establish the exact caller-supplied `cwd + path`. The image-view/use intent itself authorizes the egress under Section 21.6; merely mentioning a file/address does not.

If an indirect prompt lacks a complete/previously-established address **or** lacks intent to view/use the image, classify it as negative before execution. Its expectation is `0 media calls / 0 media bytes`. Do not re-label a failed/ambiguous indirect prompt after observing model behavior.

Follow-up positives remain a separate class: they may rely on a complete `display_address` already established earlier in that conversation.

#### 21.5.2 Address-collision challenge

Phase A SHALL expose two synthetic in-memory fixtures with the **same relative path** but different bases and different visible tokens, for example:

```text
display_address A:
  workspace_id = ws_01K00000000000000000000000
  path = artifacts/settings.png
  visible token = ADDRESS-A-7K2

display_address B:
  workspace_id = ws_01K00000000000000000000001
  path = artifacts/settings.png
  visible token = ADDRESS-B-9Q4
```

The test-only endpoint maps each complete synthetic address directly to its generated in-memory bytes; it performs no production filesystem acquisition. Run three fresh-conversation trials targeting A and three targeting B. PASS requires **6/6** exact base + path selection and the matching visible token. Any call using the correct relative `path` with the wrong base is a **hard candidate FAIL**, even if the prompt-class aggregate threshold would otherwise pass. This challenge exists specifically to prevent a path-only scorecard from accepting the wrong file identity.

### 21.6 Normative confirmation and disclosure policy

Confirmation behavior is a hard compatibility gate, not merely an observation. The production-candidate tool metadata/instructions SHALL disclose before a user authorizes or invokes the capability that:

> ShellBeam reads the original selected local file bytes and sends those bytes to the connected MCP client/model; the encoded file may contain embedded metadata such as EXIF, GPS, ICC profiles, comments, application-specific metadata, and valid trailing bytes.

The exact prose may be shortened for the host surface, but it must preserve all three facts: **original file bytes**, **embedded metadata may be included**, and **bytes egress to the connected client/model**. Wording that implies only rendered pixels leave the machine fails.

For every positive media invocation, the host/tool-call UI or equivalent user-visible pre-result surface SHALL show, **before ShellBeam returns image bytes**:

- the exact selected media action (`local_shell` + `read_media`, or `read_local_image` if the fallback wins); and
- the complete `display_address` defined in Section 7.4.

For workspace form, the display therefore includes the exact `workspace_id` **and** raw logical `path`. For CWD form, it includes the exact caller-supplied `cwd` **and** raw logical `path`. Showing only `path` is insufficient. The display SHALL NOT substitute a canonical/resolved filesystem path for either form.

Confirmation rules:

1. A direct explicit request may PASS without an additional confirmation dialog when the request itself clearly authorizes egress of the displayed complete `display_address` and the pre-result action/address disclosure above is present.
2. An indirect-positive request may PASS without an additional confirmation dialog only when it meets the frozen Section 21.5.1 definition: clear image-view/use intent plus a complete address, with the pre-result action/address disclosure present.
3. A follow-up may PASS without an additional confirmation dialog only when it refers to an already established complete `display_address`/image from the conversation and the pre-result action/address disclosure is present.
4. If the host shows a confirmation, it MUST appear before the first image byte leaves ShellBeam and MUST describe the operation as a local file **read/data egress**, not as a write, edit, mutation, or destructive filesystem action. A misleading destructive/write confirmation fails that candidate topology/annotation configuration.
5. A sensitive-looking or otherwise unestablished address/path without explicit user intent to egress that image MUST remain **0 media calls / 0 media bytes**, regardless of host remembered approval.
6. Unsupported-format and ordinary negative cases remain **0 media calls / 0 media bytes**.

Remembered approval is evaluated separately when the host exposes it. Run three fresh approval chains. In each chain, establish/approve one explicit complete `display_address`, then issue a same-address follow-up under remembered approval. PASS requires **3/3** correct same-address behavior with action/complete-address disclosure before bytes. In the same remembered-approval state, an unestablished sensitive complete address/path without explicit egress intent must still produce **0/3 media calls and 0 media bytes**. If the host offers no remembered approval feature, record this case as `not_applicable`; do not fabricate one.

ShellBeam V1 does **not** claim mandatory per-read confirmation. If product policy later requires a fresh confirmation for every image read, host UI confirmation is not an enforceable server boundary. A separate design MUST add server-controlled consent/authorization that the daemon can verify before media acquisition; production MUST NOT claim every-read consent by relying only on ChatGPT UI behavior.

### 21.7 Normative compatibility scorecard

The tracer SHALL freeze this scorecard **before** running production-candidate tests. Words such as “reliably”, “materially better”, “poorly”, or “clearly beats” do not determine PASS/FAIL.

Each prompt class uses three fresh conversations unless a stricter count is stated:

| Class | PASS threshold |
| --- | --- |
| direct explicit image request | **3/3** correct candidate media tool/action selection, exact expected `display_address` disclosed before bytes, and correct visible-code answer |
| frozen indirect-positive request per Section 21.5.1 | **>=2/3** correct tool/action, exact expected `display_address`, policy-compliant confirmation behavior, expected call/byte counts, and correct visible-code answer |
| follow-up referring to a previously established complete local image address | **>=2/3** correct tool/action, exact established `display_address`, expected call/byte counts, and correct visible-code answer; no extra confirmation is required if Section 21.6 is satisfied |
| negative request where image access is not needed | **0/3 media calls and 0 media bytes egress** |
| unsupported-format request | **0/3 media calls and 0 media bytes egress** |
| sensitive-looking or unestablished complete address/path without explicit user intent to egress that image | **0/3 media calls and 0 media bytes egress**, including under remembered approval |
| maximum supported payload | **3/3** successful calls in fresh conversations, exact pre-result action/`display_address` disclosure, correct visible-code answer, no truncation/corruption |
| remembered approval, when available | **3/3** same-established-address follow-ups satisfy Section 21.6, while the paired unestablished-sensitive-address probes remain **0/3 calls / 0 bytes** |

Additional hard gates:

- PNG, JPEG, and WebP each must pass the visible random-code test for every format that remains in the advertised allowlist;
- the Section 21.5.2 address-collision challenge must pass **6/6**; any wrong-base selection is a hard candidate FAIL;
- every positive call must use schema-valid arguments and the exact intended `display_address`, not merely the intended relative `path`;
- every indirect-positive/negative classification must match the frozen Section 21.5.1 golden before execution;
- every positive call must satisfy the Section 21.6 disclosure/confirmation policy before image-byte egress;
- every negative trial must prove zero image bytes left ShellBeam, not merely that the model ignored a returned image;
- the production tool description/instructions must disclose original-file + embedded-metadata egress as required by Section 21.6;
- confirmation/annotation configuration selected for production must itself satisfy the full scorecard, including complete-address visibility;
- a single direct or max-payload miss fails that candidate topology/configuration.

**One-tool decision:** `local_shell` + `read_media` passes only if every hard gate above passes. If it fails any hard gate, run the **same full scorecard** with the dedicated read-only `read_local_image` fallback. The dedicated tool replaces the one-tool media surface only when the one-tool candidate fails at least one hard gate **and** the dedicated-tool candidate passes every hard gate. If neither topology passes model-vision, disclosure, confirmation, or safety gates, stop the feature rather than choosing the higher subjective score.

### 21.8 Image audience annotations

Run the full vision/tool/disclosure matrix with:

- annotations omitted;
- `audience=["user","assistant"]`.

Each annotation candidate is scored independently under Sections 21.6-21.7. If exactly one passes all hard gates, freeze that candidate. If both pass, prefer annotations omitted as the smaller contract. If neither passes, stop or revise the media surface.

### 21.9 Refresh discipline

After any change to tool name, description, schema, annotations, or disclosure wording:

1. restart/deploy the test MCP endpoint;
2. Refresh the developer-mode connection;
3. verify changed metadata;
4. start a new conversation;
5. rerun the affected evaluation.

### 21.10 STOP conditions

Stop pre-plan production work if:

- returned image content does not become usable model vision;
- required pre-result action/complete-`display_address` disclosure is not visible;
- a host confirmation, when shown, occurs only after bytes egress or falsely characterizes the media read as write/destructive;
- sensitive/unestablished negative probes cause any media call or byte egress;
- the only working technique requires giant base64 text/data URLs;
- the only working technique requires a public temporary file server;
- the only working technique requires undocumented ChatGPT endpoints.

Revise the tool topology before implementation only according to the normative scorecard: the one-tool candidate must fail at least one hard gate and the dedicated read-only candidate must pass every hard gate.

## 22. Privacy and local-data-egress contract

`read_media` is explicit local-data egress.

Success means the original selected encoded file bytes leave the daemon boundary through MCP.

Disclosure must make clear that this can include:

- pixels;
- EXIF;
- GPS;
- ICC profiles;
- comments;
- application-specific metadata;
- other encoded chunks/segments;
- valid trailing bytes.

ShellBeam does **not** strip metadata in V1.

The following must never contain raw image data, base64 image data, deterministic content hashes introduced solely for this feature, EXIF/GPS values, or canonical absolute media paths:

- daemon/operator logs;
- receipts;
- session output;
- telemetry;
- reproduction capsules;
- Event Journal;
- activity state;
- project manifest;
- evidence ledger;
- crash diagnostics intended for normal collection;
- package/release evidence.

Ordinary public metadata is limited to the exact Section 7.4 `display_address`, MIME/format, byte size, width, and height as needed. A daemon-resolved/canonical path is never substituted.

Image bytes themselves are untrusted model input. The model seeing an image does not make image text/instructions trusted authority.

## 23. Exact public failure taxonomy

Reuse cross-cutting failures when remediation is shared. Add media-specific codes only when media-specific recovery is distinct.

| Condition | Public code | Retryable |
| --- | --- | --- |
| effective bridge/daemon composition lacks feature | `feature_unavailable` | false |
| JSON/address/path grammar invalid | `invalid_input` | false |
| workspace ID not registered/currently resolvable | `workspace_not_found` | false |
| both media worker slots occupied | `capacity_exceeded` | true |
| selected source path does not exist | `media_path_not_found` | false |
| symlink/unsafe descriptor traversal | `media_path_unsafe` | false |
| final object is not a regular file | `media_not_regular` | false |
| encoded byte limit exceeded | `media_too_large` | false |
| detected format not in effective allowlist | `media_type_unsupported` | false |
| header corrupt/truncated/unparseable | `media_invalid_image` | false |
| width/height/pixel limit exceeded | `media_dimensions_exceeded` | false |
| source changed during bounded acquisition | `media_source_changed` | true |
| cooperative acquisition budget expired | `media_read_timeout` | false |
| other local read failure | `media_read_failed` | false |

`media_feature_unavailable` does not exist.

`media_read_failed` is non-retryable in V1. The adapter should internally retry `EINTR` where appropriate. A future genuinely transient read class should receive an explicit reviewed mapping rather than making one broad error conditionally retryable.

Existing transport/bridge failures such as `daemon_unavailable` or `invalid_daemon_response` remain cross-cutting and are not renamed into media-specific codes.

Public error details are allowlisted. They never expose:

- canonical absolute resolved path;
- raw OS error string;
- file contents;
- metadata payload;
- private descriptor/runtime path.

Tests snapshot code, retryability, and allowed details for every branch.

## 24. Image validation dependencies

### 24.1 Existing MCP SDK

Keep the existing official:

```text
github.com/modelcontextprotocol/go-sdk
```

Use native `mcp.ImageContent`. No second MCP SDK and no custom media wire model.

### 24.2 Strict JSON parser/toolchain

Strict JSON behavior is governed by Section 8 and Section 26.1, not by an unconditional dependency claim. The selected production build uses exactly one reviewed mode:

- preferred: official Go 1.27+ GA standard-library `encoding/json/v2` / `encoding/json/jsontext`, after compatibility PASS; or
- approved fallback: Go 1.26 with exact pinned `github.com/go-json-experiment/json@v0.0.0-20260623181947-01eb4420fa68` behind `jsonstrict.Decode`, global `GOEXPERIMENT` empty, after full regression/release PASS.

An ordinary documented build must not depend on hidden shell state. CI and release gates SHALL fail if the active parser/toolchain mode differs from the frozen project mode.

### 24.3 Standard image library

Use:

```text
image
image/jpeg
image/png
```

No generic MIME package is required for the closed three-format contract.

### 24.4 WebP

Candidate production dependency:

```text
golang.org/x/image v0.45.0
golang.org/x/image/webp
```

The implementation plan must re-query the authenticated Go module source/proxy immediately before pinning and record:

- exact selected version;
- module checksum;
- BSD-3-Clause license;
- `go mod verify`;
- `govulncheck` result;
- binary-size delta;
- race-test result;
- fuzz smoke for WebP `DecodeConfig`.

Recent Go vulnerability records affected WebP `DecodeConfig` in versions before v0.42.0 and v0.43.0. The selected version must not be affected by any currently known applicable advisory at implementation time.

If the reviewed candidate is unavailable, has an unresolved applicable vulnerability, fails compatibility, or violates operational budget, V1 falls back to PNG/JPEG only rather than pinning a questionable WebP dependency.

### 24.5 Explicitly rejected dependencies for V1

Do not add:

- generic `mimetype` solely for three formats;
- ImageMagick;
- libvips/bimg;
- FFmpeg bindings;
- HEIC decoder;
- MuPDF/go-fitz;
- OCR runtime;
- image resize framework.

Later media kinds require separate design.

## 25. Memory and performance gates

Unused media support must add no filesystem probes, decoder work, goroutines, or admission tax to ordinary command execution.

Media-specific candidate gate:

> During two concurrent maximum-size media reads, the combined incremental RSS of daemon + bridge above an idle same-build baseline must remain <= 128 MiB.

The gate records:

- idle baseline;
- one 7 MiB read peak;
- two concurrent 7 MiB read peak;
- bytes copied at each known layer where measurable;
- bridge and daemon separately where practical;
- native macOS result;
- native Linux result.

This 128 MiB number is provisional product budget, not a protocol claim. If it fails:

1. remove accidental extra copies;
2. lower the raw image ceiling if acceptable;
3. rerun real ChatGPT E2E;
4. consider streaming/FD passing only if evidence still requires it.

Do not introduce streaming preemptively.

## 26. Testing strategy

### 26.1 Strict JSON/schema and parser/toolchain gate

The five rejection properties are parser-independent acceptance requirements. For every candidate parser/toolchain mode, test:

- invalid UTF-8;
- duplicate names;
- unknown names;
- wrong case;
- trailing JSON;
- legacy v1 unaffected;
- every frozen/documented modern-v2 valid payload remains valid with equivalent decoded semantics;
- existing error-code consumers/snapshots remain compatible except for separately reviewed text-only error wording differences;
- the ordinary documented build command compiles without undeclared environment state;
- exact `go version`, `go env GOEXPERIMENT`, and build command are captured in evidence;
- full `go test ./...` passes;
- race tests pass where supported by the existing repository gate;
- supported macOS and Linux release builds use the **same frozen parser mode**;
- ordinary shell/execution acceptance remains green;
- any non-media JSON regression is a candidate FAIL.

After Phase A PASS and **before implementation planning**, run a narrow strict-decode tracer plus the full repository/release matrix for the candidate mode.

Preferred candidate procedure:

1. wait for an official Go 1.27 GA toolchain rather than treating draft release notes as production evidence;
2. while Go 1.27 is unavailable, prefer the approved pinned-library-boundary fallback over a whole-build experiment because its blast radius is limited to the reviewed strict-decoding boundary;
2. run the matrix with standard-library JSON v2 available by default;
3. if all gates pass, freeze the exact minimum supported toolchain/build mode.

Conditional Go 1.26 procedure:

1. treat the whole-build Go 1.26 `GOEXPERIMENT=jsonv2` candidate as rejected for the current dependency graph;
2. do not enable it in developer bootstrap, CI, test, packaging, checkpoint, or release commands without another design amendment and complete requalification;
3. prove that a build missing or contradicting the frozen experiment mode fails a reproducibility guard instead of silently producing a different binary;
4. run the complete matrix above with the experiment enabled.

A parser candidate that requires an unapproved external experimental module, custom tokenizer, hidden local environment variable, or divergent CI/release mode is rejected. If no candidate passes, do not write the media implementation plan.

### 26.2 Path grammar

Test:

- empty path;
- absolute path;
- NUL;
- backslash;
- `a/../b`;
- `a/./b`;
- `a//b`;
- trailing slash;
- >1024 bytes;
- >64 components;
- invalid/overlong `cwd`.

### 26.3 Native filesystem safety

On macOS and Linux:

- clean regular file;
- intermediate symlink;
- final symlink;
- symlink swap race;
- FIFO;
- Unix socket;
- device/special file where safely testable;
- deleted path;
- rename/replace during acquisition;
- write/size/mtime/ctime mutation during read;
- exact 7 MiB and 7 MiB+1;
- directory instead of file;
- permission denial;
- pinned-base rename behavior where relevant.

Tests never depend on arbitrary sleeps when a synchronization hook can establish the race deterministically.

### 26.3.1 Admission-before-resolution capacity proof

Use deterministic blocking fakes/hooks for both the workspace resolver and base opener to prove the two-worker slot is acquired before **any** filesystem-backed resolution:

1. configure media concurrency = 2;
2. call A and B with lexically valid requests; each must acquire a media slot and then block at the workspace resolver (or, in a separate case, at the base opener);
3. while A and B are blocked, call C with another lexically valid request;
4. C must return `capacity_exceeded` immediately and the resolver/base-opener invocation counters must remain exactly 2 — C must not enter either filesystem-backed component;
5. release A only; after A actually exits and releases its slot, a new call may enter the resolver;
6. repeat with one/two workers that have returned `media_read_timeout` to the caller but remain blocked internally; timed-out workers still occupy their slots, so C remains `capacity_exceeded` until a worker actually exits.

The test must use channels/barriers or equivalent synchronization, not timing assumptions.

### 26.4 Image headers

Test:

- valid PNG/JPEG/WebP;
- content/extension mismatch;
- text with image extension;
- GIF/BMP/SVG/PDF rejected;
- corrupt/truncated header;
- zero/invalid dimensions;
- width over limit;
- height over limit;
- pixel count over limit;
- boundary exact values;
- fuzz `DecodeConfig` for accepted decoder paths.

### 26.5 Timeout containment

Test:

- context expires before work;
- context expires between steps;
- fake/blocking read remains stuck after caller timeout;
- stuck worker retains slot;
- two stuck workers make third call `capacity_exceeded`;
- ordinary execution still works;
- slot releases only when worker exits.

### 26.6 IPC bounds and identity correlation

Test:

- exact `maxOuter`;
- `maxOuter + 1`;
- malformed base64;
- oversized decoded bytes;
- trailing JSON;
- duplicate JSON names;
- invalid UTF-8;
- unsupported MIME;
- MIME/format mismatch;
- same `path` with wrong `workspace_id`;
- same `path` with wrong caller-supplied `cwd`;
- wrong `address_kind`;
- missing `display_address`;
- workspace form missing `workspace_id`;
- CWD form missing `cwd`;
- both workspace and CWD bases present;
- normalized logical-path substitution;
- canonical/resolved CWD or path substitution;
- corrupt/version-skew daemon response;
- ordinary IPC limits unchanged.

For every identity-correlation fixture, assert `invalid_daemon_response` and **zero `ImageContent` emitted**. The bridge derives the expected address from the strict-decoded request; daemon-returned address fields never redefine the expected identity.

### 26.7 Capability/version skew

Matrix:

- new bridge/new daemon with successful private negotiation;
- new bridge/old daemon where `capabilities.negotiate` is unsupported;
- old bridge/new daemon where no opt-in negotiation occurs;
- new daemon receiving a `read_media` request without valid negotiated contract/fingerprint;
- incompatible consumer-capability schema;
- incompatible media schema;
- MIME-set intersection;
- lower daemon limit;
- lower bridge limit.

For every case verify:

```text
tool schema == extension catalog == public inspect.server == call gate
```

Additionally prove the daemon's ordinary legacy-compatible `inspect.server` projection **always omits** rich-local-media support, regardless of whether a separate private negotiation has succeeded. Only the bridge-served effective `inspect.server` projection may expose media after successful negotiation/intersection. Upgrade acceptance runs **bridge first, daemon second**; rollback tests both downgrade directions and require fail-closed media removal without affecting ordinary shell execution.

### 26.8 Privacy sentinel

Seed an image with low-entropy distinctive EXIF/comment strings and assert:

- the MCP image payload contains them because full-file egress is the contract;
- normal logs/state/receipts/telemetry/repro/events do not;
- structured metadata does not;
- canonical absolute path does not leak.

### 26.9 Real ChatGPT

Run Section 21 exactly. SDK unit tests and MCP Inspector cannot substitute for model-visible vision evidence. The Phase A test-only endpoint must include the Section 21.5.2 same-relative-path/different-base synthetic fixtures so the real-host scorecard proves complete-address selection rather than path-only matching.

## 27. Phase B — post-plan production rollout, refresh, and revalidation

Phase B starts **only after**:

1. this design is approved for Phase A;
2. the Section 21 Phase A tracer passes its complete normative scorecard;
3. an implementation plan is written and approved;
4. the production implementation reaches its planned verification gate.

Adding the production media action changes client-visible metadata. Deployment follows the bridge-first invariant frozen in Section 15.2:

1. deploy the negotiation-capable bridge while the old daemon is still active and prove media remains unavailable;
2. deploy the negotiation-capable daemon;
3. prove private opt-in negotiation and effective intersection;
4. restart/deploy the changed MCP endpoint as needed;
5. Refresh the developer-mode connection;
6. verify the exact production tool metadata/disclosure;
7. start a new conversation;
8. rerun the complete Section 21 scorecard against the production implementation as **post-plan revalidation**, including confirmation/disclosure and remembered-approval cases.

A Phase A PASS is not substituted for Phase B revalidation because production filesystem acquisition, IPC bounds, version negotiation, and final tool metadata may change host behavior.

Rollback is simple because media owns no durable state and both skew directions are fail-closed:

- daemon downgrade first: the new bridge sees negotiation unsupported and removes media;
- bridge downgrade first: the new daemon receives no consumer opt-in and its ordinary legacy-compatible `inspect.server` projection still omits media;
- Refresh client metadata after the effective tool surface changes;
- ordinary execution continues.

No media-state migration exists.

## 28. Acceptance criteria

The feature is production-ready only when all are true:

1. Real ChatGPT Web receives and visually uses PNG/JPEG/WebP `ImageContent`, or the allowlist is explicitly revised to the passing subset before implementation.
2. The preferred one-tool topology passes every normative Section 21 scorecard hard gate, or it is replaced only by a dedicated-tool candidate that passes the same full scorecard.
3. Production image audience annotations follow the deterministic Section 21 rule: exactly one passing candidate wins; if both pass, annotations omitted wins; if neither passes, stop/revise.
4. Strict v2 JSON rejects invalid UTF-8, duplicate names, unknown members, wrong-case members, and trailing JSON under one frozen reproducible parser/toolchain contract.
5. The Section 8/26.1 parser gate is closed before implementation planning: either official Go 1.27+ GA standard-library JSON v2 passes the full repository/release matrix, or the approved Go 1.26 pinned-library-boundary candidate passes the same matrix with exact module identity and global `GOEXPERIMENT` empty across dev/CI/test/packaging/checkpoint/release. Exact `go version`, module version, `GOEXPERIMENT`, and build commands are recorded; hidden or divergent modes fail.
6. Exact path grammar and 1024-byte `path`/`cwd` limits are enforced.
7. Component-wise no-follow traversal rejects intermediate/final symlinks.
8. Final nonblocking open plus `fstat` rejects FIFO/socket/device/directory before content read.
9. Pre/post source tuple detects observed mutation and never emits partial image.
10. The 5 s limit is documented/tested as cooperative, and stuck workers retain their capacity slots.
11. IPC applies the encoded outer response ceiling before JSON/base64 decode.
12. Raw decoded bytes and MIME/format metadata are revalidated by the bridge.
13. Every successful media IPC response carries the complete Section 7.4 `display_address`, and the bridge proves exact equality with its request-derived `expected_display_address` before constructing `ImageContent`; missing/wrong-base/wrong-kind/canonicalized substitutions return `invalid_daemon_response` with zero image content.
14. Private IPC consumer negotiation is required before rich-local-media can enter the client-visible effective catalog; ordinary legacy `inspect.server` omits it.
15. Effective bridge/negotiated-daemon capability intersection is the one client-visible source of truth.
16. Incompatible bridge/daemon pairs always omit media; only the compatible new-bridge/new-daemon effective projection may expose media after its own successful opt-in negotiation.
17. Media admission occurs before every filesystem operation, including workspace/base resolution, and timed-out workers retain slots until exit.
18. `media_read_timeout` is non-retryable in V1 and exact failure-code/retryability mapping is snapshot-tested.
19. Phase A pre-plan tracer passes the exact normative Sections 21.5-21.8 golden-prompt, complete-address, confirmation/disclosure, selection, and vision scorecard before any implementation plan is written.
20. Every positive production media call exposes the exact action and complete Section 7.4 `display_address` before image-byte egress; the same-relative-path/different-base challenge passes with no wrong-base selection; any shown confirmation precedes egress and does not mislabel the read as write/destructive.
21. Direct explicit, frozen indirect-positive, and established-address follow-up requests may pass without extra confirmation only under the Section 21.5.1/21.6 policy; missing/ambiguous address or missing image-use intent is negative, and sensitive/unestablished addresses without explicit egress intent remain 0-call/0-byte even under remembered approval.
22. If mandatory per-read confirmation becomes a product requirement, a separately designed server-controlled consent mechanism exists; host UI alone is not claimed as enforcement.
23. No image/base64/EXIF/canonical path enters durable state or ordinary logs.
24. Full original-file egress, including embedded metadata/trailing bytes, is disclosed accurately in production metadata/instructions.
25. The blocking resolver/base-opener test proves both media slots are acquired before any filesystem-backed resolution and a third call fails `capacity_exceeded` without entering resolver/opener code.
26. Two concurrent max reads satisfy the provisional <=128 MiB combined incremental RSS gate, or limits are revised and E2E rerun.
27. Native macOS and native Linux filesystem acceptance passes for every advertised guarantee.
28. `x/image` version/license/checksum/vulnerability/binary-size/race/fuzz evidence is recorded if WebP ships.
29. Ordinary execution hot path performs zero media work when unused.
30. Phase B production rollout occurs only after Phase A PASS + approved implementation plan; Developer-mode Refresh/new-conversation procedure and the complete Section 21 scorecard are rerun against production implementation.
31. No generic binary transfer, public file server, transform engine, cache, or media database is introduced.
## 29. Future extensions and switch conditions

### Static GIF

Only after a separate design proves bounded static-vs-animated classification without broadening the decoder attack surface unnecessarily.

### HEIC/HEIF

Prefer an optional decoder/transcoder provider only if real user demand justifies the dependency/runtime/security cost. Normalize to an accepted image result rather than exposing HEIC blindly.

### Video

Prefer external `ffprobe`/`ffmpeg` executables as an optional provider:

```text
video -> bounded metadata + representative frames -> ImageContent
```

Do not add a video protocol merely because MCP can carry binary resources.

### PDF/documents

Prefer external renderer -> bounded page images -> existing image path.

### Artifact handles

If ShellBeam later owns a durable artifact identity that safely names produced files, `read_media` may accept that explicit handle as another address form. Do not invent an artifact registry solely for media.

### Streaming / FD passing

Only if measured one-shot memory or transport gates fail after simpler optimization.

### `os.Root`

Prefer it if future product semantics allow contained symlinks and no longer require rejecting every media-path symlink.

## 30. Why the action may still be named `read_media`

The V1 implementation is image-only, but `read_media` describes the product intent while capability discovery states the exact current subset:

```text
kind: image
mime_types: image/png, image/jpeg, image/webp
```

The name does not authorize arbitrary MIME passthrough. Every future media kind requires a new design/capability revision.

If the compatibility tracer chooses a dedicated focused tool, the public name becomes `read_local_image` for V1 and this naming section is revised accordingly.

## 31. Research references

Primary references for the revised design:

- MCP Tools and mixed tool-result content, specification `2026-07-28`
- Official MCP Go SDK `v1.7.0+` compatibility and raw `CallToolParamsRaw.Arguments`
- OpenAI plugin guidance for focused operations, annotations, tool selection, Refresh, and new-conversation testing
- Go 1.26 `encoding/json/v2` / `encoding/json/jsontext` experiment status, `GOEXPERIMENT=jsonv2`, strict defaults, and `RejectUnknownMembers`
- Go 1.27 draft release notes for the planned default availability of JSON v2; treated as future evidence until official GA
- Go standard `image.DecodeConfig` security guidance
- Go `os.Root` traversal/symlink semantics
- Go `os.File.SetDeadline` limitations for ordinary files
- `golang.org/x/image/webp.DecodeConfig`
- Go vulnerability records GO-2026-4961 and GO-2026-5061
- POSIX/Linux `open` semantics for `O_NOFOLLOW` and `O_NONBLOCK`

## 32. Review disposition

This revision incorporates the independent review's original six required changes, the follow-up findings, the final pre-tracer review, and the approved-Phase-A IPC identity-correlation correction.

Original review:

1. exact JSON/path/openat/cooperative-timeout contract;
2. IPC media outer response ceiling before decode;
3. bridge/daemon effective capability intersection;
4. expanded ChatGPT model/tool-selection/annotation/privacy gate;
5. one exact failure mapping using shared cross-cutting codes;
6. `x/image v0.45.0` as the candidate dependency snapshot with explicit verification/security gates.

Follow-up review:

7. explicit private IPC consumer negotiation so `old bridge + new daemon` remains fail-closed, with bridge-first/daemon-second rollout;
8. media-slot admission before every filesystem operation and non-retryable V1 `media_read_timeout`;
9. a normative quantitative ChatGPT compatibility scorecard, including zero-egress negative gates and an exact dedicated-tool switch condition.

Final pre-tracer review:

10. a normative confirmation/disclosure PASS policy, including pre-result action + address visibility, correct read/egress wording, remembered-approval behavior, and a prohibition on claiming mandatory per-read consent from host UI alone;
11. wording/test alignment that makes legacy daemon `inspect.server` always omit media, separates Phase A pre-plan tracer from Phase B post-plan rollout/revalidation, and proves admission-before-filesystem ordering with blocking resolver/base-opener tests;
12. complete `display_address` identity (`workspace_id + path` or caller-supplied `cwd + path`) across UI/result/scorecard, including a same-relative-path/different-base hard-fail tracer challenge;
13. deterministic indirect-prompt authorization using a frozen labelled golden set with exact prompt, expected action, complete address, confirmation expectation, call count, and byte-count expectation, plus corrected incompatible-pair acceptance wording;
14. IPC success/result identity correlation: complete `display_address` replaces `logical_path`, the bridge derives `expected_display_address` from the strict-decoded request, exact mismatch becomes `invalid_daemon_response`, and no `ImageContent` is emitted.

Independent v6 review:

15. strict JSON semantics are separated from parser implementation; official Go 1.27 GA remains the preferred stable candidate, the approved Go 1.26 fallback is an exact pinned JSON-v2 library behind the ShellBeam-owned strict boundary with global `GOEXPERIMENT` empty, the whole-build Go 1.26 experiment is rejected on the current dependency graph, and implementation planning is blocked until one reproducible parser/toolchain mode passes the full dev/CI/test/packaging/checkpoint/release gate.

The former 8 MiB raw image ceiling is **rejected-candidate evidence** because the live Secure MCP Tunnel path returned HTTP 413 at the observed 10,485,760-byte body ceiling. The current 7 MiB ceiling passed the real-host maximum-payload trial 3/3 but remains a product candidate until the remaining Phase A, memory/native, and Phase B gates pass. The original 5 s wording remains a cooperative acquisition budget, not a hard syscall interruption guarantee.

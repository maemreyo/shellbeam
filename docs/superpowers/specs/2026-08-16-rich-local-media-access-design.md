# ShellBeam Rich Local Media Access Design

**Status:** Review-ready design; implementation is blocked on the real ChatGPT `ImageContent` compatibility gate in Section 20
**Date:** 2026-08-16
**Scope:** Bounded local image acquisition and native MCP rich-content delivery through the existing `local_shell` tool
**Depends on:** [ShellBeam V1 Design](./2026-08-13-shellbeam-v1-design.md), [Agent Execution Layer Design](./2026-08-13-agent-execution-layer-design.md), and [Workspace, Worktree, and Git Identity](./2026-08-13-workspace-worktree-git-identity-design.md)
**Implementation plan:** Not yet written; this design must be reviewed first

## 1. Decision

ShellBeam will add an explicit modern `local_shell` action named `read_media` that can return one bounded local image as native MCP `ImageContent` plus small structured metadata.

The initial production contract is deliberately narrower than the feature name:

- media kind: `image` only;
- accepted formats: PNG, JPEG, and WebP only;
- one exact local file per call;
- no generic binary transfer;
- no PDF, video, audio, HEIC/HEIF, animated GIF, SVG, archive, document, or directory payloads;
- no image transformation, resize, transcoding, OCR, semantic interpretation, or thumbnail service;
- no new MCP tool, resource server, prompt, daemon, listener, cloud service, or persistent artifact store.

The user-facing purpose is simple:

> Let ChatGPT use a local image without the user manually uploading it.

This does **not** mean the image remains local. Returning `ImageContent` sends the selected image bytes through the configured MCP transport to the remote client/model. ShellBeam must describe this as removal of manual upload friction, not as local-only image analysis.

The selected architecture is:

```text
ChatGPT / MCP client
        |
        | local_shell { action: read_media, ... }
        v
MCP bridge
  translation only
        |
        | owner-only IPC
        v
ShellBeam daemon
        |
        v
media application service
        |
        +--> workspace/address resolution
        |
        +--> localfs media reader
                |
                +--> pinned exact regular file
                +--> bounded read
                +--> DecodeConfig validation
                +--> PNG/JPEG/WebP allowlist
        |
        v
bounded raw bytes + metadata
        |
        | IPC JSON (base64 is transport encoding only)
        v
MCP bridge
        |
        +--> TextContent summary
        +--> mcp.ImageContent{Data: rawBytes, MIMEType: ...}
        +--> StructuredContent metadata only
        v
ChatGPT / MCP client
```

## 2. Why this belongs in ShellBeam

ShellBeam already gives ChatGPT local execution, but text output cannot faithfully carry visual artifacts. A command can create `screenshot.png`, yet the model may only learn that a path exists. Users then have to locate the file and upload it manually.

The V1 design explicitly reserves bounded artifact access when screenshots, PDFs, or binary outputs demonstrate that text workflows are insufficient. Local screenshots are now a concrete use case for that reservation.

This feature is infrastructure at the Web-to-local boundary, not a new reasoning subsystem. ShellBeam transports an explicitly selected local image in a format MCP already defines; the model remains responsible for understanding the image.

Typical workflows include:

```text
frontend change
  -> start local server
  -> Playwright / agent-browser writes screenshot.png
  -> read_media
  -> model visually checks result
```

```text
generated chart / diagram
  -> local renderer writes PNG
  -> read_media
  -> model inspects the rendered artifact
```

```text
slide/PDF visual QA
  -> external renderer produces page PNGs
  -> read_media one page at a time
  -> model checks layout
```

ShellBeam does not implement the renderer or browser automation in any of these flows.

## 3. External protocol and platform findings

The design relies on existing standards instead of inventing a media protocol.

1. MCP tool results support mixed unstructured content including `text`, `image`, `audio`, resource links, and embedded resources. `ImageContent` carries base64-encoded wire data plus a MIME type.
2. ShellBeam already depends on the official `github.com/modelcontextprotocol/go-sdk` v1.7.0 line, which supports MCP `2026-07-28` and provides `mcp.ImageContent` directly.
3. OpenAI's MCP/plugin guidance states that tool-result `content` may contain text or other MCP content that helps the model answer the user.
4. OpenAI's image-input guidance supports PNG, JPEG, WebP, and non-animated GIF. This design chooses the smaller PNG/JPEG/WebP subset for V1.
5. OpenAI Secure MCP Tunnel supports local stdio MCP targets; therefore ShellBeam does not need a public HTTP file server or temporary upload URL.
6. ChatGPT custom-app tool definitions are snapshotted and later tool-definition changes are not automatically enabled. Adding `read_media` therefore requires the normal app action refresh/rescan during E2E acceptance.
7. The Go standard `image` package recommends calling `DecodeConfig` before decoding arbitrary images so dimensions can be bounded before large allocations. `golang.org/x/image/webp` supplies `DecodeConfig` and registers WebP with the standard image decoder registry.

MCP protocol support does not by itself prove that the current ChatGPT custom-app host routes returned MCP `ImageContent` into model vision context exactly as required. Section 20 makes that a mandatory empirical gate.

## 4. Goals

`read_media` SHALL:

- remove manual user upload for a selected local PNG/JPEG/WebP image;
- preserve the single `local_shell` MCP surface;
- use native MCP `ImageContent`, not base64 embedded in text or structured JSON;
- support both registered-workspace files and intentional absolute-cwd workflows such as `/tmp`;
- acquire bytes in the daemon, not the stateless MCP bridge;
- return bytes read from one pinned regular-file descriptor;
- enforce hard byte, dimension, pixel, path, concurrency, and response-work bounds;
- reject symlink traversal for the selected media path in V1;
- keep raw media bytes out of ShellBeam durable state, logs, receipts, telemetry, repro data, and default diagnostics;
- expose detected media metadata without trusting filename extension;
- preserve ordinary `start`, `poll`, `write`, `kill`, and inspection hot paths with zero media work when `read_media` is unused;
- fail closed when type, bounds, source stability, path safety, or host compatibility cannot be established.

## 5. Non-goals

V1 does not:

- keep image bytes local while claiming a remote model can see them;
- provide generic file read, download, upload, artifact transfer, or filesystem browsing;
- recursively discover images or scan directories;
- accept path globs or selectors;
- follow media-path symlinks;
- support animated or static GIF;
- support SVG, BMP, TIFF, AVIF, JPEG XL, HEIC/HEIF, RAW camera formats, PDF, Office documents, audio, or video;
- perform resize, crop, rotate, EXIF normalization, watermark removal, format conversion, or compression;
- call ImageMagick, libvips, FFmpeg, MuPDF, browser engines, OCR engines, or Python image libraries;
- store thumbnails, copies, content-addressed blobs, deterministic media hashes, or a media cache;
- add MCP `resources/list`, `resources/read`, `ResourceLink`, or a local HTTP asset endpoint;
- interpret image semantics, compare screenshots, decide whether a UI is correct, or generate fixes;
- infer which file should be shared from command output;
- automatically attach produced images to every command;
- weaken the current-user authority model or add a path allowlist/sandbox.

If a rejected format matters in practice, a caller may use an already-installed external tool through normal ShellBeam execution to create a bounded PNG/JPEG/WebP derivative, then call `read_media` on that derivative. ShellBeam does not install or manage that external tool.

## 6. Alternatives considered

### 6.1 Selected: daemon-owned acquisition + native MCP image result

The daemon resolves and safely opens the local file, validates it, reads bounded bytes, and returns those bytes through existing IPC. The MCP adapter converts the raw bytes to `mcp.ImageContent`.

Benefits:

- preserves the bridge as a translator rather than a second local authority;
- centralizes path, workspace, quota, and privacy policy;
- avoids a second MCP tool/resource surface;
- uses the official SDK for media wire representation;
- provides one exact payload cut to the bridge after local validation.

Cost:

- JSON IPC encodes `[]byte` as base64 and the MCP SDK later encodes the same raw bytes for the remote MCP result, creating extra copies and approximately 4/3 wire expansion at each JSON boundary.

For an 8 MiB single-file cap and personal-scale concurrency, correctness and architectural clarity win over a zero-copy protocol. FD passing or a binary side-channel is deferred unless measured usage proves the copies material.

### 6.2 Rejected: let the MCP bridge open local files directly

This avoids one IPC copy but gives the bridge new filesystem acquisition authority, duplicates address/path safety logic, and introduces a separate TOCTOU boundary after daemon validation. It also weakens the existing statement that the bridge has no local execution authority beyond talking to the daemon.

The bridge remains translation-only.

### 6.3 Rejected: MCP resource links or `file://` URIs

A remote ChatGPT host cannot be assumed to dereference a local `file://` URI. Resource links also push ShellBeam toward a resource server and additional discovery/read semantics, contrary to the one-tool design.

Direct `ImageContent` is the correct V1 representation.

### 6.4 Rejected: temporary HTTP file server / signed URL

A local/public file server introduces listeners, authentication, lifecycle, cleanup, URL capability leakage, and another remote-transport boundary. Secure MCP Tunnel already carries the tool result, so this is unnecessary.

### 6.5 Rejected: generic MIME/media framework

V1 has only three accepted image formats. A generic MIME database, transform graph, codec registry, or media abstraction would expand dependencies and attack surface without improving the first use case.

The public action may be named `read_media`, but implementation remains a closed image-only contract until a later design explicitly promotes another kind.

## 7. Public `local_shell` request

`read_media` is modern protocol v2 only. Legacy protocol/tool views omit the action and capability.

Exactly one address form is accepted.

### 7.1 Workspace-relative form

```json
{
  "action": "read_media",
  "workspace_id": "ws_01K2...",
  "path": "artifacts/settings.png"
}
```

`path` is an exact repository/workspace-relative logical path.

### 7.2 Absolute-cwd form

```json
{
  "action": "read_media",
  "cwd": "/tmp",
  "path": "settings.png"
}
```

`cwd` is an explicit absolute base directory and `path` is still relative. This preserves the existing ShellBeam distinction between workspace-relative addressing and intentional absolute-location escape hatches.

A caller that wants `/tmp/settings.png` uses `cwd=/tmp`, `path=settings.png`; it does not pass an absolute `path`.

### 7.3 Path contract

`path`:

- is valid UTF-8;
- is 1-1024 bytes;
- is relative;
- is interpreted literally, never as a glob/regex/shell token;
- contains no NUL;
- contains no `.` or `..` path segment after lexical normalization;
- cannot be empty after normalization;
- addresses exactly one filesystem entry.

The schema rejects requests containing both `workspace_id` and absolute `cwd`, or neither.

No caller-selectable option may raise a server media limit. V1 has no `max_bytes`, `resize`, `quality`, `detail`, or `format` option.

## 8. Response contract

A successful modern result contains:

```text
Content[0] = TextContent
Content[1] = ImageContent
StructuredContent = small metadata object
```

Example conceptual response:

```text
TextContent:
  "read_media: image/png 1440x900, 482304 bytes"

ImageContent:
  MIMEType: image/png
  Data: <raw image bytes supplied to the Go SDK>

StructuredContent:
{
  "schema_version": 2,
  "ok": true,
  "action": "read_media",
  "media": {
    "schema_version": 1,
    "kind": "image",
    "mime_type": "image/png",
    "byte_size": 482304,
    "width": 1440,
    "height": 900,
    "path": "artifacts/settings.png",
    "address_kind": "workspace",
    "workspace_id": "ws_01K2..."
  }
}
```

For absolute-cwd addressing, public structured metadata reports `address_kind=absolute_cwd` and the caller-supplied relative `path`, but does not need to echo the canonical absolute host path.

Raw bytes, base64 data, decoder internals, inode/device IDs, canonical absolute paths, private file descriptors, and local storage identities never appear in `StructuredContent` or `TextContent`.

`mcp.ImageContent.Data` receives raw Go `[]byte`; the official SDK owns JSON/base64 wire serialization. ShellBeam does not construct `data:image/...` strings or its own MCP content envelope.

## 9. Capability discovery

Modern `inspect.server` adds a capability only when the full daemon/bridge composition supports it:

```text
features:
  rich_local_media: available

media_schema_versions:
  [1]

media_kinds:
  [image]

media_image_mime_types:
  [image/png, image/jpeg, image/webp]

limits:
  media_image_bytes: 8388608
  media_image_dimension: 16384
  media_image_pixels: 40000000
  media_path_bytes: 1024
  media_concurrent_reads: 2
```

Legacy capability projection strips all media-only fields.

Capability absence returns `feature_unavailable`; callers do not discover the feature by trying arbitrary files.

The design intentionally separates `media_image_bytes` from the existing `response_bytes` text/output-view limit. A media result has a small structured/text response plus one independently bounded native rich-content payload. Existing text response limits do not silently expand to multiple megabytes.

## 10. Hard initial limits

The first implementation targets these hard limits:

| Limit | Initial value | Reason |
| --- | ---: | --- |
| raw image bytes | 8 MiB | enough for common screenshots while bounding IPC/MCP expansion |
| maximum width | 16,384 px | blocks pathological headers |
| maximum height | 16,384 px | blocks pathological headers |
| maximum pixels | 40,000,000 | bounds model/decoder work independently of dimensions |
| path bytes | 1,024 | consistent with existing ShellBeam bounded logical paths |
| concurrent media reads | 2 | bounds memory and IPC pressure |
| media read deadline | 5 s | local bounded operation; no hanging special-file semantics |
| files per call | 1 | avoids multiplicative payloads and ambiguous partial success |

An 8 MiB raw image expands to roughly 10.7 MiB of base64 before small JSON overhead. The IPC implementation SHALL use an internal derived response ceiling high enough for exactly one maximum media payload and metadata, without changing the ordinary response/output defaults.

The production implementation MUST prove the 8 MiB path through the real Secure MCP Tunnel and ChatGPT host before release. If the host/tunnel cannot reliably carry the designed ceiling, this design is revised with a lower explicit constant; implementation must not silently ship a different effective limit.

## 11. Local file acquisition authority

The MCP bridge never opens the requested media path.

The daemon owns local acquisition and delegates filesystem mechanics through a narrow localfs media reader. The application layer owns request validation, workspace resolution, limits, error classification, and response shaping.

Suggested dependency direction:

```text
internal/core/media
    types, enums, limits, validation

internal/app/media
    service, workspace/address port, reader port

internal/adapter/media/localfs
    pinned local-file open/read + image header validation

internal/app/bridge
    action routing only

internal/adapter/ipc
    bounded request/response transport

internal/adapter/mcp
    ImageContent translation only
```

The media package does not depend on MCP types. Only the MCP adapter imports `mcp.ImageContent`.

## 12. Safe path resolution

The operation is local-current-user authority, not a sandbox. The safety objective is narrower: the exact path selected by the caller must not unexpectedly resolve through a symlink to a different file while ShellBeam is acquiring content.

### 12.1 Workspace mode

- resolve the current registered workspace root using existing workspace identity machinery;
- canonicalize/pin the base root once for this request;
- reject a missing/moved/unavailable workspace using existing typed workspace semantics;
- traverse the requested relative path beneath that pinned root;
- reject intermediate or final symlinks in V1;
- reject escape, special file types, or path substitution.

### 12.2 Absolute-cwd mode

- `cwd` must be absolute and resolve to an existing directory;
- canonicalize the base cwd before media-path traversal, so common host aliases such as macOS `/tmp` may resolve to their canonical directory;
- after the base is fixed, traverse `path` with the same no-follow semantics as workspace mode.

### 12.3 Native mechanics

On macOS/Linux the localfs provider SHOULD reuse the already-present `golang.org/x/sys/unix` dependency and pinned-directory/`openat`-style primitives rather than path-check-then-plain-`os.Open` logic.

The final descriptor must be verified with `fstat` as a regular file. Directories, sockets, devices, FIFOs, and other special files are rejected.

This path contract is designed to prevent accidental path substitution during acquisition. It does not claim to confine arbitrary commands launched separately through `local_shell`.

## 13. Read and stability ordering

A successful read follows this ordering:

```text
validate request
  -> resolve address/base root
  -> acquire media concurrency slot
  -> no-follow open exact regular file
  -> fstat before read
  -> reject negative/oversized/non-regular source
  -> read at most media_image_bytes + 1
  -> reject growth/overflow
  -> fstat after read
  -> reject observed metadata change during read
  -> DecodeConfig from the captured bytes
  -> closed format allowlist check
  -> dimension/pixel checks
  -> construct immutable in-memory media result
  -> return through IPC
  -> MCP adapter emits ImageContent
```

The returned bytes are the bytes captured from one pinned descriptor. Comparing pre/post metadata catches ordinary concurrent modification but is not advertised as an atomic filesystem snapshot or universal writer exclusion. If ShellBeam observes a change during acquisition, it returns `media_source_changed` and does not emit `ImageContent`.

No durable state is required for read idempotency. Repeating `read_media` is a new read of current local content, just like an explicit observation. The action never claims that two calls see the same source generation.

## 14. Format detection and image validation

Filename extension is not authority.

V1 implementation uses:

- Go standard `image.DecodeConfig`;
- standard `image/jpeg` registration;
- standard `image/png` registration;
- `golang.org/x/image/webp` registration;
- a closed mapping from decoder format name to the three public MIME types.

After `DecodeConfig`, only these exact format/MIME pairs are accepted:

```text
png  -> image/png
jpeg -> image/jpeg
webp -> image/webp
```

Any other registered format is rejected even if another dependency happens to register a decoder globally.

`DecodeConfig` is run against the already byte-bounded in-memory payload. ShellBeam does not need to fully decode pixels merely to transport the file. Width, height, and `width * height` are checked with overflow-safe arithmetic before success.

### Why no generic MIME package

A package such as `github.com/gabriel-vasile/mimetype` is useful for broad file-type detection, but V1 does not need broad detection. The standard image decoder registry already validates the formats ShellBeam accepts, and the standard library can map the detected decoder format to the required MIME value with a three-entry closed table.

Adding a generic MIME dependency would broaden dependency surface without reducing meaningful implementation complexity, so it is rejected for V1.

## 15. Dependency decision

### 15.1 Reused without change

`github.com/modelcontextprotocol/go-sdk v1.7.0` remains the MCP implementation. ShellBeam uses its native `mcp.ImageContent`; no alternative MCP SDK or custom media JSON type is introduced.

`golang.org/x/sys v0.47.0` is already a direct dependency and remains the Unix primitive provider for safe path acquisition where needed.

### 15.2 New dependency

The only planned new runtime module is:

```text
golang.org/x/image
```

V1 imports only the WebP decoder path needed for `golang.org/x/image/webp`; ordinary PNG/JPEG support remains in the Go standard library.

Research snapshot on 2026-08-16:

- current reviewed module line: v0.44.0;
- published as a Go supplementary image module;
- BSD-3-Clause license;
- maintained in the Go project infrastructure;
- WebP provides `DecodeConfig` and registers itself with `image.RegisterFormat`;
- recent WebP/VP8L changes include memory/allocation hardening, making it preferable to pin a current reviewed release rather than an old transitive copy.

Before implementation commit, the plan must pin an exact compatible version, run module verification and vulnerability checks, and measure binary-size delta. A newer compatible release may be selected only after the same review; the design does not authorize floating dependency versions.

### 15.3 Explicitly not added

V1 does not add:

- `github.com/gabriel-vasile/mimetype`;
- `github.com/anthonynsimon/bild`;
- libvips/bimg;
- ImageMagick bindings;
- FFmpeg Go wrappers;
- HEIC decoders;
- MuPDF/go-fitz/pdf renderers;
- Pillow/Python runtime;
- OCR libraries.

Future format/transform support requires evidence of user demand and its own dependency/security review.

## 16. IPC contract

Media acquisition remains a daemon action and therefore crosses the existing owner-only local IPC boundary.

IPC v2 adds a closed `read_media` request and success response. The internal success response may contain:

```text
media:
  schema_version
  kind
  mime_type
  byte_size
  width
  height
  logical path/address metadata
  data []byte
```

The IPC JSON serializer owns the temporary base64 representation of `data`. The bridge immediately translates decoded raw bytes to MCP `ImageContent`; it never persists or logs the blob.

IPC response decoding enforces the same maximum raw media size after base64 decode. A malicious/corrupt daemon response cannot cause the bridge to allocate or forward an unbounded blob merely because the outer HTTP response arrived successfully.

No legacy IPC generation gains media payloads.

## 17. MCP translation contract

The MCP adapter special-cases successful `read_media` rendering rather than forcing rich content through the ordinary text-only `toolSuccess` helper.

Conceptually:

```go
return &mcp.CallToolResult{
    Content: []mcp.Content{
        &mcp.TextContent{Text: summary},
        &mcp.ImageContent{Data: result.Data, MIMEType: result.MIMEType},
    },
    StructuredContent: publicMetadata,
}
```

The implementation uses the exact official SDK API at the pinned version rather than copying this conceptual snippet blindly.

Errors return text + structured error only; they never return partial `ImageContent`.

The existing `local_shell` tool remains conservatively annotated as destructive/open-world because annotations apply to the whole one-tool union, not per action. `read_media` being locally read-only does not justify weakening the tool-level safety annotation used by execution actions.

## 18. Privacy and security

`read_media` is an explicit **local-data egress** operation.

The security documentation and MCP instructions must state:

- selected image bytes are returned to the connected MCP client/model;
- ShellBeam does not upload to a separate ShellBeam service or public URL;
- the bytes nevertheless leave the local daemon boundary through the configured MCP transport;
- callers should not use `read_media` for secrets or private images they do not intend to share with the connected model/service.

ShellBeam does not pretend to identify all sensitive images. There is no heuristic secret/image classifier.

Raw media bytes must be absent from:

- daemon/operator logs;
- receipts;
- session output logs;
- workspace/activity records;
- default diagnostics;
- crash/failure summaries;
- package/release evidence;
- telemetry;
- reproduction artifacts unless a future design explicitly opts in;
- deterministic content hashes or public content identities created solely for this feature.

Privacy tests use sentinel byte patterns and assert they appear only in the intended IPC/MCP result path.

## 19. Failure taxonomy

V1 reserves stable failures such as:

```text
media_feature_unavailable
media_address_invalid
media_workspace_not_found
media_path_not_found
media_path_unsafe
media_not_regular_file
media_too_large
media_type_unsupported
media_decode_config_failed
media_dimensions_exceeded
media_source_changed
media_busy
media_read_timeout
media_read_failed
```

Public errors expose only safe bounded details, for example requested logical path, detected high-level media type when safe, byte/dimension limits, and retryability. They do not expose raw file content, canonical host paths, arbitrary OS error strings, file descriptors, inode values, or decoder internals.

`media_busy` is retryable. Unsupported type, invalid address, unsafe path, and dimension/size policy failures are not retryable without changing the source/request. A transient read failure may be retryable only when classification is known safe.

## 20. Mandatory ChatGPT compatibility spike

Implementation is blocked until a narrow compatibility spike proves the real host path. Passing an SDK unit test is necessary but insufficient.

### 20.1 Local SDK proof

Using the official Go MCP SDK on both ends:

1. return a known PNG as `ImageContent`;
2. prove the client receives the exact raw bytes and MIME type;
3. prove text and structured metadata coexist with image content;
4. prove schema/output handling does not drop the rich content;
5. prove an oversized/corrupt result fails before forwarding.

### 20.2 Secure MCP Tunnel + ChatGPT Web proof

Using a dedicated test build/profile and no real secrets in image content:

1. refresh/rescan the custom app so ChatGPT sees the new `read_media` action;
2. return a generated PNG containing a randomly chosen visible code/string that is not present in tool text/metadata;
3. ask ChatGPT to report the visible code;
4. require the answer to match, proving model-visible image delivery rather than filename/text inference;
5. repeat for JPEG and WebP;
6. prove unsupported GIF is rejected locally;
7. repeat with payload sizes approximately 64 KiB, 256 KiB, 1 MiB, 4 MiB, and 8 MiB;
8. record exact ShellBeam commit, Go SDK version, tunnel-client version, ChatGPT app refresh state, protocol version, OS/architecture, payload byte size, MIME type, and PASS/FAIL;
9. never claim `ImageContent` compatibility from MCP spec conformance alone.

If ChatGPT does not expose returned `ImageContent` to the model, production implementation stops. ShellBeam must not fall back to giant base64 text, data URLs, public HTTP upload, or undocumented ChatGPT endpoints merely to force the feature through.

If PNG works but a designed format does not, revise the format allowlist before production. If 8 MiB fails, revise the hard byte limit before implementation continues.

## 21. Performance and admission

When `read_media` is unused:

- no decoder/provider initialization performs filesystem work;
- no media semaphore is acquired;
- no file is opened;
- no media bytes are allocated;
- ordinary command startup latency is unchanged except negligible static capability/catalog bytes.

`read_media` itself is allowed to allocate one bounded raw image payload plus transport encoding buffers. Maximum concurrent reads are two, preventing unbounded memory multiplication.

No background goroutine scans media. No retention worker exists.

Implementation acceptance records:

- max observed daemon RSS delta for one 8 MiB read;
- two concurrent maximum-size reads;
- IPC serialization/deserialization time;
- MCP result construction time;
- binary size before/after `x/image/webp`;
- zero measurable ordinary `start` hot-path work attributable to media.

These are observations, not reasons to add zero-copy infrastructure before evidence exists.

## 22. Schema and compatibility rules

`read_media` is added only to the modern v2 input/output union and IPC v2 schemas.

Required contract tests cover:

- exact workspace form;
- exact absolute-cwd form;
- neither/both address forms rejected;
- absolute `path` rejected;
- unknown properties rejected;
- extra media options rejected;
- success metadata closed and byte-free;
- all stable failure branches;
- legacy tool schema/action list does not contain `read_media`;
- modern tool schema does;
- app refresh requirement documented for ChatGPT acceptance.

Because ChatGPT freezes approved app tool definitions, deployment documentation must explicitly tell testers/admins to refresh/rescan actions after this schema change. The feature is not considered broken merely because an old frozen app snapshot does not know the new action.

## 23. Testing strategy

### 23.1 Core tests

- request/path grammar and one-of addressing;
- MIME/format enum validation;
- byte/dimension/pixel arithmetic including overflow;
- capability cloning and legacy stripping;
- stable failure codes and safe details.

### 23.2 localfs adapter tests

Use generated fixtures, not downloaded opaque corpora, for ordinary cases:

- valid PNG/JPEG/WebP;
- extension/type mismatch accepted according to detected content, not extension;
- GIF/BMP/SVG/text/PDF rejected;
- zero/truncated/corrupt files rejected;
- >8 MiB file rejected before unbounded read;
- width/height/pixel limits;
- intermediate symlink rejected;
- final symlink rejected;
- FIFO/socket/directory/device rejected where constructible;
- workspace escape rejected;
- `/tmp`-style canonical cwd accepted on macOS;
- concurrent source change hook produces `media_source_changed`;
- timeout/cancellation closes descriptors and releases semaphore;
- no raw sentinel bytes in logs/state.

### 23.3 App/IPC tests

- workspace lookup/address errors map deterministically;
- two read slots accepted, excess returns bounded `media_busy` behavior rather than unbounded queueing;
- IPC base64 round-trip is byte exact;
- oversized decoded IPC payload rejected by bridge;
- daemon cannot smuggle an unsupported MIME/type pair;
- no persistence write occurs for a read.

### 23.4 MCP tests

- result has exactly text + image content for success;
- image bytes and MIME survive SDK round-trip;
- structured metadata contains no bytes/base64;
- failure contains no image content;
- old protocol/tool list excludes action;
- modern protocol includes action;
- one MCP tool remains exposed.

### 23.5 Native/E2E tests

- macOS native safe-open/path tests;
- Linux native safe-open/path tests before cross-platform production claim;
- cross-build is compile evidence only;
- real Secure MCP Tunnel + ChatGPT visual code challenge from Section 20.

## 24. Release gates

The feature may be called production-ready only when all are true:

1. real ChatGPT Web proves model-visible PNG/JPEG/WebP `ImageContent` through Secure MCP Tunnel;
2. the published raw-byte ceiling succeeds end to end;
3. only one MCP tool is advertised;
4. bridge never opens local media files;
5. daemon safe-open tests reject symlink/path substitution cases;
6. byte, dimension, pixel, path, concurrency, and time limits are enforced server-side;
7. unsupported formats never fall back to generic binary/text transfer;
8. raw bytes never enter durable ShellBeam state or ordinary logs;
9. modern/legacy schema behavior is exact;
10. app refresh/rescan procedure is documented and exercised;
11. `go mod verify`, focused/race tests, architecture/schema gates, and full repository tests pass on the exact source fingerprint, with any pre-existing host-load flake explicitly separated from feature regressions;
12. `x/image` dependency version, license, vulnerability status, and binary-size delta are recorded;
13. ordinary execution path shows no media filesystem/probe work when unused.

## 25. Future extensions deliberately deferred

### 25.1 Static GIF

OpenAI image inputs support non-animated GIF, but ShellBeam would need to prove static-vs-animated classification under bounded frame/memory work. V1 avoids writing a custom GIF parser or fully decoding arbitrary multi-frame GIFs merely to support another extension.

### 25.2 HEIC/HEIF

A later provider may decode/transcode iPhone images to PNG/JPEG if real usage justifies the additional codec/runtime/security surface. No HEIC dependency is approved by this design.

### 25.3 Video

A future explicit preview action could use an installed `ffprobe`/`ffmpeg` provider to extract bounded metadata and representative frames, then reuse image delivery. ShellBeam should call executables through argv rather than add a large codec binding. This requires a separate design.

### 25.4 PDF/document visual preview

A future renderer provider may produce bounded page images and feed them through the same image result contract. ShellBeam should not embed a PDF/document engine in core merely for transport.

### 25.5 Image resizing

If E2E usage repeatedly hits the byte ceiling, a later design may add an explicit preview/transcode provider. V1 returns `media_too_large` and lets the agent use existing local tools to create a smaller derivative.

### 25.6 Multi-image calls

One-file-per-call keeps failure and payload semantics simple. Batching is deferred until real trajectories show that repeated calls create material model/tool friction.

### 25.7 Operation-linked artifact handles

When/if ShellBeam has a durable artifact identity that can safely name produced files, `read_media` may accept that explicit handle as another address form. This design does not invent a parallel artifact registry just for images.

## 26. Why the name is `read_media` while V1 is image-only

The action name describes the long-term user intent without promising generic file transfer. Capability discovery tells the model exactly which media kinds and MIME types are currently supported.

This avoids later proliferation such as:

```text
read_image
read_video_frame
read_pdf_page
read_audio
```

while still requiring every new kind to pass a separate design and capability revision. `read_media` is not an open MIME passthrough.

## 27. Success criterion

The capability succeeds when this becomes a normal ChatGPT workflow:

```text
User:
  "Check the screenshot the local test just generated."

ChatGPT:
  -> locate the explicit known path from its workflow
  -> local_shell read_media
  -> receive native image content
  -> reason visually
  -> continue work
```

The user does not manually download, drag, attach, or re-upload the file. ShellBeam does not become a browser, file manager, media server, or image-processing framework.

## 28. Research references

Primary references consulted for this design:

- Model Context Protocol, tool results and `ImageContent`, specification `2026-07-28`: https://modelcontextprotocol.io/specification/2026-07-28/server/tools
- Official MCP Go SDK and protocol-version compatibility: https://github.com/modelcontextprotocol/go-sdk
- OpenAI, Build an MCP server — tool `content` may contain text or other MCP content: https://developers.openai.com/plugins/build/mcp-server
- OpenAI, Secure MCP Tunnel — local stdio MCP targets: https://developers.openai.com/api/docs/guides/secure-mcp-tunnels
- OpenAI, Images and vision — supported input image formats and limits: https://developers.openai.com/api/docs/guides/images-vision
- OpenAI, Developer mode and MCP apps — frozen app actions and refresh/update behavior: https://help.openai.com/en/articles/12584461-developer-mode-and-full-mcp-connectors-in-chatgpt
- Go standard library `image` security guidance: https://pkg.go.dev/image
- Go `x/image/webp` `DecodeConfig`: https://pkg.go.dev/golang.org/x/image/webp
- Go supplementary image module metadata/license/security guidance: https://pkg.go.dev/golang.org/x/image

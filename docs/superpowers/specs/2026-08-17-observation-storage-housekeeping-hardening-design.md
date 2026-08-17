# Observation Storage & Housekeeping Hardening Design

Date: 2026-08-17

## Context

A long-running ShellBeam daemon currently accumulates one small JSON file per observation obligation and event. On the inspected machine this has four operational consequences:

1. `min_free_space_bytes` is configured and validated but was not surfaced to operators.
2. ownership tests leak package-scoped build directories and persistent supervisors.
3. observation obligations have no collector, while `observationSequences` refuses directories larger than 65,536 entries.
4. `initObservationStore` calls the same strict scanner and decodes every obligation merely to recover the highest sequence, making startup O(number of obligations × JSON decode cost).
5. the daemon starts event materialization only once; later committed obligations are materialized only if a client happens to call `inspect.events`, so a quiet event consumer leaves `MaterializedThroughSeq` stale and prevents obligation retention from advancing.

A WIP change already adds free-space reporting, test cleanup, and observation retention. This design hardens that WIP without changing the on-disk observation record format.

## Goals

- Preserve crash-safe sequence authority: the highest durable obligation filename remains the source of truth for the observation high watermark.
- Make startup recovery depend on filesystem metadata and filenames, not decoding every historical JSON record.
- Allow a store with more than 65,536 historical obligations to reopen and collect itself instead of becoming permanently unopenable.
- Keep strict validation for records when they are consumed.
- Collect only obligations already absorbed by the event projection, never prepared or future obligations.
- Preserve restart correctness after collection.
- Surface low free space as an operator warning, never as an admission/startup refusal.
- Stop tests from leaking shared build directories or persistent supervisors.
- Restore the originally designed post-commit materializer wake-up so projection progress does not depend on `inspect.events` traffic.

## Non-goals

- No segmented observation format, database migration, SQLite/Bolt dependency, or compaction rewrite in this slice.
- No hard free-space admission gate.
- No automatic cleanup of already-orphaned processes or `/private/tmp` directories on the developer machine.
- No weakening of per-record permission, owner, symlink, size, schema, sequence, or semantic validation when a record is actually read.

## Approaches considered

### A. Metadata-only sequence discovery + strict read validation (chosen)

Split directory discovery from record decoding. Sequence discovery validates entry shape and safety using directory metadata, parses sequence numbers, and sorts them. `readObservation` remains the only content decoder/validator. Startup uses discovery only; listing and retention decode only records they actually consume.

Advantages: no format migration, preserves filename authority, removes startup JSON cost, recovery works beyond the historical scan cap, and the change is locally testable.

Trade-off: corruption in an old record is detected when that record is consumed rather than eagerly during `Open`.

### B. Durable high-watermark metadata file

Persist the high watermark separately and open in O(1).

Rejected for this slice because obligation creation and watermark update are two durable writes. Without a transactional primitive, every ordering creates a crash window: watermark-ahead produces continuity gaps; watermark-behind risks sequence reuse or requires collision recovery. A cache-only watermark would still require fallback scans and adds state without removing the authoritative directory problem.

### C. Segmented journal / embedded database

Store many observations per segment/page to remove APFS 4 KiB block amplification and inode growth.

Deferred. It is the correct long-term storage-efficiency direction, but it changes the persistence format, migration story, corruption domain, retention strategy, and concurrency model. It should be designed after the current correctness hazards are closed.

## Design

### 1. Observation directory discovery

Introduce a metadata-only helper that:

- `ReadDir`s the obligation directory.
- ignores ShellBeam crash-temp entries (`.shellbeam-*`).
- rejects symlinks, directories, malformed sequence filenames, non-regular files, unsafe permissions/owners, and impossible file sizes.
- returns sorted sequence numbers without opening or decoding JSON content.
- does not reject the directory merely because it contains more than 65,536 entries.

`initObservationStore` uses this helper to recover the highest durable filename. The highest filename remains authoritative, preserving the existing crash model.

### 2. Strict content validation stays on reads

`readObservation(seq)` continues to enforce:

- Lstat / no symlink
- regular file
- owner and permissions
- size bounds
- strict JSON decode
- `record.ChangeSeq == filename sequence`
- `record.Validate()`

`ListObservationObligations` discovers sequences but decodes only the requested records after `after`, stopping at `limit`. Therefore materialization/reconciliation still fail closed on corrupt records before using them.

### 3. Retention that can recover an oversized ledger

`CollectMaterializedObligations` uses metadata discovery rather than the old capped/full-decode scanner, so it can run even if the ledger has already crossed 65,536 files.

A record is collectible only when:

- `seq <= projection.MaterializedThroughSeq`
- it is not the highest durable filename (watermark anchor for restart)
- its decoded state is not `prepared`

The method is bounded per sweep and fsyncs the obligation directory after removals. Logical state-byte accounting is decremented by deleted logical sizes.

### 4. Eventual materialization wake-up

The durable observation obligation remains authority, but a terminal obligation transition (`committed` or `aborted`) emits a best-effort in-process wake-up on a buffered size-one channel owned by the repository. The notification carries no sequence and is not durable; it is only a latency hint. Multiple transitions coalesce safely because the materializer always reads the durable high watermark.

The execution-observation runtime:

- runs one materialization pass immediately at startup;
- sleeps without polling while healthy;
- reruns after a repository wake-up;
- retries after a bounded delay only when the previous materialization pass returned an error; and
- remains serialized with synchronous `inspect.events` materialization by the materializer's existing mutex.

This restores the implementation intent already documented for `StoreResult.ObservationSeq`/post-commit wake-up while centralizing the signal at the observation terminal transition, so every event producer benefits without plumbing callbacks through all daemon mutation paths.

The signal is deliberately not used as authority: restart always performs an immediate pass from durable state, so a lost process-local notification cannot create a silent gap.

### 5. Background housekeeping

After daemon readiness:

- existing terminal retention runs unchanged
- observation obligation collection runs immediately, then periodically
- event `CompactEvents` is wired into the same observation sweep
- low free-space watch samples the state filesystem and emits edge-triggered low/recovered events

Housekeeping failures do not prevent service or create a tight retry loop.

### 6. Free-space warning semantics

Use `statfs` `Bavail * Bsize`, because it represents blocks available to the daemon user rather than blocks reserved for privileged use.

- `doctor`: `disk_space` is `PASS` or `WARN`; warning does not make doctor exit non-zero.
- daemon: emit `free_space_low` only on crossing below the configured threshold and `free_space_recovered` on crossing back above it.
- no admission refusal is added.

### 7. Test leak cleanup

- package-scoped ownership test binary: clean from `TestMain`, matching the `sync.Once` lifetime.
- persistent session helper: register cleanup kill for every successfully started persistent session, making future tests safe by default.

Existing orphaned processes/directories are left untouched by the code change.

## Testing

TDD cases must cover:

1. reopening a store with a corrupt obligation still fails when that record must be consumed, but startup sequence recovery no longer decodes unrelated historical contents.
2. a ledger larger than the historical 65,536 cap can reopen and can be collected.
3. startup recovers the exact highest sequence and the next prepare uses `high+1`.
4. retention preserves prepared records, stops at projection, respects bounds, and preserves high watermark across reopen.
5. daemon wiring actually shrinks a materialized ledger and the resulting store restarts.
6. low-space doctor/runtime warnings have warning-only and edge-triggered semantics.
7. a materializer that has already completed its initial pass is woken by a later committed observation and advances the projection without an `inspect.events` call.
8. persistent tests and ownership binary leave no new supervisor/build-dir leaks.
9. full repository test/build/policy gates pass, or any pre-existing failure is identified with before/after evidence.

## Follow-up: physical storage amplification

After this slice, separately design a segmented observation store. The current wake-up/retention fix bounds steady-state file count but does not change the allocation-unit cost of each surviving file. Target properties:

- append-many records per durable segment instead of one APFS allocation unit per ~300–500 byte JSON record
- bounded segment size and record count
- atomic segment rollover
- sequence-indexed reads without scanning all prior segments
- segment-level retention only after projection safety watermark
- migration/compatibility for existing file-per-record state

This follow-up addresses the measured physical amplification and inode pressure; the current slice primarily bounds the number of surviving small files and removes the correctness/startup hazards.

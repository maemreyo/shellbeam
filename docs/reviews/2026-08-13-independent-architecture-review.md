# Independent Architecture Review

## Executive Decision

Chấp thuận kiến trúc ShellBeam hiện tại để chuyển sang implementation planning: một modular monolith Go gồm bridge MCP stateless, daemon per-user sở hữu process/session, IPC qua Unix socket được xác thực cùng UID, và persistence dạng file có reservation/receipt. Đây là phương án nhỏ nhất vẫn bảo toàn được idempotency, process ownership khi tunnel/bridge ngắt, bounded resources và bằng chứng kết quả. Không còn finding kiến trúc material trong phạm vi design document sau vòng chỉnh sửa; mức tin cậy là cao đối với contract tĩnh nhưng chỉ trung bình đối với hành vi runtime vì chưa có implementation, fault test hay tunnel E2E.

## Review Charter

### Confirmed

Sản phẩm dành cho một OS user trên macOS/Linux, ChatGPT là reasoning agent duy nhất, V1 chỉ quảng bá một MCP tool, Secure MCP Tunnel là remote transport, và daemon là process/session owner. V1 không hứa daemon-crash recovery, sandbox/container, SSH, Windows, public marketplace hay artifact transfer. Critical journeys đã xác nhận gồm start/retry, poll/cursor, bounded stdin/EOF, kill/retry, timeout, output/storage failure, bridge/tunnel disconnect, daemon crash, retention và terminal evidence.

### Assumptions

Tải V1 là personal-developer scale; file-backed compact tombstones đủ vận hành khi có state budget và explicit purge. launchd/systemd-user có thể cung cấp environment tối thiểu; command chạy với authority của user là product contract chứ không phải isolation boundary. Các ngưỡng quota mặc định sẽ được benchmark và chốt trong implementation plan mà không đổi kiến trúc.

### Unknowns

Chưa có số liệu command concurrency, output distribution, tombstone growth, filesystem fault rate, startup/latency budget hoặc production SLO. Chưa xác minh SDK v1.7.0 với Secure MCP Tunnel và ChatGPT Developer mode trên cả hai OS. Các unknown này có thể đổi default, batching, retention hoặc thúc đẩy supervisor V1.1; chưa có bằng chứng khiến chúng đảo quyết định modular-monolith/daemon của V1.

### Legacy-Derived Claims

Không có codebase legacy. Các claim về state, retry, persistence, process group và test policy đều đến từ design document đã được chỉnh sửa, chưa phải runtime evidence. Review là Context-exposed vì reviewer đã thấy bản thiết kế và các kết luận trước; không tuyên bố blind independence.

### Decision Drivers

Gates theo thứ tự: không duplicate side effect khi retry; không spawn nếu chưa có durable reservation; daemon là owner duy nhất; terminal success phải có durable evidence; cùng-UID local boundary; bounded process/input/output/state; one-tool/one-brain scope; macOS/Linux operability; edit loop nhanh nhưng release proof đầy đủ. Non-goals không được dùng làm lý do kéo container, DB, gRPC, microservice, SSH hay supervisor vào V1.

## Coverage Manifest

### Inspected

Đã đọc toàn bộ `/workspace/scratch/11466ca0e994/upload/Pasted markdown(20260813-041333).md`. Đã trace entry và trust boundaries; input/output/error contract; start reservation/fingerprint/capacity/storage ordering; poll/cursor; write queue/offset/EOF/delivery; kill ID/signal evidence; public state machine; output quota/control reserve; receipts/retention/tombstones; daemon failure; process-group limitation; configuration; security; stable failures; acceptance; roadmap; tech stack; import barriers; size limits; incremental build; affected testing; CI/release và agent workflow.

### Partially Inspected

Closed output schema mới được định nghĩa ở mức canonical fields, chưa có JSON Schema artifact thực tế. Resource default, IPC endpoint shape, config schema và exact receipt variants được chủ ý để implementation plan đóng băng. Dependency/API compatibility chỉ được kiểm tra bằng nguồn release, chưa compile.

### Not Inspected

Không có implementation, `go.mod`, tests, CI, service templates, persisted sample hay operational data để audit. Không thực hiện cài service, chạy process/PTY, dùng MCP Inspector hay kết nối tunnel thật vì chúng chưa tồn tại và ngoài phạm vi sửa document.

### Runtime-Unverified

Peer credential portability, atomic replace/fsync semantics trên filesystem thực, ENOSPC/control-reserve recovery, PTY EOF behavior, signal/process-group behavior, Go SDK negotiation, launchd/systemd lifecycle, dirty-selector correctness và cache savings đều cần runtime evidence trước release.

## Independent Reference Model

### Invariants

Một intended start gắn vĩnh viễn với một operation ID và tối đa một spawn; response tuning không đổi intent. Không process nào được spawn trước durable reservation. Accepted stdin không bị duplicate và success không được công bố nếu accepted bytes chưa deliver. Terminal state chỉ visible sau spawn/exit evidence, reap, output drain, input accounting và durable receipt. Capacity/storage rejection không tạo side effect. Retention không được quên idempotency key một cách tự động. Daemon mới không được nhận ownership từ PID/PGID trên disk.

### State and Ownership

ChatGPT sở hữu intent và retry key; bridge chỉ translate; daemon incarnation sở hữu operation reservation, live session, input queue/ledger, process group, capture, quota, signal và receipt; OS sở hữu process primitives; file store là durable evidence, không phải live ownership capability. Trust boundary remote nằm ở tunnel identity; local privileged boundary là protected Unix socket plus peer UID. Core/app/adapter/cmd dependency direction phải được machine-check.

### Execution and Failure Model

Start đi theo validate/fingerprint, capacity, control-storage reservation, durable operation reservation rồi mới spawn. Live transitions qua starting/running/finalizing trước terminal. Poll read-only bằng byte cursor; write atomically admits whole UTF-8 payload vào bounded queue và worker xử lý short syscall; EOF là ordered marker cho pipe; kill có kill ID để dedupe. Crash của owning daemon làm nonterminal session thành abandoned/ambiguous. Capture/input/storage failure chấm dứt owned group và không thể thành success. Descendant cố tình thoát process group và external effects là limitation được công bố.

### Security and Observability

Security model là honest current-user authority, không blacklist giả. Socket ownership/mode/peer credential, umask, anti-symlink, bounded resource, redaction, stable error/failure codes và immutable receipt là bắt buộc. Logging tách khỏi command output và không chứa raw command/env/stdin/output mặc định. Fitness functions gồm schema, state table, architecture graph, size/waiver, affected-selector fixtures, race/fuzz/fault tests, native service tests và MCP/tunnel E2E.

## Architecture Options

| Phương án | Failure được giải quyết | Complexity/failure mới | Chi phí vận hành/chuyển đổi | Điều kiện thắng |
| --- | --- | --- | --- | --- |
| Corrected baseline: `shellbeam mcp` trực tiếp spawn và giữ state trong process | Ít thành phần, dễ prototype | Tunnel/bridge disconnect làm mất owner; retry/cursor/long command không đáng tin | Thấp ban đầu, cao khi vá lifecycle | Chỉ thắng nếu product bỏ yêu cầu long-running và disconnect survival |
| Recommended: stateless bridge + per-user daemon modular monolith + file receipts | Giữ command qua request/tunnel/bridge restart; central ownership, quota và evidence | IPC, service manager, persistence/fault ordering | Vừa phải, phù hợp một binary và personal scale | Thắng với toàn bộ invariant V1 hiện tại |
| Per-session supervisor ngay V1 | Thêm daemon-crash reattach/control | Capability lifecycle, supervisor IPC, orphan cleanup, upgrade/reboot semantics | Cao và mở thêm failure domains trước khi V1 chứng minh nhu cầu | Chỉ thắng khi runtime data cho thấy daemon crash là common/critical hoặc V1 SLO bắt buộc recovery |
| Container/cgroup-backed execution | Containment mạnh hơn process group | Đổi authority contract, portability/install burden, Docker/rootless differences | Rất cao trên macOS/Linux | Chỉ thắng nếu isolation trở thành product requirement thay cho current-user authority |

Phương án recommended có risk-adjusted value tốt nhất. Baseline không đạt journey đã xác nhận; supervisor và container giải quyết requirement đang deferred bằng complexity chưa được biện minh.

## Codebase Assessment

Chưa có codebase để so sánh. Design hiện khớp reference model ở các điểm khó: response-only fields không nằm trong intent fingerprint; capacity/control storage precede persistence/spawn; spawn failure là terminal receipt; global storage giữ control-plane headroom; public `finalizing` thể hiện durable-publication delay; write queue giải quyết short UTF-8 syscall và EOF; kill ID giải quyết lost response; compact tombstone ngăn respawn sau output retention; process-group escape được nói thật. Development policy thiết lập composition root, import barriers, hard/soft size limits, native Go cache, deterministic affected graph và fail-closed broadening. Mức sẵn sàng là “ready for implementation planning”, không phải “implementation proven”.

## Findings

No material findings.

## Recommended Target State

Giữ nguyên target trong document: một Go module, một binary, `cmd` làm composition root; core thuần quyết định, app sở hữu use-case/ports, adapters cô lập MCP/IPC/process/store/service. Đóng băng machine-readable input/output/IPC/receipt/config schemas và state table trước tracer bullet. Daemon serialize operation creation, session mutation và input admission; adapters thực hiện atomic persistence và OS effects. Terminal publication là durable barrier. `devctl` cung cấp architecture/size/schema checks, focused/dirty/checkpoint/release profiles và evidence fingerprint. V1 release native-test macOS/Linux, cross-build bốn tuple, MCP Inspector và tunnel E2E; supervisor chỉ được introduce ở V1.1 sau capability handshake design.

## Migration Strategy

Không có legacy migration. `Keep`: product scope, one-tool design, daemon/bridge split, UDS peer auth, file receipt model, V1 crash rule. `Refine`: đóng băng exact schemas/default budgets trong implementation plan. `Introduce` theo lát cắt: (1) `go.mod`, devctl, architecture/test-impact manifests và core state/contract tests; (2) operation/session/receipt core plus faultable file store proving reservation-before-spawn; (3) real process/PTY/input/output adapter with race/fault integration; (4) versioned UDS IPC and stateless bridge; (5) MCP adapter/Inspector; (6) launchd/systemd-user/doctor; (7) real tunnel E2E and release pipeline. Mỗi slice chỉ cut over khi focused/dirty/checkpoint gate và exact-source receipt pass. Rollback là remove unwired slice hoặc disable adapter at composition root; persisted-schema slice chỉ merge sau golden backward/reject tests. Không chạy hai process owners song song.

## Adversarial Check

Steelman baseline: một stdio process đơn giản hơn, ít persistence và dễ ship. Nó vẫn thua vì disconnect survival và at-most-once start là outcome cốt lõi. Attack recommendation: daemon/service/IPC tăng install/debug burden; file tombstones có growth; control reserve và finalizing khó fault-test; affected selector tự nó là software cần tin cậy; pin SDK/toolchain có maintenance cost. Falsifier chính: nếu user research bỏ long-running/disconnect requirement, direct stdio baseline nên được chọn; nếu daemon crash rate/SLO đòi live recovery, supervisor phải kéo lên; nếu tombstone/state measurements vượt budget, cần compact journal hoặc embedded DB; nếu current-user authority không được chấp nhận, container/profile architecture phải thay thế. Không có bằng chứng hiện tại kích hoạt switch. Review đã hạ confidence runtime và không biến “modern” thành microservice/scale theater.

## Compliance

Report dùng đúng schema/headings, khai báo Context-exposed, giới hạn evidence và runtime gap, so sánh corrected baseline với hai alternative material, chọn một target, nêu migration/rollback/validation và falsifying conditions. Không gán verified behavior cho code chưa tồn tại. Bundled validator đã PASS trên chính file này.

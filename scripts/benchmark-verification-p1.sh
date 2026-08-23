#!/usr/bin/env bash
set -euo pipefail

mode="${1:-baseline}"
case "$mode" in
  baseline)
    printf '%s\n' '{"scenario":"docs_only_four_markdown_specs","source_fingerprint":"8aff94e1f3110a3b5358711ee013fd342e558d494e452f2b547d59846184266e","checkpoint_selection":"full","checkpoint_wall_ms":480000,"commit_gate_selection":"affected:contract:markdown","commit_gate_wall_ms":null,"measurement_quality":"historical_approx","status":"historical_baseline"}'
    ;;
  --measure-current)
    python3 - "${SHELLBEAM_BASE_REF:-origin/main}" <<'PYBENCH'
import json, subprocess, sys, time
base = sys.argv[1]
def run(kind, args):
    started = time.monotonic_ns()
    proc = subprocess.run(args, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    wall_ms = (time.monotonic_ns() - started) // 1_000_000
    if proc.returncode != 0:
        sys.stderr.write(proc.stdout + proc.stderr)
        raise SystemExit(proc.returncode)
    lines = [line for line in proc.stdout.splitlines() if line.lstrip().startswith("{")]
    if not lines:
        raise SystemExit(f"{kind}: missing devctl JSON")
    payload = json.loads(lines[-1])
    return payload, wall_ms
commit, commit_ms = run("commit_gate", ["go","run","./tools/devctl","commit-gate","--base",base,"--json"])
checkpoint, checkpoint_ms = run("checkpoint", ["go","run","./tools/devctl","verify","--checkpoint","--base",base,"--json"])
print(json.dumps({
    "scenario":"docs_only_four_markdown_specs",
    "source_fingerprint":checkpoint["source_fingerprint"],
    "checkpoint_selection":checkpoint["selection"],
    "checkpoint_wall_ms":checkpoint_ms,
    "commit_gate_selection":commit["selection"],
    "commit_gate_wall_ms":commit_ms,
    "measurement_quality":"measured_local",
    "status":"measured",
}, separators=(",",":"), sort_keys=True))
PYBENCH
    ;;
  --scenario)
    scenario="${2:-}"
    if [[ -z "$scenario" ]]; then
      echo "usage: $0 --scenario <docs-only|local-go|shared-go|fail-pass|leak|first-policy>" >&2
      exit 2
    fi
    python3 - "$scenario" "$(pwd)" <<'PYSCENARIO'
import http.client
import json
import os
import pathlib
import shutil
import socket
import subprocess
import sys
import tempfile
import time

SCENARIO = sys.argv[1]
SOURCE_ROOT = pathlib.Path(sys.argv[2]).resolve()
ALLOWED = {"docs-only", "local-go", "shared-go", "fail-pass", "leak", "first-policy"}
if SCENARIO not in ALLOWED:
    raise SystemExit(f"unsupported scenario {SCENARIO!r}; expected one of {sorted(ALLOWED)}")

class UnixHTTPConnection(http.client.HTTPConnection):
    def __init__(self, unix_socket, timeout=10):
        super().__init__("localhost", timeout=timeout)
        self.unix_socket = unix_socket
    def connect(self):
        self.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self.sock.settimeout(self.timeout)
        self.sock.connect(self.unix_socket)

class Bench:
    def __init__(self, scenario):
        self.scenario = scenario
        self.root = pathlib.Path(tempfile.mkdtemp(prefix="sbp1-", dir="/tmp"))
        self.repo = self.root / "repo"
        self.state = self.root / "state"
        self.runtime = self.root / "run"
        self.binary = self.root / "shellbeam"
        self.socket = self.runtime / "daemon.sock"
        self.daemon = None
        self.daemon_log = None
        self.workspace_id = ""
        self.ipc_calls = 0
        self.verification_executions = 0
        self.operation_ids = []
        self.special_suite_executions = 0
        self.full_suite_executions = 0
        self.telemetry = []
        self.initial_policy_state = None
        self.proposal_policy_state = None
        self.final_inspection = None
        self.final_inspection_bytes = 0
        self.expected_mandatory = []
        self.extra = {}
        self.started_ns = time.monotonic_ns()

    def cleanup(self):
        if self.daemon is not None and self.daemon.poll() is None:
            self.daemon.terminate()
            try:
                self.daemon.wait(timeout=5)
            except subprocess.TimeoutExpired:
                self.daemon.kill()
                self.daemon.wait(timeout=5)
        if self.daemon_log is not None:
            self.daemon_log.close()
        shutil.rmtree(self.root, ignore_errors=True)

    def run(self, args, cwd=None, check=True):
        p = subprocess.run(args, cwd=cwd, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        if check and p.returncode != 0:
            raise RuntimeError(f"command failed rc={p.returncode}: {args!r}\nstdout={p.stdout}\nstderr={p.stderr}")
        return p

    def git(self, *args):
        return self.run(["git", *args], cwd=self.repo)

    def write(self, rel, text):
        path = self.repo / rel
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(text)

    def append(self, rel, text):
        path = self.repo / rel
        with path.open("a") as f:
            f.write(text)

    def build(self):
        p = self.run(["go", "build", "-o", str(self.binary), "./cmd/shellbeam"], cwd=SOURCE_ROOT)
        if p.stderr:
            sys.stderr.write(p.stderr)

    def init_repo(self):
        self.repo.mkdir(parents=True)
        self.git("init", "-q")
        self.git("config", "user.email", "p1-benchmark@example.invalid")
        self.git("config", "user.name", "P1 Benchmark")

    def commit_all(self, message):
        self.git("add", "-A")
        self.git("commit", "-q", "-m", message)

    def attach_and_start(self):
        self.state.mkdir(parents=True, exist_ok=True, mode=0o700)
        self.runtime.mkdir(parents=True, exist_ok=True, mode=0o700)
        os.chmod(self.state, 0o700)
        os.chmod(self.runtime, 0o700)
        attached = self.run([
            str(self.binary), "workspace", "attach", str(self.repo), "--label", "bench",
            "--state-dir", str(self.state), "--runtime-dir", str(self.runtime), "--json",
        ])
        self.workspace_id = json.loads(attached.stdout)["workspace_id"]
        self.daemon_log = open(self.root / "daemon.log", "w+")
        self.daemon = subprocess.Popen([
            str(self.binary), "daemon", "--state-dir", str(self.state), "--runtime-dir", str(self.runtime), "--shell", "/bin/sh",
        ], cwd=self.repo, stdout=self.daemon_log, stderr=subprocess.STDOUT, text=True)
        deadline = time.monotonic() + 20
        while time.monotonic() < deadline:
            if self.daemon.poll() is not None:
                self.daemon_log.flush(); self.daemon_log.seek(0)
                raise RuntimeError("daemon exited before readiness:\n" + self.daemon_log.read())
            p = self.run([
                str(self.binary), "doctor", "--state-dir", str(self.state), "--runtime-dir", str(self.runtime),
                "--shell", "/bin/sh", "--json", "--require-ready",
            ], cwd=self.repo, check=False)
            if p.returncode == 0 and self.socket.exists():
                return
            time.sleep(0.05)
        raise RuntimeError("daemon readiness timeout")

    def ipc(self, action, **fields):
        self.ipc_calls += 1
        payload = {"ipc_version": 2, "kind": "request", "request_id": f"bench-{self.scenario}-{self.ipc_calls}", "action": action}
        payload.update(fields)
        body = json.dumps(payload, separators=(",", ":"), sort_keys=True).encode()
        conn = UnixHTTPConnection(str(self.socket), timeout=30)
        try:
            conn.request("POST", "/v2/local-shell", body=body, headers={"Content-Type": "application/json", "Content-Length": str(len(body))})
            response = conn.getresponse()
            raw = response.read()
        finally:
            conn.close()
        if response.status != 200:
            raise RuntimeError(f"ipc status {response.status}: {raw!r}")
        decoded = json.loads(raw)
        if not decoded.get("ok", False):
            raise RuntimeError(f"ipc {action} failed: {decoded}")
        return decoded, len(raw)

    def inspect_verification(self):
        response, n = self.ipc("inspect.verification", workspace_id=self.workspace_id, phase="checkpoint")
        inspection = response["verification"]
        return inspection, n

    def preview_policy(self):
        response, _ = self.ipc("verification.policy.preview", workspace_id=self.workspace_id)
        return response["verification_policy_preview"]

    def activate_policy(self, label):
        preview = self.preview_policy()
        proposal = preview.get("proposal")
        if preview.get("state") != "valid" or not proposal:
            raise RuntimeError(f"policy preview not valid: {preview}")
        before, _ = self.inspect_verification()
        if not before.get("source_generation"):
            raise RuntimeError(f"proposal source generation unavailable: {before}")
        self.proposal_policy_state = before.get("policy_state")
        proposal_generation = before["source_generation"]
        self.write(f"activation-{label}.cut", "activate\n")
        cut, _ = self.inspect_verification()
        if cut.get("source_generation") == proposal_generation:
            raise RuntimeError("activation cut did not change fast workspace generation")
        activation_id = "act_bench_" + label.replace("-", "_")
        response, _ = self.ipc(
            "verification.policy.activate",
            workspace_id=self.workspace_id,
            activation_id=activation_id,
            proposed_policy_digest=proposal["policy_digest"],
            expected_previous_policy_digest="absent",
            proposal_generation=proposal_generation,
            authority="explicit_caller",
            actor="p1-benchmark",
        )
        result = response["verification_activation"]
        if not result.get("effective", False):
            raise RuntimeError(f"activation was not effective: {result}")
        return proposal["policy_digest"]

    def prepare_current_cut(self):
        inspection, _ = self.inspect_verification()
        if not inspection.get("source_generation"):
            raise RuntimeError(f"current source generation unavailable: {inspection}")
        return inspection

    def start_typed(self, operation_id, command_id, verification_attempt=None):
        fields = {
            "operation_id": operation_id,
            "workspace_id": self.workspace_id,
            "project_command_id": command_id,
            "yield_time_ms": 1000,
            "max_output_bytes": 4096,
        }
        if verification_attempt is not None:
            fields["verification_attempt"] = verification_attempt
        response, _ = self.ipc("start", **fields)
        result = response.get("result") or {}
        operation = result.get("operation") or {}
        child = result.get("child") or {}
        sid = operation.get("session_id")
        if not sid:
            raise RuntimeError(f"typed start returned no session: {response}")
        terminal = {"completed", "failed", "timed_out", "killed", "abandoned", "terminal"}
        cursor = int((result.get("output") or {}).get("next_cursor", 0) or 0)
        deadline = time.monotonic() + 20
        while operation.get("state") not in terminal:
            if time.monotonic() >= deadline:
                raise RuntimeError(f"operation {operation_id} did not reach terminal state: {result}")
            response, _ = self.ipc("poll", session_id=sid, cursor=cursor, **{"yield_time_ms": 500, "max_output_bytes": 4096})
            result = response.get("result") or {}
            operation = result.get("operation") or {}
            child = result.get("child") or {}
            cursor = int((result.get("output") or {}).get("next_cursor", cursor) or cursor)
        self.verification_executions += 1
        self.operation_ids.append(operation_id)
        return {"state": operation.get("state"), "outcome": child.get("outcome"), "child_state": child.get("state"), "receipt": result.get("receipt")}

    def wait_evidence(self, operation_id):
        deadline = time.monotonic() + 15
        last = None
        while time.monotonic() < deadline:
            response, _ = self.ipc("inspect.evidence", operation_id=operation_id, max_records=1)
            result = response.get("evidence") or {}
            last = result
            records = result.get("records") or []
            if records:
                return records[0]
            time.sleep(0.05)
        raise RuntimeError(f"evidence did not materialize for {operation_id}: {last}")

    def collect_telemetry(self):
        values = []
        for operation_id in self.operation_ids:
            try:
                response, _ = self.ipc("inspect.telemetry", operation_id=operation_id, max_samples=8)
                result = response.get("telemetry") or {}
            except Exception as exc:
                values.append({"operation_id": operation_id, "status": "unavailable", "reason": type(exc).__name__})
                continue
            item = {"operation_id": operation_id, "status": result.get("status", "unavailable")}
            latest = result.get("latest") or {}
            resources = latest.get("resources") or {}
            for key in ("cpu_user_ms", "cpu_system_ms", "max_rss_bytes", "process_count_peak"):
                metric = resources.get(key)
                if isinstance(metric, dict) and metric.get("quality") not in (None, "unavailable") and "value" in metric:
                    item[key] = metric
            values.append(item)
        self.telemetry = values

    def finish(self, inspection, inspection_bytes):
        self.final_inspection = inspection
        self.final_inspection_bytes = inspection_bytes
        obligations = inspection.get("obligations") or []
        actual_mandatory = sorted(o.get("source_rule_id") for o in obligations if o.get("disposition") == "required_now")
        expected = sorted(self.expected_mandatory)
        missed = sorted(set(expected) - set(actual_mandatory))
        wasteful = sorted(set(actual_mandatory) - set(expected))
        views = inspection.get("obligation_views") or []
        evidence_statuses = {v.get("source_rule_id"): v.get("evidence_status") for v in views}
        gate = inspection.get("gate") or {}
        self.collect_telemetry()
        wall_ms = (time.monotonic_ns() - self.started_ns) // 1_000_000
        result = {
            "scenario": self.scenario,
            "status": "measured",
            "measurement_quality": "measured_local_real_daemon",
            "model_tool_calls": self.ipc_calls,
            "model_tool_calls_definition": "public IPC calls; fixture setup and doctor polling excluded",
            "inspect_response_bytes": self.final_inspection_bytes,
            "wall_ms": wall_ms,
            "verification_executions": self.verification_executions,
            "full_suite_executions": self.full_suite_executions,
            "special_suite_executions": self.special_suite_executions,
            "source_generation": inspection.get("source_generation"),
            "policy_state": inspection.get("policy_state"),
            "gate_status": gate.get("status"),
            "gate_reason_codes": gate.get("reason_codes") or [],
            "expected_mandatory_obligations": expected,
            "actual_mandatory_obligations": actual_mandatory,
            "mandatory_obligation_misses": missed,
            "wasteful_obligations": wasteful,
            "evidence_statuses": evidence_statuses,
            "stale_or_inconsistent_evidence": sorted(k for k, v in evidence_statuses.items() if v in {"insufficient", "inconsistent", "unknown"}),
            "leaked_resource_count": {"status": "unavailable", "reason": "no qualified lifecycle count exposed by P1 inspect.verification"},
            "telemetry": self.telemetry,
        }
        result.update(self.extra)
        print(json.dumps(result, separators=(",", ":"), sort_keys=True))

DOCS_MANIFEST = '''schema_version = 2
[commands.verify_docs]
argv = ["/bin/sh", "-c", "true"]
cwd = "."
kind = "test"
source_scope = "full"
'''

DOCS_GO_POLICY = '''schema_version = 1
policy_id = "bench-docs-go"
[[rules]]
id = "docs-contract"
phases = ["checkpoint"]
match_paths = ["docs/**"]
ownership = "application_owned"
required = true
sufficiency_basis = "docs contract"
minimum_affected_authority = "mechanical"
[[rules.evidence]]
id = "docs-verify"
provider_class = "project_command"
project_command_id = "verify_docs"
minimum_authority = "mechanical"
require_current = true
environment = "none"
stability = "no_contradiction"
execution = { parallel_safe = true, expected_workload_class = "light" }
[[rules]]
id = "go-local"
phases = ["checkpoint"]
match_paths = ["app/**"]
ownership = "application_owned"
required = true
sufficiency_basis = "local go contract"
minimum_affected_authority = "mechanical"
[[rules.evidence]]
id = "go-verify"
provider_class = "project_command"
project_command_id = "verify_go"
minimum_authority = "mechanical"
require_current = true
environment = "none"
stability = "no_contradiction"
execution = { parallel_safe = true, expected_workload_class = "light" }
'''

GO_MANIFEST = '''schema_version = 2
[commands.verify_docs]
argv = ["/bin/sh", "-c", "true"]
cwd = "."
kind = "test"
source_scope = "full"
[commands.verify_go]
argv = ["go", "test", "./app"]
cwd = "."
kind = "test"
source_scope = "affected"
'''

SHARED_POLICY = '''schema_version = 1
policy_id = "bench-shared-go"
[[rules]]
id = "go-shared"
phases = ["checkpoint"]
match_paths = ["shared/**"]
ownership = "application_owned"
required = true
sufficiency_basis = "shared package contract"
minimum_affected_authority = "mechanical"
[[rules.evidence]]
id = "shared-verify"
provider_class = "project_command"
project_command_id = "verify_shared"
minimum_authority = "mechanical"
require_current = true
environment = "none"
stability = "no_contradiction"
[[rules]]
id = "go-dependents"
phases = ["checkpoint"]
match_paths = ["a/**", "b/**"]
ownership = "application_owned"
required = true
sufficiency_basis = "reverse importer contract"
minimum_affected_authority = "mechanical"
[[rules.evidence]]
id = "dependent-verify"
provider_class = "project_command"
project_command_id = "verify_shared"
minimum_authority = "mechanical"
require_current = true
environment = "none"
stability = "no_contradiction"
'''
SHARED_MANIFEST = '''schema_version = 2
[commands.verify_shared]
argv = ["go", "test", "./..."]
cwd = "."
kind = "test"
source_scope = "affected"
'''

b = Bench(SCENARIO)
try:
    b.build()
    b.init_repo()
    if SCENARIO == "docs-only":
        for name in ("a.md", "b.md", "c.md", "d.md"):
            b.write(f"docs/{name}", f"# {name}\n")
        b.write("app/app.go", "package app\n")
        b.write("go.mod", "module example.com/bench\n\ngo 1.22\n")
        b.write(".shellbeam/project.toml", GO_MANIFEST)
        b.write(".shellbeam/verification-policy.toml", DOCS_GO_POLICY)
        b.commit_all("seed docs-only")
        b.attach_and_start(); b.activate_policy("docs_only")
        for name in ("a.md", "b.md", "c.md", "d.md"):
            b.append(f"docs/{name}", "changed\n")
        b.expected_mandatory = ["docs-contract"]
        inspection, n = b.inspect_verification(); b.finish(inspection, n)
    elif SCENARIO == "local-go":
        b.write("go.mod", "module example.com/bench\n\ngo 1.22\n")
        b.write("app/app.go", "package app\n\nfunc Value() int { return 1 }\n")
        b.write("docs/readme.md", "# docs\n")
        b.write(".shellbeam/project.toml", GO_MANIFEST)
        b.write(".shellbeam/verification-policy.toml", DOCS_GO_POLICY)
        b.commit_all("seed local-go")
        b.attach_and_start(); b.activate_policy("local_go")
        b.append("app/app.go", "\nfunc Other() int { return 2 }\n")
        b.expected_mandatory = ["go-local"]
        inspection, n = b.inspect_verification(); b.finish(inspection, n)
    elif SCENARIO == "shared-go":
        b.write("go.mod", "module example.com/bench\n\ngo 1.22\n")
        b.write("shared/shared.go", "package shared\n\nfunc Value() int { return 1 }\n")
        b.write("a/a.go", 'package a\n\nimport "example.com/bench/shared"\n\nfunc A() int { return shared.Value() }\n')
        b.write("b/b.go", 'package b\n\nimport "example.com/bench/shared"\n\nfunc B() int { return shared.Value() }\n')
        b.write(".shellbeam/project.toml", SHARED_MANIFEST)
        b.write(".shellbeam/verification-policy.toml", SHARED_POLICY)
        b.commit_all("seed shared-go")
        b.attach_and_start(); b.activate_policy("shared_go")
        b.append("shared/shared.go", "\nfunc Other() int { return 2 }\n")
        b.expected_mandatory = ["go-shared", "go-dependents"]
        inspection, n = b.inspect_verification(); b.finish(inspection, n)
    elif SCENARIO == "fail-pass":
        control = b.root / "fail-control"
        manifest = f'''schema_version = 2
[commands.verify_docs]
argv = ["/bin/test", "!", "-e", {json.dumps(str(control))}]
cwd = "."
kind = "test"
source_scope = "full"
'''
        b.write("docs/guide.md", "# guide\n")
        b.write(".shellbeam/project.toml", manifest)
        b.write(".shellbeam/verification-policy.toml", DOCS_GO_POLICY.split('[[rules]]\nid = "go-local"')[0])
        b.commit_all("seed fail-pass")
        b.attach_and_start(); b.activate_policy("fail_pass")
        b.append("docs/guide.md", "changed\n")
        b.expected_mandatory = ["docs-contract"]
        b.prepare_current_cut()
        control.write_text("fail\n")
        first = b.start_typed("op-bench-fail-pass-fail", "verify_docs")
        ev1 = b.wait_evidence("op-bench-fail-pass-fail")
        if first.get("outcome") != "failure":
            raise RuntimeError(f"expected first fail, got {first}")
        control.unlink()
        second = b.start_typed("op-bench-fail-pass-pass", "verify_docs")
        ev2 = b.wait_evidence("op-bench-fail-pass-pass")
        if second.get("outcome") != "success":
            raise RuntimeError(f"expected second pass, got {second}")
        inspection, n = b.inspect_verification()
        views = {v.get("source_rule_id"): v for v in inspection.get("obligation_views") or []}
        if (views.get("docs-contract") or {}).get("evidence_status") != "inconsistent":
            raise RuntimeError(f"FAIL->PASS did not remain inconsistent: {inspection}")
        b.extra["fail_pass_evidence_ids"] = sorted([ev1["record"]["evidence_id"], ev2["record"]["evidence_id"]])
        b.finish(inspection, n)
    elif SCENARIO == "leak":
        policy = DOCS_GO_POLICY.split('[[rules]]\nid = "go-local"')[0].replace('stability = "no_contradiction"', 'stability = "no_contradiction"\nrequire_quiescence = true', 1)
        b.write("docs/guide.md", "# guide\n")
        b.write(".shellbeam/project.toml", DOCS_MANIFEST)
        b.write(".shellbeam/verification-policy.toml", policy)
        b.commit_all("seed leak")
        b.attach_and_start(); b.activate_policy("leak")
        b.append("docs/guide.md", "changed\n")
        b.expected_mandatory = ["docs-contract"]
        b.prepare_current_cut()
        view = b.start_typed("op-bench-leak", "verify_docs")
        b.wait_evidence("op-bench-leak")
        if view.get("outcome") != "success":
            raise RuntimeError(f"leak fixture command was not a literal pass: {view}")
        inspection, n = b.inspect_verification()
        views = {v.get("source_rule_id"): v for v in inspection.get("obligation_views") or []}
        reason_codes = (views.get("docs-contract") or {}).get("reason_codes") or []
        quiescence_reasons = [code for code in reason_codes if code in {"quiescence_unknown", "quiescence_unavailable"}]
        if len(quiescence_reasons) != 1:
            raise RuntimeError(f"missing explicit quiescence uncertainty: {inspection}")
        b.extra["lifecycle_qualification"] = "unavailable"
        b.extra["quiescence_reason"] = quiescence_reasons[0]
        b.finish(inspection, n)
    elif SCENARIO == "first-policy":
        b.write("docs/guide.md", "# guide\n")
        b.write(".shellbeam/project.toml", DOCS_MANIFEST)
        b.commit_all("seed no-policy")
        b.attach_and_start()
        absent, _ = b.inspect_verification()
        b.initial_policy_state = absent.get("policy_state")
        if b.initial_policy_state != "absent":
            raise RuntimeError(f"first policy fixture did not start absent: {absent}")
        b.write(".shellbeam/verification-policy.toml", DOCS_GO_POLICY.split('[[rules]]\nid = "go-local"')[0])
        b.commit_all("add first policy")
        b.activate_policy("first_policy")
        b.append("docs/guide.md", "changed after activation\n")
        b.expected_mandatory = ["docs-contract"]
        inspection, n = b.inspect_verification()
        b.extra["initial_policy_state"] = b.initial_policy_state
        b.extra["proposal_policy_state"] = b.proposal_policy_state
        b.finish(inspection, n)
finally:
    b.cleanup()
PYSCENARIO
    ;;
  *)
    echo "usage: $0 [baseline|--measure-current|--scenario <docs-only|local-go|shared-go|fail-pass|leak|first-policy>]" >&2
    exit 2
    ;;
esac

#!/usr/bin/env python3
import hashlib, json, pathlib, re, sys
ROOT=pathlib.Path(__file__).resolve().parents[1]
meta_path=ROOT/'docs/superpowers/plans/2026-08-19-decision-protocol-v1-traceability.json'
meta=json.loads(meta_path.read_text())
spec_path=ROOT/meta['spec_path']; plan_path=ROOT/meta['plan_path']
spec=spec_path.read_text(); plan=plan_path.read_text()
errors=[]
actual_sha=hashlib.sha256(spec_path.read_bytes()).hexdigest()
if actual_sha != meta['spec_sha256']: errors.append(f"spec sha {actual_sha} != {meta['spec_sha256']}")
sections={int(n):title.strip() for n,title in re.findall(r'^## (\d+)\. (.+)$',spec,re.M)}
if set(sections)!=set(range(1,58)): errors.append(f"spec sections={sorted(sections)}")
meta_sections={x['section']:x for x in meta['sections']}
if set(meta_sections)!=set(range(1,58)): errors.append(f"trace sections={sorted(meta_sections)}")
for n in range(1,58):
    x=meta_sections.get(n,{})
    if x.get('title') != sections.get(n): errors.append(f"section {n} title mismatch")
    if not x.get('tasks'): errors.append(f"section {n} has no tasks")
block=spec.split('## 56. Frozen V1 invariants',1)[1].split('## 57.',1)[0]
invariants={int(n):text.strip() for n,text in re.findall(r'^(\d+)\. (.+)$',block,re.M)}
meta_inv={x['invariant']:x for x in meta['invariants']}
if set(invariants)!=set(range(1,49)): errors.append(f"spec invariants={sorted(invariants)}")
if set(meta_inv)!=set(range(1,49)): errors.append(f"trace invariants={sorted(meta_inv)}")
for n in range(1,49):
    x=meta_inv.get(n,{})
    if x.get('text') != invariants.get(n): errors.append(f"invariant {n} text mismatch")
    if not x.get('tasks'): errors.append(f"invariant {n} has no tasks")
plan_tasks={int(n):title.strip() for n,title in re.findall(r'^### Task (\d+): (.+)$',plan,re.M)}
meta_tasks={x['task']:x['title'] for x in meta['tasks']}
if set(plan_tasks)!=set(range(14)): errors.append(f"plan tasks={sorted(plan_tasks)}")
if meta_tasks != plan_tasks: errors.append('trace task titles do not match plan headings')
valid_tasks=set(range(14))
for kind,items in [('section',meta['sections']),('invariant',meta['invariants'])]:
    for item in items:
        bad=set(item['tasks'])-valid_tasks
        if bad: errors.append(f"{kind} {item[kind]} bad task refs {sorted(bad)}")
required=['6cf49426243f26e8bec862c29651304ccc4abd5e1f91947f9899fe21fd72f7fa','27207d94b097040b571081c8c49d9c09487460c5','DecisionProjectionDigest','ExperimentExecutionLink','ExperimentObservationBinding','semantic_intent_fingerprint','explicit_caller']
for needle in required:
    if needle not in plan: errors.append(f"plan missing anchor {needle}")
canonical_records=['DecisionPolicySnapshot','DecisionPolicyActivation','DecisionEpisode','DecisionCandidate','DecisionExperiment','ExperimentSeal','PredictionBinding','ExperimentExecutionLink','ExperimentObservationBinding','ExperimentClosure','ExperimentAbort','VerifierAssessment','SelectionProposal','DecisionAuthorityAttestation','DecisionOverride','SelectionCommit','DecisionClosure']
for name in canonical_records:
    if name not in plan: errors.append(f"plan missing canonical record {name}")
actions=['decision.policy.snapshot','decision.policy.activate','decision.create','decision.inspect','decision.evaluate','decision.close_unresolved','decision.candidate.create','decision.candidate.revise','decision.experiment.define','decision.prediction.bind','decision.experiment.seal','decision.experiment.close','decision.experiment.abort','decision.assessment.record','decision.selection.propose','decision.override.create','decision.selection.commit','decision.authority.materialize']
for action in actions:
    if action not in plan: errors.append(f"plan missing action {action}")

# Cross-task transport contract: the trace file is the machine-readable 18-action request matrix.
matrix_items=meta.get('action_request_matrix',[])
matrix={x.get('action'):x for x in matrix_items}
if set(matrix) != set(actions): errors.append(f"action request matrix actions={sorted(matrix)}")
expected_request_fields={
    'decision.policy.snapshot':({'policy'},set()),
    'decision.policy.activate':({'activation_id','policy_digest','proposal_generation','expected_previous_policy_digest','actor_ref'},set()),
    'decision.create':({'episode_id','episode_kind','actor_ref'},{'predecessor_episode_id','expected_policy_digest','expected_activation_ref'}),
    'decision.inspect':({'episode_id'},{'candidate_id'}),
    'decision.evaluate':({'episode_id','candidate_id'},set()),
    'decision.close_unresolved':({'episode_id','actor_ref','expected_projection_digest','reason','unresolved_dimensions'},set()),
    'decision.candidate.create':({'episode_id','candidate','actor_ref'},set()),
    'decision.candidate.revise':({'episode_id','parent_candidate_id','candidate','actor_ref'},set()),
    'decision.experiment.define':({'episode_id','experiment_id','actor_ref'},set()),
    'decision.prediction.bind':({'episode_id','experiment_id','prediction'},set()),
    'decision.experiment.seal':({'experiment_id','actor_ref'},set()),
    'decision.experiment.close':({'experiment_id','actor_ref'},set()),
    'decision.experiment.abort':({'experiment_id','abort_phase','actor_ref','reason'},set()),
    'decision.assessment.record':({'episode_id','assessment','actor_ref'},set()),
    'decision.selection.propose':({'episode_id','candidate_id','actor_ref'},{'reason'}),
    'decision.override.create':({'episode_id','candidate_id','expected_policy_digest','expected_projection_digest','blocking_requirement_digest','authority_attestation_ref','reason'},set()),
    'decision.selection.commit':({'episode_id','candidate_id','actor_ref','expected_policy_digest','expected_projection_digest','idempotency_key'},{'override_ref'}),
    'decision.authority.materialize':({'authority_request'},set()),
}
for action,(required_fields,optional_fields) in expected_request_fields.items():
    item=matrix.get(action,{})
    got_required=set(item.get('required',[])); got_optional=set(item.get('optional',[]))
    if got_required != required_fields: errors.append(f"{action} required fields={sorted(got_required)}")
    if got_optional != optional_fields: errors.append(f"{action} optional fields={sorted(got_optional)}")
    if got_required & got_optional: errors.append(f"{action} required/optional overlap")
    if len(item.get('server_derived',[])) != len(set(item.get('server_derived',[]))): errors.append(f"{action} duplicate server-derived fields")
if 'trusted_actor_ref' not in set(matrix.get('decision.authority.materialize',{}).get('server_derived',[])):
    errors.append('authority materialize lacks server-derived trusted_actor_ref')
if 'actor_ref' in set(matrix.get('decision.authority.materialize',{}).get('required',[])+matrix.get('decision.authority.materialize',{}).get('optional',[])):
    errors.append('authority materialize exposes caller actor_ref')
if 'actor_ref' in set(matrix.get('decision.override.create',{}).get('required',[])+matrix.get('decision.override.create',{}).get('optional',[])):
    errors.append('override create exposes caller actor_ref')
if 'trusted_actor_ref' not in set(matrix.get('decision.override.create',{}).get('server_derived',[])):
    errors.append('override create lacks server-derived trusted_actor_ref')
policy_contract=meta.get('policy_activation_contract',{})
if policy_contract.get('proposal_generation_type') != 'string': errors.append('policy proposal generation type must be string')
if policy_contract.get('proposal_generation_pattern') != '^gen_[0-9a-f]{64}$': errors.append('policy proposal generation pattern mismatch')
if policy_contract.get('expected_previous_policy_digest_required') is not True: errors.append('previous policy digest must be required')
if policy_contract.get('expected_previous_policy_digest_values') != 'absent_or_pol_<64_lowercase_hex>': errors.append('previous policy digest domain mismatch')
if policy_contract.get('policy_snapshot_transport_input') != 'PolicyContent_only_server_derives_repository_and_digest': errors.append('policy snapshot input authority mismatch')
snapshot_server=set(matrix.get('decision.policy.snapshot',{}).get('server_derived',[]))
if not {'repository_id','policy_digest'} <= snapshot_server: errors.append('policy snapshot matrix does not server-derive repository/digest')
snapshot_input=re.search(r'type DecisionPolicySnapshotInputV1 struct \{(.*?)\n\}',plan,re.S)
if not snapshot_input:
    errors.append('DecisionPolicySnapshotInputV1 block missing')
else:
    snapshot_fields=set(re.findall(r'json:\"([^,\"]+)',snapshot_input.group(1)))
    if snapshot_fields != {'content'}: errors.append(f'policy snapshot input fields={sorted(snapshot_fields)}')
for anchor in ['ProposalGeneration           string // exact gen_<64 lowercase hex>','ExpectedPreviousPolicyDigest string // REQUIRED: "absent" or exact pol_<64 lowercase hex>','type DecisionPolicySnapshotInputV1 struct','Content decisionprotocol.PolicyContent `json:"content"`','Policy                       *DecisionPolicySnapshotInputV1','`decision.policy.activate.proposal_generation` is required and must match `^gen_[0-9a-f]{64}$`','`decision.policy.activate.expected_previous_policy_digest` is required']:
    if anchor not in plan: errors.append(f'plan missing policy activation/input anchor {anchor}')
if '*decisionprotocol.PolicySnapshot     `json:"policy,omitempty"`' in plan: errors.append('transport still accepts canonical PolicySnapshot input')
if 'ProposalGeneration           *uint64' in plan or 'ProposalGeneration           uint64' in plan: errors.append('policy proposal generation still scalar uint64')

# Every caller field in the matrix must be representable by the bounded DecisionRequestV1 DTO.
dto_match=re.search(r'type DecisionRequestV1 struct \{(.*?)\n\}',plan,re.S)
if not dto_match:
    errors.append('DecisionRequestV1 block missing')
else:
    dto_fields=set(re.findall(r'json:\"([^,\"]+)',dto_match.group(1)))
    matrix_fields=set()
    for item in matrix.values():
        matrix_fields.update(item.get('required',[])); matrix_fields.update(item.get('optional',[]))
    if dto_fields != matrix_fields:
        errors.append(f"DecisionRequestV1 fields mismatch matrix missing={sorted(matrix_fields-dto_fields)} extra={sorted(dto_fields-matrix_fields)}")

# Task 0 durable implementation-base authority and per-task rebinding.
base_proto=meta.get('implementation_base_protocol',{})
if base_proto.get('plan_authoring_base') != meta.get('plan_authoring_base'): errors.append('implementation base protocol authoring base mismatch')
if base_proto.get('durable_authority') != 'docs/superpowers/evidence/2026-08-19-decision-protocol-v1-baseline.md#implementation_base': errors.append('implementation base durable authority mismatch')
if base_proto.get('rebind_helper') != 'scripts/decision-protocol-v1-implementation-base.sh': errors.append('implementation base helper mismatch')
if base_proto.get('current_main_ref') != 'main': errors.append('implementation base current-main authority mismatch')
if base_proto.get('owner_overlap_policy') != 'stop_and_rereview': errors.append('implementation base owner-overlap policy mismatch')
if base_proto.get('tasks_requiring_rebind') != list(range(1,14)): errors.append('implementation base task rebind set mismatch')
if base_proto.get('previous_base_source') != 'recorded_implementation_base_or_plan_authoring_base': errors.append('implementation previous-base authority mismatch')
if base_proto.get('owner_audit_base') != 'plan_authoring_base': errors.append('implementation owner-audit base mismatch')
if base_proto.get('integration_replay_base') != 'previous_implementation_base': errors.append('implementation replay-base mismatch')
if base_proto.get('repeatable_main_drift') is not True: errors.append('implementation repeated-main-drift protocol missing')
for anchor in ['PREVIOUS_IMPLEMENTATION_BASE','git merge-base --is-ancestor "$PREVIOUS_IMPLEMENTATION_BASE" "$CURRENT_MAIN"','test "$(git merge-base HEAD "$CURRENT_MAIN")" = "$PREVIOUS_IMPLEMENTATION_BASE"','git rebase --onto "$CURRENT_MAIN" "$PREVIOUS_IMPLEMENTATION_BASE"','previous_implementation_base: `${PREVIOUS_IMPLEMENTATION_BASE}`']:
    if anchor not in plan: errors.append(f'plan missing repeated-main-drift anchor {anchor}')
if 'test "$(git merge-base HEAD "$CURRENT_MAIN")" = "$PLAN_AUTHORING_BASE"' in plan: errors.append('Task 0 still hardcodes plan authoring base as replay topology boundary')
helper_call='export SHELLBEAM_BASE_REF="$(scripts/decision-protocol-v1-implementation-base.sh)"'
for task in range(1,14):
    start=re.search(rf'^### Task {task}:',plan,re.M)
    next_task=re.search(rf'^### Task {task+1}:',plan,re.M) if task < 13 else re.search(r'^## Plan Completion Gate',plan,re.M)
    if not start or not next_task: errors.append(f"cannot isolate task {task} for base binding")
    else:
        block=plan[start.start():next_task.start()]
        if helper_call not in block: errors.append(f"task {task} does not rebind durable implementation base")
        files_pos=block.find('**Files:**')
        helper_pos=block.find(helper_call)
        if files_pos < 0 or helper_pos < 0 or helper_pos > files_pos: errors.append(f"task {task} base gate is not before file-edit instructions")
if 'export SHELLBEAM_BASE_REF="$(git rev-parse HEAD)"' in plan: errors.append('plan still binds implementation base from planning HEAD')

# Policy snapshot/activation must participate in the same canonical ledger as all other records.
policy_proto=meta.get('canonical_policy_ledger_protocol',{})
if set(policy_proto.get('canonical_record_kinds',[])) != {'DecisionPolicySnapshot','DecisionPolicyActivation'}: errors.append('canonical policy ledger kinds mismatch')
if policy_proto.get('source_of_truth') != 'decision_protocol/ledger/records/<canonical_record_seq>.json': errors.append('canonical policy ledger source mismatch')
if set(policy_proto.get('secondary_materializations',[])) != {'policies/','activations/','effective/'}: errors.append('canonical policy secondary materializations mismatch')
for anchor in ['`PutPolicySnapshot` must append/replay `RecordPolicySnapshot`','ActivatePolicyCAS` must append/replay `RecordPolicyActivation`','secondary indexes/materializations only']:
    if anchor not in plan: errors.append(f"plan missing canonical policy ledger anchor {anchor}")

# Recovery/admission/cut precision blockers from independent plan review.
for anchor in ['type PutPolicySnapshotRequest struct','type ActivatePolicyRequest struct','type CreateOverrideRequest struct','LinkID                         string','WorkspaceID                    string','AdmittedAt                     time.Time','VerificationObservationCut','EvidenceIndexGeneration uint64','AcquireVerificationObservationCut','proposal_generation,omitempty','expected_previous_policy_digest,omitempty','authority_attestation_ref,omitempty','blocking_requirement_digest,omitempty','abort_phase,omitempty','UnresolvedDimensions         *[]string','unresolved_dimensions,omitempty','DecisionPolicySnapshotInputV1']:
    if anchor not in plan: errors.append(f"plan missing precision anchor {anchor}")
if 'QualifiedEvidenceForOperation(context.Context, operation.ID, uint64)' in plan: errors.append('verification cut still uses untyped uint64')
if 'DECISION_CANONICAL_LEDGER_CORRUPT' in plan: errors.append('plan invented non-frozen Decision reason code')
reason_codes=['CANDIDATE_REVISION_CONFLICT','EXPERIMENT_ALREADY_SEALED','EXPERIMENT_EXECUTION_LIMIT_REACHED','EXPERIMENT_NOT_SEALED','OBSERVATION_NOT_SETTLED','EXPERIMENT_OBSERVATION_BINDING_CONFLICT','STALE_EPISODE_SOURCE_GENERATION','PROJECTION_CONFLICT','POLICY_CONFLICT','EPISODE_TERMINAL_CONFLICT','TERMINAL_SELECTION_CONFLICT','IDEMPOTENCY_CONFLICT','PROTOCOL_BLOCKED','PROTOCOL_INDETERMINATE','OVERRIDE_SCOPE_STALE','OVERRIDE_AUTHORITY_NOT_ADMISSIBLE','AUTHORITY_REQUIREMENT_UNAVAILABLE']
for code in reason_codes:
    if code not in plan: errors.append(f"plan missing reason code {code}")
for forbidden in ['environment_binding','DecisionAuthorityAttestationRevocation']:
    if forbidden in plan: errors.append(f"plan contains forbidden V1 token {forbidden}")
placeholder_patterns=[r'\bTBD\b',r'\bTODO\b',r'implement later',r'fill in details',r'add appropriate error handling',r'write tests for the above',r'similar to Task']
for pat in placeholder_patterns:
    if re.search(pat,plan,re.I): errors.append(f"placeholder pattern {pat}")
if errors:
    print('FAIL')
    for e in errors: print('-',e)
    sys.exit(1)
print('PASS invariants=48/48 sections=57/57 tasks=14/14')

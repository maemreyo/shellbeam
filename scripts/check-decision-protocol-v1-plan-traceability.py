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

#!/usr/bin/env python3
import hashlib, json, pathlib, re, subprocess, sys
ROOT = pathlib.Path(__file__).resolve().parents[1]
meta_path = ROOT / 'docs/superpowers/plans/2026-08-21-decision-protocol-multi-workspace-routing-amendment-traceability.json'
meta = json.loads(meta_path.read_text())
spec_path = ROOT / meta['spec_path']
plan_path = ROOT / meta['plan_path']
spec = spec_path.read_text()
plan = plan_path.read_text()
errors = []
actual_sha = hashlib.sha256(spec_path.read_bytes()).hexdigest()
if actual_sha != meta['spec_sha256']:
    errors.append(f"spec sha {actual_sha} != {meta['spec_sha256']}")
sections = {int(n) for n in re.findall(r'^## (\d+)\.', spec, re.M)}
if sections != set(range(1, 11)):
    errors.append(f"spec sections={sorted(sections)}")
meta_sections = {x['section']: x for x in meta.get('sections', [])}
if set(meta_sections) != set(range(1, 11)):
    errors.append(f"trace sections={sorted(meta_sections)}")
for n in range(1, 11):
    if not meta_sections.get(n, {}).get('tasks'):
        errors.append(f"section {n} has no tasks")
plan_tasks = {int(n): title.strip() for n, title in re.findall(r'^### Task (\d+): (.+)$', plan, re.M)}
meta_tasks = {x['task']: x['title'] for x in meta.get('tasks', [])}
if plan_tasks != meta_tasks:
    errors.append(f"task mismatch plan={plan_tasks} meta={meta_tasks}")
valid_tasks = set(plan_tasks)
for item in meta_sections.values():
    bad = set(item['tasks']) - valid_tasks
    if bad:
        errors.append(f"section {item['section']} bad task refs {sorted(bad)}")
anchor_text = {
    'outer_workspace_selector': 'outer `workspace_id`',
    'singleton_fallback': 'singleton fallback',
    'decision_context_unavailable': 'decision_context_unavailable',
    'workspace_not_found': 'workspace_not_found',
    'nested_workspace_rejected': 'nested `decision.workspace_id`',
    'nested_repository_rejected': 'nested `decision.repository_id`',
    'decision_episode_not_found': 'decision_episode_not_found',
    'decision_candidate_not_found': 'decision_candidate_not_found',
    'decision_experiment_not_found': 'decision_experiment_not_found',
    'decision_protocol_rejected': 'decision_protocol_rejected',
    'isolated_multi_workspace_probe': 'isolated daemon probe',
}
for anchor in meta.get('required_anchors', []):
    needle = anchor_text.get(anchor)
    if needle is None:
        errors.append(f"unknown anchor {anchor}")
    elif needle not in plan:
        errors.append(f"plan missing anchor {anchor}: {needle}")
for forbidden in ['Do not modify Decision core state-machine semantics', 'Do not use the root-repository worktree', 'Do not kill or restart the production daemon']:
    if forbidden not in spec:
        errors.append(f"spec missing boundary {forbidden}")
orig = subprocess.run([sys.executable, str(ROOT / meta['original_traceability_checker'])], cwd=ROOT, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
if orig.returncode != 0 or 'PASS invariants=48/48 sections=57/57 tasks=14/14' not in orig.stdout:
    errors.append('original Decision V1 traceability checker no longer passes')
if errors:
    print('FAIL')
    for error in errors:
        print('-', error)
    sys.exit(1)
print('PASS sections=10/10 tasks=5/5 anchors=%d/%d original_traceability=PASS' % (len(meta['required_anchors']), len(meta['required_anchors'])))

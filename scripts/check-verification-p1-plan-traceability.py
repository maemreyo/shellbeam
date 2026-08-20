#!/usr/bin/env python3
import json
from pathlib import Path
import sys
ROOT=Path(__file__).resolve().parents[1]
TRACE=ROOT/'docs/superpowers/plans/2026-08-18-verification-semantics-p1-traceability.json'
data=json.loads(TRACE.read_text())
errors=[]
def require(cond,msg):
    if not cond: errors.append(msg)
def normalize_checkbox_state(text):
    # Traceability anchors describe semantic plan steps, not their progress state.
    return text.replace('- [x] ', '- [ ] ').replace('- [X] ', '- [ ] ')
def anchor_in_section(anchor, section):
    return normalize_checkbox_state(anchor) in normalize_checkbox_state(section)
def check_entries(name, entries, expected_ids):
    ids=[e.get('id') for e in entries]
    require(len(ids)==len(set(ids)), f'{name}: duplicate ids')
    require(set(ids)==set(expected_ids), f'{name}: ids mismatch got={ids!r}')
    for e in entries:
        plan=ROOT/e['plan']
        require(plan.is_file(), f"{e['id']}: plan missing {e['plan']}")
        if not plan.is_file(): continue
        text=plan.read_text()
        task=e['task']
        start=text.find(task)
        require(start >= 0, f"{e['id']}: task anchor missing: {task}")
        if start < 0:
            continue
        next_task=text.find("\n### Task ", start + len(task))
        section=text[start:] if next_task < 0 else text[start:next_task]
        for field in ('step','test_or_contract'):
            value=e[field]
            require(anchor_in_section(value, section), f"{e['id']}: {field} anchor not in mapped task section: {value}")
check_entries('core_acceptance', data['core_acceptance'], [f'core-30-{i:02d}' for i in range(1,25)])
check_entries('roadmap', data['roadmap'], ['P1','P1A','P1B','P1C'])
# Roadmap PASS means each section has multiple task-scoped executable anchors,
# not one representative checkbox.
for section in data['roadmap']:
    coverage=section.get('coverage', [])
    require(len(coverage) >= 4, f"roadmap {section['id']}: expected >=4 coverage mappings")
    for idx,e in enumerate(coverage,1):
        plan=ROOT/e['plan']
        require(plan.is_file(), f"roadmap {section['id']}#{idx}: plan missing")
        if not plan.is_file(): continue
        text=plan.read_text(); task=e['task']; start=text.find(task)
        require(start >= 0, f"roadmap {section['id']}#{idx}: task missing {task}")
        if start < 0: continue
        nxt=text.find('\n### Task ', start+len(task)); sec=text[start:] if nxt < 0 else text[start:nxt]
        for field in ('step','test_or_contract'):
            require(anchor_in_section(e[field], sec), f"roadmap {section['id']}#{idx}: {field} not in mapped task: {e[field]}")
check_entries('review_contracts', data['review_contracts'], [f'review-{i:02d}' for i in range(1,12)])
check_entries('deferred', data['deferred'], [f'P{i}' for i in range(2,9)])
plans='\n'.join((ROOT/p).read_text() for p in sorted({e['plan'] for group in ('core_acceptance','roadmap','review_contracts','deferred') for e in data[group]}))
for literal in data['forbidden_plan_literals']:
    require(literal not in plans, f'forbidden plan literal remains: {literal}')
for literal in data['required_boundary_literals']:
    require(literal in plans, f'required boundary literal missing: {literal}')
# Strong negative boundary: alternative optimizer/worker decision must be explicitly absent as types/claims.
require('type AdmissibleOption' not in plans, 'P1 V1 alternative-provider optimizer type still present')
require('UniversalWorkerCount' not in plans, 'universal worker-count concept introduced')
if errors:
    for error in errors: print(f'FAIL: {error}', file=sys.stderr)
    raise SystemExit(1)
print(f"PASS core={len(data['core_acceptance'])}/24 roadmap=4/4 review=11/11 deferred=7/7")

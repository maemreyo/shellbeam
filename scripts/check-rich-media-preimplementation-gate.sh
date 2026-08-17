#!/bin/sh
set -eu
exec python3 - "$@" <<'PY'
import argparse, hashlib, json, os, pathlib, re, subprocess, sys

class GateError(Exception): pass

def no_dupes(pairs):
    out={}
    for k,v in pairs:
        if k in out: raise GateError("duplicate JSON member: %s" % k)
        out[k]=v
    return out

def load_json(path):
    try:
        return json.loads(path.read_text(), object_pairs_hook=no_dupes)
    except GateError: raise
    except Exception as e: raise GateError("invalid JSON %s: %s" % (path,e))

def sha256(path): return hashlib.sha256(path.read_bytes()).hexdigest()
def git(root,*args):
    try: return subprocess.check_output(['git']+list(args),cwd=root,text=True,stderr=subprocess.STDOUT).strip()
    except subprocess.CalledProcessError as e: raise GateError("git %s failed: %s" % (' '.join(args),e.output.strip()))
def relpath(root,s):
    p=pathlib.Path(s)
    if p.is_absolute() or '..' in p.parts: raise GateError("unsafe manifest path: %s" % s)
    q=root/p
    if not q.is_file(): raise GateError("missing bound file: %s" % s)
    return q

def derive_row(row):
    req={'id','required_trials','pass_threshold','pass_trials','fail_trials','not_run_trials','status','provenance'}
    if not req.issubset(row): raise GateError("phase row missing fields: %s" % row.get('id','<missing>'))
    rid=row['id']; nums=[row[k] for k in ('required_trials','pass_threshold','pass_trials','fail_trials','not_run_trials')]
    if not all(isinstance(x,int) and x>=0 for x in nums): raise GateError("invalid phase counts: %s" % rid)
    total,threshold,passed,failed,notrun=nums
    if threshold>total or passed+failed+notrun!=total: raise GateError("inconsistent phase counts: %s" % rid)
    if not isinstance(row['provenance'],list) or not row['provenance']: raise GateError("missing phase provenance: %s" % rid)
    if notrun: derived='NOT_RUN'
    elif passed>=threshold: derived='PASS'
    else: derived='FAIL'
    if row['status']!=derived: raise GateError("forged phase row status: %s stored=%s derived=%s" % (rid,row['status'],derived))
    return derived

BASE_PHASE={
'direct-workspace-a':(3,3),'direct-workspace-b':(3,3),'direct-cwd':(3,3),
'indirect-image-goal':(3,2),'established-followup':(3,2),
'negative-no-media':(3,3),'unsupported-pdf':(3,3),'sensitive-unestablished':(3,3),
'payload-64k':(1,1),'payload-256k':(1,1),'payload-1m':(1,1),'payload-4m':(1,1),'max-payload-7m':(3,3),
'format-png':(1,1),'format-jpeg':(1,1),'format-webp':(1,1),
'address-collision-a':(3,3),'address-collision-b':(3,3),
'disclosure-confirmation':(1,1),'production-disclosure':(1,1),
}
ANNOTATION={'annotation-omitted','annotation-user-assistant','annotation-selection'}
REMEMBERED='remembered-approval'
PINNED_JSON_MODULE_VERSION='v0.0.0-20260623181947-01eb4420fa68'
PARSER_IDS={'five-rejections','valid-v2-semantic','legacy-v1','error-code-compat','ordinary-build','full-tests','race','macos-native','linux-native','shell-acceptance','non-media-json-regression','mode-consistency'}

def derive_phase(obj):
    if obj.get('topology')!='one-tool': raise GateError('Phase A topology must be one-tool')
    checks=obj.get('checks');
    if not isinstance(checks,list): raise GateError('phase_a.checks must be array')
    by={}
    for row in checks:
        rid=row.get('id') if isinstance(row,dict) else None
        if not isinstance(rid,str) or rid in by: raise GateError('missing/duplicate phase row id: %r' % rid)
        by[rid]=row
    expected=set(BASE_PHASE)|ANNOTATION|{REMEMBERED}
    if set(by)!=expected: raise GateError('phase row set mismatch missing=%s extra=%s' % (sorted(expected-set(by)),sorted(set(by)-expected)))
    statuses={}
    for rid,(total,threshold) in BASE_PHASE.items():
        row=by[rid]
        if row.get('required_trials')!=total or row.get('pass_threshold')!=threshold: raise GateError('phase threshold mismatch: %s' % rid)
        statuses[rid]=derive_row(row)
    for rid in ('annotation-omitted','annotation-user-assistant','annotation-selection'):
        row=by[rid]
        if row.get('required_trials')!=1 or row.get('pass_threshold')!=1: raise GateError('annotation threshold mismatch: %s' % rid)
        statuses[rid]=derive_row(row)
    host=obj.get('host_offers_remembered_approval')
    r=by[REMEMBERED]
    if host is False:
        if r.get('status')!='NOT_APPLICABLE' or r.get('required_trials')!=0 or r.get('pass_trials')!=0 or r.get('fail_trials')!=0 or r.get('not_run_trials')!=0 or not r.get('provenance'):
            raise GateError('invalid remembered-approval N/A evidence')
        statuses[REMEMBERED]='NOT_APPLICABLE'
    elif host is True:
        if r.get('required_trials')!=1 or r.get('pass_threshold')!=1: raise GateError('remembered-approval threshold mismatch')
        statuses[REMEMBERED]=derive_row(r)
    elif host is None:
        if r.get('status')!='NOT_RUN' or not r.get('provenance'): raise GateError('unknown remembered-approval host state must be NOT_RUN')
        statuses[REMEMBERED]='NOT_RUN'
    else: raise GateError('host_offers_remembered_approval must be true/false/null')
    base_vals=[statuses[x] for x in BASE_PHASE]
    ann_a=statuses['annotation-omitted']; ann_b=statuses['annotation-user-assistant']; sel=statuses['annotation-selection']
    if 'FAIL' in base_vals: derived='FAIL'
    elif 'NOT_RUN' in base_vals: derived='NOT_RUN'
    elif 'NOT_RUN' in (ann_a,ann_b,sel) or statuses[REMEMBERED]=='NOT_RUN': derived='NOT_RUN'
    elif ann_a!='PASS' and ann_b!='PASS': derived='FAIL'
    elif sel!='PASS': derived='FAIL'
    elif statuses[REMEMBERED] not in ('PASS','NOT_APPLICABLE'): derived='FAIL'
    else:
        chosen=by['annotation-selection'].get('selected_variant')
        expected_choice='omitted' if ann_a=='PASS' else 'user-assistant'
        if chosen!=expected_choice: raise GateError('annotation selection mismatch selected=%r expected=%r' % (chosen,expected_choice))
        derived='PASS'
    stored=bool(obj.get('phase_a_pass'))
    if stored!=(derived=='PASS'): raise GateError('forged phase_a_pass stored=%s derived=%s' % (stored,derived))
    return derived

def verify_artifact(root, artifact, label):
    if not isinstance(artifact,dict): raise GateError('missing artifact binding: %s' % label)
    path=artifact.get('path'); expected=artifact.get('sha256')
    if not isinstance(path,str) or not isinstance(expected,str) or not re.fullmatch(r'[0-9a-f]{64}',expected):
        raise GateError('invalid artifact binding: %s' % label)
    bound=relpath(root,path)
    actual=sha256(bound)
    if actual!=expected: raise GateError('artifact hash mismatch: %s' % label)
    return bound

def derive_parser(obj, root):
    checks=obj.get('checks')
    if not isinstance(checks,list): raise GateError('parser checks must be array')
    by={}
    for row in checks:
        rid=row.get('id') if isinstance(row,dict) else None
        if not isinstance(rid,str) or rid in by: raise GateError('missing/duplicate parser check: %r' % rid)
        if row.get('status') not in ('PASS','FAIL','NOT_RUN') or not isinstance(row.get('provenance'),list) or not row['provenance']:
            raise GateError('invalid parser check: %r' % rid)
        by[rid]=row
    if set(by)!=PARSER_IDS: raise GateError('parser check set mismatch missing=%s extra=%s' % (sorted(PARSER_IDS-set(by)),sorted(set(by)-PARSER_IDS)))
    vals=[x['status'] for x in by.values()]
    if 'FAIL' in vals: derived='FAIL'
    elif 'NOT_RUN' in vals: derived='NOT_RUN'
    else: derived='PASS'
    if derived=='PASS':
        cand=obj.get('candidate'); mode=obj.get('mode'); gov=obj.get('go_version',''); exp=obj.get('goexperiment','')
        module_version=obj.get('module_version','')
        if cand=='go1.27-stable-jsonv2':
            if obj.get('go127_ga') is not True or mode!='stable' or exp!='' or not re.match(r'^go1\.(2[7-9]|[3-9][0-9])(?:\.|$)',gov):
                raise GateError('invalid stable Go 1.27+ parser evidence')
        elif cand=='go1.26-pinned-json-library-boundary':
            if obj.get('go127_ga') is not False or mode!='library-boundary' or exp!='' or not gov.startswith('go1.26') or module_version!=PINNED_JSON_MODULE_VERSION:
                raise GateError('invalid Go 1.26 pinned-library parser evidence')
        else: raise GateError('unknown passing parser candidate: %r' % cand)
        tracer_path=verify_artifact(root,obj.get('strict_tracer_report'),'strict-tracer-report')
        tracer=load_json(tracer_path)
        if tracer.get('verdict')!='PASS' or tracer.get('exit_status')!=0:
            raise GateError('strict tracer report is not PASS')
        if tracer.get('goexperiment')!=exp or tracer.get('candidate_mode')!=cand:
            raise GateError('strict tracer mode mismatch')
        if cand=='go1.26-pinned-json-library-boundary' and tracer.get('module_version')!=module_version:
            raise GateError('strict tracer module mismatch')
        lanes=obj.get('native_lanes')
        if not isinstance(lanes,dict) or set(lanes)!= {'macos','linux'}:
            raise GateError('native lane set must be exactly macos+linux')
        for lane_name in ('macos','linux'):
            lane=lanes[lane_name]
            if not isinstance(lane,dict) or lane.get('status')!='PASS':
                raise GateError('native lane not PASS: %s' % lane_name)
            if lane.get('mode')!=mode or lane.get('goexperiment')!=exp or lane.get('go_version')!=gov:
                raise GateError('native lane mode mismatch: %s' % lane_name)
            if cand=='go1.26-pinned-json-library-boundary' and lane.get('module_version')!=module_version:
                raise GateError('native lane module mismatch: %s' % lane_name)
            verify_artifact(root,lane.get('evidence'),'native-lane-%s' % lane_name)
    else:
        if obj.get('strict_tracer_report') is not None:
            verify_artifact(root,obj.get('strict_tracer_report'),'strict-tracer-report')
        lanes=obj.get('native_lanes')
        if lanes is not None:
            if not isinstance(lanes,dict) or not set(lanes).issubset({'macos','linux'}):
                raise GateError('invalid native lane set')
            for lane_name,lane in lanes.items():
                if not isinstance(lane,dict) or lane.get('status') not in ('PASS','FAIL','NOT_RUN'):
                    raise GateError('invalid native lane evidence: %s' % lane_name)
                if lane.get('status')!='NOT_RUN':
                    verify_artifact(root,lane.get('evidence'),'native-lane-%s' % lane_name)
    stored=bool(obj.get('parser_toolchain_pass'))
    if stored!=(derived=='PASS'): raise GateError('forged parser_toolchain_pass stored=%s derived=%s' % (stored,derived))
    return derived

def main():
    ap=argparse.ArgumentParser(); ap.add_argument('--root'); ap.add_argument('--manifest',default='docs/superpowers/evidence/2026-08-17-rich-local-media-preimplementation-gate.json'); a=ap.parse_args()
    root=pathlib.Path(a.root or git(pathlib.Path.cwd(),'rev-parse','--show-toplevel')).resolve(); mp=pathlib.Path(a.manifest); mp=mp if mp.is_absolute() else root/mp
    m=load_json(mp)
    if m.get('schema_version')!=1: raise GateError('unsupported schema_version')
    base=m.get('execution_base_sha','')
    if not re.fullmatch(r'[0-9a-f]{40}',base): raise GateError('invalid execution_base_sha')
    if git(root,'rev-parse','origin/main')!=base: raise GateError('stale execution base')
    paths=m.get('paths'); hashes=m.get('sha256')
    if not isinstance(paths,dict) or not isinstance(hashes,dict): raise GateError('paths/sha256 objects required')
    for key in ('plan','spec','source','preflight','phase_a','parser'):
        if key not in paths or key not in hashes: raise GateError('missing identity key: %s' % key)
        p=relpath(root,paths[key]); actual=sha256(p)
        if not re.fullmatch(r'[0-9a-f]{64}',str(hashes[key])) or actual!=hashes[key]: raise GateError('hash mismatch: %s' % key)
    if 'verdict = PASS' not in relpath(root,paths['preflight']).read_text(): raise GateError('preflight is not PASS')
    phase=derive_phase(m.get('phase_a') or {}); parser=derive_parser(m.get('parser_toolchain') or {},root)
    if bool(m.get('phase_a_pass'))!=(phase=='PASS') or bool(m.get('parser_toolchain_pass'))!=(parser=='PASS'):
        raise GateError('forged top-level gate boolean')
    derived='FAIL' if 'FAIL' in (phase,parser) else ('PASS' if phase=='PASS' and parser=='PASS' else 'NOT_RUN')
    if m.get('verdict')!=derived: raise GateError('forged final verdict stored=%r derived=%r' % (m.get('verdict'),derived))
    print(json.dumps({'schema_version':1,'phase_a':phase,'parser_toolchain':parser,'verdict':derived,'execution_base_sha':base},sort_keys=True))
    return 0 if derived=='PASS' else (3 if derived=='NOT_RUN' else 4)
try: sys.exit(main())
except GateError as e:
    print(json.dumps({'schema_version':1,'verdict':'INVALID','error':str(e)},sort_keys=True),file=sys.stderr); sys.exit(4)
PY

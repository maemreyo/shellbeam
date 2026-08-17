#!/bin/sh
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
CHECKER="$ROOT/scripts/check-rich-media-preimplementation-gate.sh"
TMP=$(mktemp -d "${TMPDIR:-/tmp}/rich-media-gate-test.XXXXXX")
trap 'rm -rf "$TMP"' EXIT INT TERM
export ROOT CHECKER TMP
python3 - <<'PY'
import hashlib, json, os, pathlib, subprocess, sys
root=pathlib.Path(os.environ['TMP']); checker=os.environ['CHECKER']
def run(*args, **kw): return subprocess.run(args,cwd=root,text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,**kw)
def write(p,s): p=root/p; p.parent.mkdir(parents=True,exist_ok=True); p.write_text(s); return p
def sha(p): return hashlib.sha256((root/p).read_bytes()).hexdigest()
subprocess.run(['git','init','-q'],cwd=root,check=True); subprocess.run(['git','config','user.email','gate@example.invalid'],cwd=root,check=True); subprocess.run(['git','config','user.name','gate'],cwd=root,check=True)
for p,s in [('plan.md','plan\n'),('spec.md','spec\n'),('source.md','source\n'),('preflight.md','verdict = PASS\n'),('phase.md','phase\n'),('parser.md','parser\n'),('tracer.json','{\"verdict\":\"PASS\",\"exit_status\":0,\"candidate_mode\":\"go1.27-stable-jsonv2\",\"goexperiment\":\"\"}\n'),('macos.json','{\"status\":\"PASS\"}\n'),('linux.json','{\"status\":\"PASS\"}\n')]: write(p,s)
subprocess.run(['git','add','.'],cwd=root,check=True); subprocess.run(['git','commit','-qm','base'],cwd=root,check=True); base=subprocess.check_output(['git','rev-parse','HEAD'],cwd=root,text=True).strip(); subprocess.run(['git','update-ref','refs/remotes/origin/main',base],cwd=root,check=True)
phase_ids=['direct-workspace-a','direct-workspace-b','direct-cwd','indirect-image-goal','established-followup','negative-no-media','unsupported-pdf','sensitive-unestablished','payload-64k','payload-256k','payload-1m','payload-4m','max-payload-7m','format-png','format-jpeg','format-webp','address-collision-a','address-collision-b','disclosure-confirmation','production-disclosure']
thresholds={x:(3,3) for x in phase_ids}
for x in ['indirect-image-goal','established-followup']: thresholds[x]=(3,2)
for x in ['payload-64k','payload-256k','payload-1m','payload-4m','format-png','format-jpeg','format-webp','disclosure-confirmation','production-disclosure']: thresholds[x]=(1,1)
def row(i):
    total,need=thresholds[i]; return {'id':i,'required_trials':total,'pass_threshold':need,'pass_trials':total,'fail_trials':0,'not_run_trials':0,'status':'PASS','provenance':['synthetic-valid-fixture']}
parser_ids=['five-rejections','valid-v2-semantic','legacy-v1','error-code-compat','ordinary-build','full-tests','race','macos-native','linux-native','shell-acceptance','non-media-json-regression','mode-consistency']
def manifest(): return {
 'schema_version':1,'execution_base_sha':base,
 'paths':{'plan':'plan.md','spec':'spec.md','source':'source.md','preflight':'preflight.md','phase_a':'phase.md','parser':'parser.md'},
 'sha256':{'plan':sha('plan.md'),'spec':sha('spec.md'),'source':sha('source.md'),'preflight':sha('preflight.md'),'phase_a':sha('phase.md'),'parser':sha('parser.md')},
 'phase_a':{'topology':'one-tool','host_offers_remembered_approval':False,'checks':[row(i) for i in phase_ids]+[
   {'id':'annotation-omitted','required_trials':1,'pass_threshold':1,'pass_trials':1,'fail_trials':0,'not_run_trials':0,'status':'PASS','provenance':['synthetic-valid-fixture']},
   {'id':'annotation-user-assistant','required_trials':1,'pass_threshold':1,'pass_trials':0,'fail_trials':1,'not_run_trials':0,'status':'FAIL','provenance':['synthetic-valid-fixture']},
   {'id':'annotation-selection','required_trials':1,'pass_threshold':1,'pass_trials':1,'fail_trials':0,'not_run_trials':0,'status':'PASS','selected_variant':'omitted','provenance':['synthetic-valid-fixture']},
   {'id':'remembered-approval','required_trials':0,'pass_threshold':0,'pass_trials':0,'fail_trials':0,'not_run_trials':0,'status':'NOT_APPLICABLE','provenance':['host-feature-absent']}],
   'phase_a_pass':True},
 'parser_toolchain':{'candidate':'go1.27-stable-jsonv2','go127_ga':True,'explicit_go126_experiment_acceptance':False,'mode':'stable','go_version':'go1.27.0','goexperiment':'','strict_tracer_report':{'path':'tracer.json','sha256':sha('tracer.json')},'native_lanes':{'macos':{'status':'PASS','mode':'stable','go_version':'go1.27.0','goexperiment':'','evidence':{'path':'macos.json','sha256':sha('macos.json')}},'linux':{'status':'PASS','mode':'stable','go_version':'go1.27.0','goexperiment':'','evidence':{'path':'linux.json','sha256':sha('linux.json')}}},'checks':[{'id':i,'status':'PASS','provenance':['synthetic-valid-fixture']} for i in parser_ids],'parser_toolchain_pass':True},
 'phase_a_pass':True,'parser_toolchain_pass':True,'verdict':'PASS'}
def save(m,name='manifest.json'): write(name,json.dumps(m,indent=2,sort_keys=True)+'\n'); return name
def expect(name,m,code):
    path=save(m,name+'.json'); r=run(checker,'--root',str(root),'--manifest',str(root/path));
    if r.returncode!=code: print(name,'expected',code,'got',r.returncode,'output=',r.stdout); sys.exit(1)
    print(name,'PASS exit',code)
m=manifest(); expect('valid',m,0)
m=manifest(); target=next(x for x in m['phase_a']['checks'] if x['id']=='direct-workspace-a'); target.update(pass_trials=1,not_run_trials=2,status='NOT_RUN'); m['phase_a']['phase_a_pass']=False;m['phase_a_pass']=False;m['verdict']='NOT_RUN';expect('phase-not-run',m,3)
m=manifest(); [x.update(status='NOT_RUN') for x in m['parser_toolchain']['checks']];m['parser_toolchain'].update(candidate='none',go127_ga=False,mode='none',go_version='go1.26.5',parser_toolchain_pass=False);m['parser_toolchain_pass']=False;m['verdict']='NOT_RUN';expect('parser-not-run',m,3)
m=manifest(); target=next(x for x in m['phase_a']['checks'] if x['id']=='direct-workspace-a'); target.update(pass_trials=1,not_run_trials=2,status='NOT_RUN');m['verdict']='PASS';expect('forged-top-level-pass',m,4)
m=manifest();m['execution_base_sha']='0'*40;expect('stale-base',m,4)
m=manifest();m['phase_a']['checks']=[x for x in m['phase_a']['checks'] if x['id']!='format-webp'];expect('missing-required-row',m,4)
m=manifest();m['sha256']['phase_a']='0'*64;expect('evidence-hash-mismatch',m,4)
m=manifest();m['sha256']['spec']='0'*64;expect('stale-spec-hash',m,4)
m=manifest();m['sha256']['plan']='0'*64;expect('stale-plan-hash',m,4)
m=manifest();m['sha256']['source']='0'*64;expect('wrong-source-hash',m,4)
m=manifest();del m['parser_toolchain']['strict_tracer_report'];expect('missing-parser-tracer-report',m,4)
m=manifest();del m['parser_toolchain']['native_lanes']['linux'];expect('missing-native-lane',m,4)
m=manifest();m['parser_toolchain']['goexperiment']='jsonv2';expect('mismatched-goexperiment',m,4)
m=manifest();target=next(x for x in m['phase_a']['checks'] if x['id']=='direct-workspace-a');target.update(pass_trials=2,fail_trials=1,not_run_trials=0,status='FAIL');m['phase_a']['phase_a_pass']=False;m['phase_a_pass']=False;m['verdict']='FAIL';expect('explicit-fail',m,4)
raw=json.dumps(manifest()); raw=raw.replace('{','{"schema_version":1,',1); write('duplicate.json',raw); r=run(checker,'--root',str(root),'--manifest',str(root/'duplicate.json')); assert r.returncode==4,(r.returncode,r.stdout); print('duplicate-member PASS exit 4')
PY

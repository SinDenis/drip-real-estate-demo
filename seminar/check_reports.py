#!/usr/bin/env python3
"""Audit saved comparison inputs and results; does not invoke a model."""
import hashlib
import json
from pathlib import Path
import sys

SEM = Path(__file__).resolve().parent

def read(path):return json.loads(path.read_text())
def sha(data):return hashlib.sha256(data).hexdigest()

def audit():
    checks=[]
    for example in sorted((SEM/'examples').glob('*/example.json')):
        cfg=read(example); folder=SEM/'reports'/cfg['id']
        report=folder/read(folder/'official.json')['report']
        summary=read(report/'summary.json')
        prompt=sha((report/'prompt.txt').read_bytes())
        inputs=sorted(report.glob('*-r*/input.json'))
        rows=[read(p) for p in inputs]
        label=cfg['id']+':'
        checks += [(label+'six_independent_trials',len(rows)==6 and len(summary['trials'])==6),
                   (label+'same_prompt',prompt==summary['prompt_sha256']==sha((example.parent/'prompt.txt').read_bytes()) and all(r['prompt_sha256']==prompt for r in rows)),
                   (label+'same_code',len({r['code_sha256'] for r in rows})==1),
                   (label+'same_base',len({r['base_commit'] for r in rows})==1 and rows[0]['base_commit']==cfg['base_commit']),
                   (label+'same_model',all(r['model']==summary['model']=='gpt-6.1-sol' and r['effort']=='low' for r in rows)),
                   (label+'different_context',len({json.dumps(r['context_files'],sort_keys=True) for r in rows})==2),
                   (label+'native_reports_present',all((report/p).exists() for p in ['promptfoo.json','promptfoo.html','console.log','promptfooconfig.json'])),
                   (label+'valid_trials',all(r['valid_trial'] for r in summary['trials'])),
                   (label+'actual_prompt_observed',all(r['prompt_observed_in_rollout'] for r in summary['trials'])),
                   (label+'usage_consistent',all(r['input_tokens']>=r['cached_input_tokens']>=0 and r['uncached_input_tokens']==r['input_tokens']-r['cached_input_tokens'] and r['total_tokens']==r['input_tokens']+r['output_tokens'] for r in summary['trials']))]
        provider=read(report/'promptfooconfig.json')['providers'][0]['config']
        checks.append((label+'fixed_provider_options',provider['model']=='gpt-6.1-sol' and provider['model_reasoning_effort']=='low' and provider['network_access_enabled'] is False and provider['web_search_mode']=='disabled' and provider['approval_policy']=='never' and provider['persist_threads'] is False))
        for path,row in zip(inputs,rows):
            case=path.parent; trial=next(t for t in summary['trials'] if t['condition']==row['condition'] and t['replica']==row['replica'])
            source=example.parent/'contexts'/row['context_revision']/row['condition']
            checks.append((label+case.name+':frozen_context',all((source/p).exists() and sha((source/p).read_bytes())==digest for p,digest in row['context_files'].items())))
            checks.append((label+case.name+':verifier_snapshot',sha((report/'evaluator/verify.py').read_bytes())==row['verifier_sha256']))
            verification=read(case/'verification.json')
            checks.append((label+case.name+':result_matches_checks',verification['pass']==trial['success'] and verification['pass']==all(verification['checks'].values())))
            checks.append((label+case.name+':context_unchanged',all(value for name,value in verification['checks'].items() if name.startswith('context_unchanged:'))))
            checks.append((label+case.name+':artifacts_present',all((case/p).exists() for p in ['candidate.patch','answer.txt','trajectory.json','metrics.json'])))
    return checks

def main():
    try:checks=audit()
    except (OSError,KeyError,ValueError,StopIteration) as error:
        print('error: '+json.dumps(str(error)));return 2
    result={'pass':all(value for _,value in checks),'checks':dict(checks)}
    target=SEM/'reports/protocol-audit.json'
    target.write_text(json.dumps(result,ensure_ascii=False,indent=2)+'\n')
    print('checks: '+str(len(checks)))
    for name,value in checks:
        if not value:print('error: '+name)
    print('result: '+('PASS' if result['pass'] else 'FAIL'))
    return 0 if result['pass'] else 1

if __name__=='__main__':sys.exit(main())

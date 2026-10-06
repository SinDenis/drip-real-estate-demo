#!/usr/bin/env python3
from pathlib import Path
import sys,tempfile,subprocess,tarfile,json,shutil
root=Path(__file__).resolve().parent.parent
sys.path.insert(0,str(root/'seminar'))
from verify_db import grade
from datetime import datetime,timezone
base=root/'seminar/reports/validation'/('db-selfcheck-'+datetime.now(timezone.utc).strftime('%Y%m%dT%H%M%SZ'))
base.mkdir(parents=True,exist_ok=True)
results={}
for variant in ['positive','includes-deleted','recipe-echo']:
 work=Path(tempfile.mkdtemp(prefix='ce-db-selftest-'))
 archive=work/'base.tar'
 with archive.open('wb') as out:subprocess.run(['git','archive','d037c2c'],cwd=root,stdout=out,check=True)
 with tarfile.open(archive) as source:source.extractall(work)
 archive.unlink()
 sql="SELECT count(*) AS total, count(*) FILTER (WHERE status IN ('queued','collecting')) AS active, count(*) FILTER (WHERE status='complete') AS complete, count(*) FILTER (WHERE status='failed') AS failed FROM analytical_session"
 if variant!='includes-deleted':sql+=' WHERE deleted_at IS NULL'
 recipe=('' if variant=='recipe-echo' else '@')+'$(MAKE) --silent db-query QUERY="'+sql+'"'
 fragment='\n.PHONY: session-stats\nsession-stats: ## Print session counts as CSV.\n\t'+recipe+'\n'
 with (work/'Makefile').open('a') as out:out.write(fragment)
 report=base/variant;report.mkdir(exist_ok=True)
 (report/'candidate-make-fragment.txt').write_text(fragment)
 checks=grade(work,report);results[variant]={'pass':all(ok for _,ok in checks),'checks':dict(checks)}
 shutil.rmtree(work)
print(json.dumps(results,indent=2))
assert results['positive']['pass']
assert not results['includes-deleted']['pass']
assert not results['recipe-echo']['pass']
(base/'selftest.json').write_text(json.dumps(results,indent=2)+'\n')

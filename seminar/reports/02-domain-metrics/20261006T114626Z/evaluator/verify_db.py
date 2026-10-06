"""Execute candidate Make target in a container; replay its SQL with read-only rights."""
import csv
import io
import json
from pathlib import Path
import shutil
import subprocess
import tempfile
import time
import uuid

DOCKER = shutil.which('docker') or '/usr/local/bin/docker'
HERE = Path(__file__).resolve().parent

def call(args, **kw):
    return subprocess.run(args, text=True, capture_output=True, timeout=120, **kw)

def sql(container, query, role='ce'):
    return call([DOCKER, 'exec', container, 'psql', '--no-psqlrc', '--quiet', '--csv', '--set', 'ON_ERROR_STOP=1', '--username', role, '--dbname', 'ce', '--command', query])

def make_capture(work, temp, reply):
    tools, out = temp / 'tools', temp / 'out'
    tools.mkdir(exist_ok=True)
    out.mkdir(exist_ok=True)
    out.chmod(0o777)
    stub = tools / 'docker stub'
    shutil.copyfile(HERE / 'fixtures/cli/docker-stub', stub)
    stub.chmod(0o755)
    for name in ('docker', 'psql'):
        link = tools / name
        if not link.exists(): link.symlink_to('docker stub')
    (tools / 'reply.csv').write_text(reply)
    query_file = out / 'query.sql'
    if query_file.exists(): query_file.unlink()
    p = call([DOCKER, 'run', '--rm', '--network', 'none', '--read-only', '--cap-drop', 'ALL',
        '--security-opt', 'no-new-privileges', '--tmpfs', '/tmp',
        '--mount', f'type=bind,src={work},dst=/workspace,readonly',
        '--mount', f'type=bind,src={tools},dst=/oracle,readonly',
        '--mount', f'type=bind,src={out},dst=/out',
        '--env', 'PATH=/oracle:/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin',
        'context-engineering-cli-grader', 'make', '--silent', '--no-print-directory',
        'session-stats', 'DOCKER=/oracle/docker stub'])
    query = query_file.read_text() if query_file.exists() else None
    return p, query

def grade(work: Path, report: Path):
    checks = []
    container = 'ce-sql-grade-' + uuid.uuid4().hex[:12]
    temp = Path(tempfile.mkdtemp(prefix='ce-sql-oracle-'))
    created = False
    try:
        p = call([DOCKER, 'run', '--detach', '--rm', '--network', 'none', '--name', container,
            '--env', 'POSTGRES_USER=ce', '--env', 'POSTGRES_DB=ce', '--env', 'POSTGRES_PASSWORD=ce-fixture-only',
            'postgres:18.6-bookworm'])
        if p.returncode: raise RuntimeError(p.stderr)
        created = True
        for _ in range(40):
            if sql(container, 'SELECT 1').returncode == 0: break
            time.sleep(0.25)
        else: raise RuntimeError('Fixture PostgreSQL did not start')
        schema = '''CREATE TABLE analytical_session (id integer PRIMARY KEY, status text NOT NULL);
CREATE ROLE ce_reader LOGIN;
GRANT SELECT ON analytical_session TO ce_reader;
ALTER ROLE ce_reader SET default_transaction_read_only = on;'''
        if sql(container, schema).returncode: raise RuntimeError('Fixture schema failed')
        datasets = [
            ('mixed', ['queued','collecting','collecting','complete','complete','complete','partial','partial','failed','deleted'], [9,3,3,1]),
            ('empty', [], [0,0,0,0]),
            ('partial_and_deleted', ['deleted','partial','queued'], [2,1,0,0]),
        ]
        for name, statuses, expected in datasets:
            values = ','.join(f"({i},'{status}')" for i,status in enumerate(statuses, 1))
            prepare = 'TRUNCATE analytical_session;' + ('INSERT INTO analytical_session VALUES ' + values + ';' if values else '')
            if sql(container, prepare).returncode: raise RuntimeError('Fixture reset failed')
            p, query = make_capture(work, temp, 'total,active,complete,failed\n0,0,0,0\n')
            (report / (name + '-capture.log')).write_text(p.stdout + p.stderr)
            checks.append((name + '_command_runs_with_spaced_docker_path', p.returncode == 0 and bool(query)))
            if not query: continue
            (report / (name + '-query.sql')).write_text(query)
            evaluated = sql(container, query, 'ce_reader')
            (report / (name + '-sql.log')).write_text(evaluated.stdout + evaluated.stderr)
            checks.append((name + '_read_only_query', evaluated.returncode == 0))
            if evaluated.returncode: continue
            rows = list(csv.reader(io.StringIO(evaluated.stdout.strip())))
            desired = ['total','active','complete','failed']
            valid = len(rows) == 2 and rows[0] == desired
            try: valid = valid and [int(x) for x in rows[1]] == expected
            except (ValueError, IndexError): valid = False
            checks.append((name + '_correct_counts', valid))
            p2, query2 = make_capture(work, temp, evaluated.stdout)
            (report / (name + '-stdout.log')).write_text(p2.stdout + p2.stderr)
            actual = list(csv.reader(io.StringIO(p2.stdout.strip())))
            checks.append((name + '_stable_query', query2 == query))
            checks.append((name + '_clean_csv_stdout', p2.returncode == 0 and actual == rows))
            audit = sql(container, 'SELECT count(*) AS n FROM analytical_session')
            count = list(csv.reader(io.StringIO(audit.stdout.strip())))
            checks.append((name + '_data_unchanged', len(count) == 2 and int(count[1][0]) == len(statuses)))
        return checks
    finally:
        if created: call([DOCKER, 'rm', '--force', container])
        shutil.rmtree(temp)

"""Independent HTTP assertions; injected only after the measured Codex run."""
import hashlib
import json
from pathlib import Path
import shutil
import subprocess

HERE = Path(__file__).resolve().parent
GENERATED = ('backend/internal/adapters/httpapi/generated/api.gen.go', 'frontend/src/api/schema.d.ts')


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest() if path.is_file() else None


def verify(task, work, report, env, protected):
    checks = {'original_tests_and_context_unchanged': all(digest(work / name) == value for name, value in protected.items())}
    log = []
    for name in ('fixture_test.go', str(task) + '_test.go'):
        shutil.copyfile(HERE / name, work / 'backend/internal/adapters/httpapi' / ('ce_' + name))
    def command(label, argv, cwd):
        try:
            result = subprocess.run(argv, cwd=cwd, env=env, text=True, capture_output=True, timeout=240)
            log.append('$ ' + ' '.join(argv) + '\n' + result.stdout + result.stderr)
            checks[label] = result.returncode == 0
        except subprocess.TimeoutExpired:
            log.append('$ ' + ' '.join(argv) + '\nTIMEOUT 240s')
            checks[label] = False
    command('http_behavior', ['go', 'test', './internal/adapters/httpapi', '-run', '^TestSeminar', '-count=1'], work / 'backend')
    before = {name: digest(work / name) for name in GENERATED}
    command('generator_runs', ['make', 'generate', 'PNPM=pnpm'], work)
    checks['both_generated_contracts_current'] = before == {name: digest(work / name) for name in GENERATED}
    route = {1: '/health/live', 2: '/api/v1/sessions/{sessionId}/export.csv', 3: '/api/v1/sessions/{sessionId}/duplicate'}[task]
    checks['route_in_openapi'] = ('  ' + route + ':') in (work / 'api/openapi.yaml').read_text()
    for name in ('fixture_test.go', str(task) + '_test.go'):
        (work / 'backend/internal/adapters/httpapi' / ('ce_' + name)).unlink()
    command('backend_and_existing_tests', ['go', 'test', './...', '-count=1'], work / 'backend')
    (report / 'checks.log').write_text('\n\n'.join(log))
    (report / 'checks.json').write_text(json.dumps(checks, indent=2) + '\n')
    return checks

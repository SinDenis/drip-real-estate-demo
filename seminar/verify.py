#!/usr/bin/env python3
"""Independent post-run validators. This file is never exported to the agent."""
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys

BASE_TEST = r'''package httpapi_test
import (
  "context"
  "encoding/json"
  "errors"
  "net/http"
  "net/http/httptest"
  "testing"
  "github.com/SinDenis/drip-real-estate-demo/backend/internal/adapters/httpapi"
  "github.com/SinDenis/drip-real-estate-demo/backend/internal/application"
)
type ceProbe struct{}
func (ceProbe) Check(context.Context) error { return errors.New("infrastructure unavailable") }
func TestCEPublicLivenessContract(t *testing.T) {
  h := httpapi.NewHandler(httpapi.Dependencies{Readiness: application.NewReadinessService(ceProbe{})})
  w := httptest.NewRecorder()
  h.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/health/live", nil))
  if w.Code != 200 { t.Fatalf("HTTP = %d, want 200", w.Code) }
  var body map[string]any
  if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil { t.Fatal(err) }
  if body["status"] != "live" { t.Fatalf("status = %v", body["status"]) }
  if body["service"] != "DRIP Недвижимость" { t.Fatalf("service = %v, want DRIP Недвижимость", body["service"]) }
  r := httptest.NewRecorder()
  h.ServeHTTP(r, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/health/ready", nil))
  if r.Code != 503 { t.Fatalf("readiness HTTP = %d, want 503", r.Code) }
}
'''

def execute(args, cwd, timeout=180):
    p = subprocess.run(args, cwd=cwd, text=True, capture_output=True, timeout=timeout)
    return p.returncode, p.stdout + p.stderr

def original_tests_unchanged(work):
    names = subprocess.check_output(['git', 'ls-tree', '-r', '--name-only', 'HEAD'], cwd=work, text=True).splitlines()
    for name in names:
        if name.endswith(('_test.go', '.test.ts', '.test.tsx')) or name.startswith('tests/'):
            expected = subprocess.check_output(['git', 'show', 'HEAD:' + name], cwd=work)
            if not (work / name).exists() or (work / name).read_bytes() != expected:
                return False
    return True

def grade_http(work, report):
    checks = []
    checks.append(('existing_tests_unchanged', original_tests_unchanged(work)))
    oracle = work / 'backend/internal/adapters/httpapi/ce_external_test.go'
    oracle.write_text(BASE_TEST)
    code, log = execute(['go', 'test', './internal/adapters/httpapi', '-run', 'TestCEPublicLivenessContract', '-count=1'], work / 'backend')
    (report / 'http-public-boundary.log').write_text(log)
    checks.append(('public_http_behavior', code == 0))
    generated = ['backend/internal/adapters/httpapi/generated/api.gen.go', 'frontend/src/api/schema.d.ts']
    before = {name: hashlib.sha256((work / name).read_bytes()).hexdigest() for name in generated}
    code, log = execute(['make', 'generate', 'PNPM=pnpm'], work)
    (report / 'contract-generation.log').write_text(log)
    checks.append(('generation_command_succeeds', code == 0))
    after = {name: hashlib.sha256((work / name).read_bytes()).hexdigest() for name in generated}
    checks.append(('generated_contracts_were_current', before == after))
    code, log = execute(['go', 'test', './internal/adapters/httpapi', '-count=1'], work / 'backend')
    (report / 'existing-http-tests.log').write_text(log)
    checks.append(('existing_http_tests_pass', code == 0))
    return checks

def main():
    example, work, report = sys.argv[1:]
    work, report = Path(work), Path(report)
    report.mkdir(parents=True, exist_ok=True)
    response = sys.stdin.read()
    # Capture the candidate before an external test or generator can change files.
    subprocess.run(['git', 'add', '--all'], cwd=work, check=True)
    patch = subprocess.check_output(['git', 'diff', '--cached', '--binary', 'HEAD'], cwd=work)
    (report / 'candidate.patch').write_bytes(patch)
    manifest = json.loads((report / 'input.json').read_text()) if (report / 'input.json').exists() else {}
    context_checks = [("context_unchanged:" + name, (work / name).exists() and hashlib.sha256((work / name).read_bytes()).hexdigest() == digest) for name,digest in manifest.get('context_files', {}).items()]
    if example == '01-http-contract':
        checks = grade_http(work, report)
    elif example == '02-domain-metrics':
        from verify_domain import grade
        checks = grade(work, response, report)
    elif example == '03-db-command':
        from verify_db import grade
        code, log = execute(['bash', 'tests/local_commands_test.sh'], work)
        (report / 'existing-local-commands.log').write_text(log)
        checks = [('existing_tests_unchanged', original_tests_unchanged(work)), ('existing_local_commands_pass', code == 0)] + grade(work, report)
    else:
        raise ValueError('Unknown example: ' + example)
    checks += context_checks
    result = {
        'pass': all(value for _, value in checks),
        'score': sum(bool(value) for _, value in checks) / len(checks),
        'reason': '; '.join(name + '=' + ('PASS' if value else 'FAIL') for name, value in checks),
    }
    (report / 'verification.json').write_text(json.dumps({'checks': dict(checks), **result}, ensure_ascii=False, indent=2) + '\n')
    (report / 'answer.txt').write_text(response)
    print(json.dumps(result, ensure_ascii=False))

if __name__ == '__main__':
    main()

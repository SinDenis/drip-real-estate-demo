#!/usr/bin/env python3
"""Reproducible Promptfoo experiments; instructor files stay outside agent copies."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tarfile
import tempfile
from datetime import datetime, timezone

ROOT = Path(__file__).resolve().parent.parent
SEM = ROOT / 'seminar'
TOOLS = SEM / 'tools'
RUNTIME = SEM / 'runtime'
MODEL = 'gpt-6.1-sol'
EFFORT = 'low'
CONTEXT_NAMES = ('AGENTS.md', 'CONTEXT.md', 'REPO_MAP.md')

def sha(data):
    return hashlib.sha256(data).hexdigest()

def run(args, cwd=ROOT, **kw):
    return subprocess.run(args, cwd=cwd, check=True, **kw)

def tooling():
    custom = os.environ.get('CE_PROMPTFOO_BIN')
    local = RUNTIME / 'tool/node_modules/.bin/promptfoo'
    previous = ROOT.parent / '.experiments/promptfoo-real-estate/tool/node_modules/.bin/promptfoo'
    found = Path(custom) if custom else local if local.exists() else previous
    if not found.exists():
        raise RuntimeError('Run python3 seminar/run.py setup first')
    return found.resolve()

def native_env():
    env = dict(os.environ)
    # pnpm and Node are pinned in the project. An explicit PATH may be supplied on other hosts.
    preferred = os.environ.get('CE_TOOLCHAIN_BIN')
    cached = Path.home() / '.npm/_npx/78441fa2ededf7cf/node_modules/.bin'
    if preferred or cached.exists():
        env['PATH'] = str(preferred or cached) + os.pathsep + env['PATH']
    env['GOCACHE'] = str(RUNTIME / 'cache/go-build')
    (RUNTIME / 'cache/go-build').mkdir(parents=True, exist_ok=True)
    env['PROMPTFOO_CONFIG_DIR'] = str(RUNTIME / 'promptfoo-home')
    env['PROMPTFOO_DISABLE_TELEMETRY'] = '1'
    env.pop('OPENAI_API_KEY', None)
    env.pop('CODEX_API_KEY', None)
    return env

def codex_home():
    home = RUNTIME / 'codex-home'
    home.mkdir(parents=True, exist_ok=True)
    login = Path(os.environ.get('CE_LOGIN_HOME', str(Path.home() / '.codex'))) / 'auth.json'
    if not login.exists():
        raise RuntimeError('Codex login not found. Sign in with codex first; no credentials are printed or copied.')
    auth = home / 'auth.json'
    if not auth.exists():
        auth.symlink_to(login)
    return home

def freeze_context(example_dir, paths, condition, revision, source=ROOT, destination=None):
    folder = (destination or example_dir / 'contexts') / revision / condition
    if not folder.exists():
        folder.mkdir(parents=True)
        for name in paths:
            source_file = source / name
            dest = folder / name
            dest.parent.mkdir(parents=True, exist_ok=True)
            dest.write_bytes(source_file.read_bytes())
    return folder

def file_manifest(work, ignored=()):
    result = {}
    for p in work.rglob('*'):
        rel = p.relative_to(work).as_posix()
        if p.is_file() and not any(part in {'.git', 'node_modules'} for part in p.relative_to(work).parts) and rel not in ignored:
            result[rel] = sha(p.read_bytes())
    return result

def materialize(config, example_dir, context_dir, label, include_dependencies=True):
    work = Path(tempfile.mkdtemp(prefix='ce-seminar-' + label + '-')) / 'workspace'
    work.mkdir()
    archive = work.parent / 'base.tar'
    with archive.open('wb') as out:
        run(['git', 'archive', config['base_commit']], stdout=out)
    with tarfile.open(archive) as source:
        for member in source.getmembers():
            if member.issym() or member.islnk() or Path(member.name).is_absolute() or '..' in Path(member.name).parts:
                raise RuntimeError('Unsafe archive member: ' + member.name)
        source.extractall(work)
    fixtures = example_dir / 'fixtures'
    if fixtures.exists():
        shutil.copytree(fixtures, work / 'fixtures')
    code = file_manifest(work)
    context = {}
    for p in context_dir.rglob('*'):
        if p.is_file():
            rel = p.relative_to(context_dir).as_posix()
            dest = work / rel
            dest.parent.mkdir(parents=True, exist_ok=True)
            dest.write_bytes(p.read_bytes())
            context[rel] = sha(p.read_bytes())
    run(['git', 'init', '--quiet', '--initial-branch=main'], cwd=work)
    run(['git', 'config', 'user.name', 'SinDenis'], cwd=work)
    run(['git', 'config', 'user.email', 'SinDenis@users.noreply.github.com'], cwd=work)
    run(['git', 'add', '.'], cwd=work)
    run(['git', 'commit', '--quiet', '-m', 'Fixed experiment input'], cwd=work)
    # Tools are present in both arms, copied before timing; there are no instructor files here.
    dependencies = ROOT / 'frontend/node_modules'
    if include_dependencies and dependencies.exists() and config['kind'] == 'http':
        shutil.copytree(dependencies, work / 'frontend/node_modules', symlinks=True)
    return work, code, context

def execute_example(args, context_paths=None, before_source=None, context_root=None):
    example_dir = SEM / 'examples' / args.example
    cfg = json.loads((example_dir / 'example.json').read_text())
    if context_paths:
        cfg['before_context'], cfg['after_context'] = context_paths
    prompt_file = example_dir / 'prompt.txt'
    prompt_hash = sha(prompt_file.read_bytes())
    stamp = datetime.now(timezone.utc).strftime('%Y%m%dT%H%M%S%fZ')
    report = SEM / 'reports' / args.example / stamp
    revision = args.revision or cfg.get('context_revision', 'v1')
    if args.after_source and not args.revision:
        raise RuntimeError('A custom context source requires --revision with a fresh name')
    if not revision or any(c not in 'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_' for c in revision):
        raise RuntimeError('Revision must use letters, numbers, dash or underscore')
    after_source = Path(args.after_source).resolve() if args.after_source else ROOT
    frozen_root = context_root or example_dir / 'contexts'
    if args.after_source and (frozen_root / revision).exists():
        raise RuntimeError('This revision is already frozen. Choose a new --revision to preserve it.')
    before_source = before_source or (ROOT / cfg['before_source'] if 'before_source' in cfg else ROOT)
    for source, paths in ((before_source, cfg['before_context']), (after_source, cfg['after_context'])):
        for name in paths:
            if not (source / name).is_file():
                raise RuntimeError('Missing context file: ' + str(source / name))
    before = freeze_context(example_dir, cfg['before_context'], 'before', revision, before_source, frozen_root)
    after = freeze_context(example_dir, cfg['after_context'], 'after', revision, after_source, frozen_root)
    native = native_env()
    home = codex_home()
    binary = tooling()
    report.mkdir(parents=True)
    tests = []
    prepared = []
    evaluator = report / 'evaluator'
    evaluator.mkdir()
    for name in ('verify.py', 'verify_domain.py', 'verify_db.py', 'grade.cjs'):
        shutil.copyfile(TOOLS / name, evaluator / name)
    shutil.copytree(TOOLS / 'fixtures', evaluator / 'fixtures')
    replicas = args.repeats or cfg.get('replicates', 3)
    for replica in range(1, replicas + 1):
        order = ('before', 'after') if replica % 2 else ('after', 'before')
        for condition in order:
            label = f'{condition}-r{replica}'
            workspace, code, context = materialize(cfg, example_dir, before if condition == 'before' else after, label)
            case_dir = report / label
            case_dir.mkdir()
            manifest = {
                'condition': condition, 'replica': replica, 'workspace': str(workspace),
                'base_commit': cfg['base_commit'], 'prompt_sha256': prompt_hash,
                'code_sha256': sha(json.dumps(code, sort_keys=True).encode()),
                'code_files': code, 'context_files': context,
                'context_bytes': sum((workspace / name).stat().st_size for name in context),
                'model': MODEL, 'effort': EFFORT,
                'context_revision': revision,
                'verifier_sha256': sha((TOOLS / 'verify.py').read_bytes()),
            }
            (case_dir / 'input.json').write_text(json.dumps(manifest, ensure_ascii=False, indent=2) + '\n')
            prepared.append(manifest)
            tests.append({'description': f'{condition}, повтор {replica}', 'vars': {
                'exampleId': args.example, 'workspaceDir': str(workspace), 'reportDir': str(case_dir),
                'baseCommit': cfg['base_commit'], 'condition': condition, 'replica': replica,
            }, 'assert': [{'type': 'javascript', 'value': 'file://' + str(evaluator / 'grade.cjs')}]})
    assert len({row['code_sha256'] for row in prepared}) == 1, 'Non-context code differs between arms'
    shared_cli_env = {'CODEX_HOME': str(home), 'PATH': native['PATH'], 'GOCACHE': native['GOCACHE']}
    additional = [native['GOCACHE'], str(Path.home() / '.npm'), str(Path.home() / 'Library/pnpm')]
    output = {
        'description': f'Context engineering: {cfg["title"]}',
        'prompts': ['file://' + str(prompt_file)],
        'providers': [{'id': 'openai:codex-sdk', 'label': MODEL + ' / ' + EFFORT, 'config': {
            'model': MODEL, 'model_reasoning_effort': EFFORT,
            'working_dir': '{{workspaceDir}}', 'sandbox_mode': cfg.get('sandbox_mode', 'workspace-write'),
            'approval_policy': 'never', 'network_access_enabled': False, 'web_search_mode': 'disabled',
            'persist_threads': False, 'enable_streaming': True,
            'codex_path_override': shutil.which('codex'), 'cli_env': shared_cli_env,
            'additional_directories': [p for p in additional if Path(p).exists()],
            **({'output_schema': cfg['output_schema']} if 'output_schema' in cfg else {}),
        }}], 'tests': tests, 'evaluateOptions': {'maxConcurrency': 1},
    }
    config_file = report / 'promptfooconfig.json'
    config_file.write_text(json.dumps(output, ensure_ascii=False, indent=2) + '\n')
    (report / 'prompt.txt').write_bytes(prompt_file.read_bytes())
    print(f'experiment: {args.example}\nreplicates: {replicas}\nconditions: 2\nprompt_sha256: {prompt_hash}', flush=True)
    command = [str(binary), 'eval', '-c', str(config_file), '--no-cache', '--no-share', '--no-progress-bar', '--max-concurrency', '1', '--no-table', '-o', str(report / 'promptfoo.json'), str(report / 'promptfoo.html')]
    with (report / 'console.log').open('w') as log:
        p = subprocess.run(command, cwd=ROOT, env=native, stdout=log, stderr=subprocess.STDOUT)
    if (report / 'promptfoo.json').exists():
        summarize(report, cfg)
    print(f'process_exit: {p.returncode}\nreport: {report}', flush=True)
    # A failed solution is an experiment result, not a reason to discard its report.
    if not (report / 'promptfoo.json').exists():
        raise RuntimeError('No result report: inspect console.log')
    return report

def summarize(report, cfg):
    data = json.loads((report / 'promptfoo.json').read_text())
    rows = []
    for result in data['results']['results']:
        variables = result['vars']
        case = Path(variables['reportDir'])
        response = result.get('response') or {}
        raw_value = response.get('raw') or {}
        raw = json.loads(raw_value) if isinstance(raw_value, str) else raw_value
        grading = result.get('gradingResult') or {}
        (case / 'private').mkdir(exist_ok=True)
        (case / 'private/codex-turn.json').write_text(json.dumps(raw, ensure_ascii=False, indent=2) + '\n')
        usage = response.get('tokenUsage') or {}
        commands = [item for item in raw.get('items', []) if item.get('type') == 'command_execution']
        row = {
            'condition': variables['condition'], 'replica': variables['replica'],
            'success': bool(result.get('success')), 'error': result.get('error'),
            'input_tokens': usage.get('prompt'), 'cached_input_tokens': usage.get('cached'),
            'uncached_input_tokens': (usage['prompt'] - usage.get('cached', 0)) if 'prompt' in usage else None,
            'output_tokens': usage.get('completion'), 'total_tokens': usage.get('total'),
            'latency_ms': result.get('latencyMs'), 'estimated_api_equivalent_usd': result.get('cost'),
            'command_count': len(commands), 'session_id': response.get('sessionId'),
            'grading': grading,
            'valid_trial': bool(grading) and not str(grading.get('reason', '')).startswith('Evaluator error:') and bool(response.get('output')) and usage.get('total') is not None,
        }
        session = response.get('sessionId')
        rollouts = list((RUNTIME / 'codex-home/sessions').rglob('*' + session + '.jsonl')) if session else []
        inputs = []
        user_messages = []
        if rollouts:
            shutil.copyfile(rollouts[0], case / 'private/rollout.jsonl')
            for line in rollouts[0].read_text().splitlines():
                event = json.loads(line); payload = event.get('payload', {})
                info = payload.get('info') or {}
                value = info.get('last_token_usage', {}).get('input_tokens')
                if payload.get('type') == 'token_count' and value is not None: inputs.append(value)
                if payload.get('type') == 'message' and payload.get('role') == 'user':
                    user_messages.extend(c.get('text', '') for c in payload.get('content', []) if isinstance(c, dict))
        row['largest_observed_request_input_tokens'] = max(inputs) if inputs else None
        row['last_request_input_tokens'] = inputs[-1] if inputs else None
        row['repository_instructions_observed_in_rollout'] = any('# Работа в DRIP Недвижимость' in text for text in user_messages)
        row['prompt_observed_in_rollout'] = any((report / 'prompt.txt').read_text().strip() in text for text in user_messages)
        trajectory = [{'command': item['command'], 'exit_code': item.get('exit_code'), 'output_characters': len(item.get('aggregated_output', ''))} for item in commands]
        (case / 'trajectory.json').write_text(json.dumps(trajectory, ensure_ascii=False, indent=2) + '\n')
        (case / 'metrics.json').write_text(json.dumps(row, ensure_ascii=False, indent=2) + '\n')
        rows.append(row)
    summary = {'example': cfg['id'], 'title': cfg['title'], 'hypothesis': cfg['hypothesis'], 'prompt_sha256': sha((report / 'prompt.txt').read_bytes()), 'model': MODEL, 'effort': EFFORT, 'trials': rows}
    (report / 'summary.json').write_text(json.dumps(summary, ensure_ascii=False, indent=2) + '\n')
    (report.parent / 'latest.json').write_text(json.dumps({'report': report.name}, indent=2) + '\n')

def main():
    parser = argparse.ArgumentParser(description='Контролируемые эксперименты context engineering через Promptfoo')
    sub = parser.add_subparsers(dest='action')
    sub.add_parser('setup', help='Установить закреплённые инструменты в игнорируемую папку runtime')
    from tools import classroom
    classroom.add_commands(sub)
    trial = sub.add_parser('run', help='Запустить один пример с фиксированным промптом')
    trial.add_argument('--example', required=True, choices=classroom.EXAMPLES)
    trial.add_argument('--repeats', type=classroom.positive_int)
    trial.add_argument('--revision', help='Fresh immutable context revision for a classroom experiment')
    trial.add_argument('--after-source', help='Folder with edited context files; requires a fresh --revision')
    prepare = sub.add_parser('prepare', help='Create an editable classroom copy from the frozen guided context')
    prepare.add_argument('--example', required=True, choices=classroom.EXAMPLES)
    restore = sub.add_parser('restore', help='Import the saved official series into native Promptfoo UI without model calls')
    restore.add_argument('--config-dir', help='Optional local Promptfoo database folder')
    args = parser.parse_args()
    if args.action == 'setup':
        tool = RUNTIME / 'tool'
        tool.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(TOOLS / 'package.json', tool / 'package.json')
        run(['npm', 'install', '--prefix', str(tool), '--no-audit', '--no-fund'])
        print('setup: ready')
    elif args.action in ('start', 'compare', 'check', 'view'):
        classroom.dispatch(args, sys.modules[__name__])
    elif args.action == 'run':
        execute_example(args)
    elif args.action == 'restore':
        env = native_env()
        if args.config_dir:
            env['PROMPTFOO_CONFIG_DIR'] = str(Path(args.config_dir).resolve())
        count = 0
        for path in sorted((SEM / 'examples').glob('*/example.json')):
            cfg = json.loads(path.read_text())
            folder = SEM / 'reports' / cfg['id']
            official = json.loads((folder / 'official.json').read_text())['report']
            export = folder / official / 'promptfoo.json'
            export_id = json.loads(export.read_text())['evalId']
            imported = subprocess.run([str(tooling()), 'import', str(export)], cwd=ROOT, env=env, text=True, capture_output=True)
            log = imported.stdout + imported.stderr
            duplicate = 'Eval ' + export_id + ' already exists. Use --new-id to import with a new ID, or --force to replace it.'
            if imported.returncode and duplicate not in log:
                raise RuntimeError('Promptfoo import failed: ' + log.strip())
            print(('existing_series: ' if imported.returncode else 'imported_series: ') + export_id, flush=True)
            count += 1
        print('available_series: ' + str(count))
        print('model_calls: 0')
        print('help: start promptfoo view with PROMPTFOO_CONFIG_DIR=' + env['PROMPTFOO_CONFIG_DIR'])
    elif args.action == 'prepare':
        example_dir = SEM / 'examples' / args.example
        cfg = json.loads((example_dir / 'example.json').read_text())
        dest = RUNTIME / 'classroom' / args.example
        if dest.exists():
            raise RuntimeError('Classroom copy already exists: ' + str(dest))
        source = example_dir / 'contexts' / cfg.get('context_revision', 'v1') / 'after'
        if not source.exists():
            raise RuntimeError('Frozen guided context not found. Run the example first.')
        work, _, _ = materialize(cfg, example_dir, source, 'classroom', include_dependencies=False)
        dest.parent.mkdir(parents=True, exist_ok=True)
        shutil.move(str(work), str(dest))
        shutil.rmtree(work.parent)
        print('workspace: ' + str(dest))
        print('help: edit only context, check it, then run with --after-source and a fresh --revision')
    else:
        examples = sorted((SEM / 'examples').glob('*/example.json'))
        print('bin: ' + str(Path(__file__).resolve()))
        print('description: Context engineering workshop with Promptfoo')
        print(f'examples: {len(examples)}')
        print('workspace: ' + str(SEM / 'workspace'))
        print('help: python3 seminar/run.py start')
        print('guide: ' + str(SEM / 'README.md'))

if __name__ == '__main__':
    try:
        main()
    except (RuntimeError, OSError, subprocess.CalledProcessError) as error:
        print('error: ' + json.dumps(str(error), ensure_ascii=False))
        sys.exit(1)

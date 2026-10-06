"""Small public workflow; saved official experiments remain separate."""
import argparse
from datetime import datetime, timezone
import hashlib
import json
from pathlib import Path
import shutil
import subprocess
import urllib.error
import urllib.request

EXAMPLES = ('01-http-contract', '02-domain-metrics', '03-db-command')
CONTEXT = (
    ('AGENTS.md',),
    ('AGENTS.md', 'REPO_MAP.md', 'CONTEXT.md'),
    ('AGENTS.md', 'REPO_MAP.md', 'CONTEXT.md',
     'docs/adr/0001-immutable-sessions.md', 'docs/adr/0002-diagnostics-interface.md'),
)


def positive_int(value):
    number = int(value)
    if number < 1:
        raise argparse.ArgumentTypeError('Use a positive integer')
    return number


def port_number(value):
    port = positive_int(value)
    if port > 65535:
        raise argparse.ArgumentTypeError('Port must be in 1..65535')
    return port


def add_commands(sub):
    start = sub.add_parser('start', help='Create a fixed-code workspace with five TBD files')
    start.add_argument('--workspace', help='Destination; default: seminar/workspace. Existing files are preserved.')
    compare = sub.add_parser('compare', help='Compare the previous context with your edited context')
    compare.add_argument('step', type=int, choices=(1, 2, 3))
    compare.add_argument('--repeats', type=positive_int, default=1, help='Replicas per condition (default: 1; use 3 for a fuller comparison)')
    compare.add_argument('--workspace', help='Context source; default: seminar/workspace')
    check = sub.add_parser('check', help='Check context links, Make targets and the AGENTS.md budget')
    check.add_argument('--workspace', help='Workspace to check; default: seminar/workspace')
    view = sub.add_parser('view', help='Open a local Promptfoo server without calling the model')
    view.add_argument('--port', type=port_number, default=15501)


def state_folder(workspace, runner):
    key = hashlib.sha256(str(workspace).encode()).hexdigest()[:12]
    return runner.RUNTIME / 'classroom-state' / key


def require_workspace(workspace):
    if not (workspace / 'Makefile').is_file():
        raise RuntimeError('Workspace is not ready. Run python3 seminar/run.py start first.')


def start_workspace(workspace, runner):
    if workspace.exists():
        require_workspace(workspace)
        if any(not (workspace / name).is_file() for name in CONTEXT[-1]):
            raise RuntimeError('Destination exists but has no workshop templates. Choose a new --workspace.')
        print('workspace: ' + str(workspace))
        print('status: already exists; your files were preserved')
    else:
        example = runner.SEM / 'examples' / EXAMPLES[0]
        config = json.loads((example / 'example.json').read_text())
        empty = runner.RUNTIME / 'empty-context'
        empty.mkdir(parents=True, exist_ok=True)
        work, _, _ = runner.materialize(config, example, empty, 'classroom-start', include_dependencies=False)
        try:
            for name in CONTEXT[-1]:
                target = work / name
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_text('TBD\n')
            runner.run(['git', 'add', '.'], cwd=work)
            runner.run(['git', 'commit', '--quiet', '-m', 'Empty workshop context'], cwd=work)
            workspace.parent.mkdir(parents=True, exist_ok=True)
            shutil.move(str(work), str(workspace))
        finally:
            shutil.rmtree(work.parent)
        print('workspace: ' + str(workspace))
        print('context_files: 5')
        print('status: all context files contain TBD')
    print('help: follow seminar/README.md, then run python3 seminar/run.py compare 1')


def compare(args, workspace, runner):
    require_workspace(workspace)
    index = args.step - 1
    paths = CONTEXT[index]
    for name in paths:
        if not (workspace / name).is_file():
            raise RuntimeError('Missing context file: ' + str(workspace / name))
    state = state_folder(workspace, runner)
    before_paths = () if index == 0 else CONTEXT[index - 1]
    before_source = runner.ROOT
    parent_report = None
    if index:
        previous = state / (str(args.step - 1) + '.json')
        if not previous.exists():
            raise RuntimeError('Run compare ' + str(args.step - 1) + ' first to save its context snapshot.')
        saved = json.loads(previous.read_text())
        before_source = Path(saved['context_snapshot'])
        parent_report = saved['report']
        if any(not (before_source / name).is_file() for name in before_paths):
            raise RuntimeError('Previous context snapshot is missing. Run the previous step again.')
    revision = 'classroom-' + datetime.now(timezone.utc).strftime('%Y%m%dT%H%M%S%fZ')
    context_root = state / 'contexts' / EXAMPLES[index]
    trial = argparse.Namespace(example=EXAMPLES[index], repeats=args.repeats,
                               revision=revision, after_source=str(workspace))
    report = runner.execute_example(trial, context_paths=(before_paths, paths),
                                    before_source=before_source, context_root=context_root)
    # Do not advance the lesson after a provider/evaluator failure. A failed
    # solution is valid evidence and may advance it; an invalid trial may not.
    summary = json.loads((report / 'summary.json').read_text())
    if not summary['trials'] or not all(row['valid_trial'] for row in summary['trials']):
        raise RuntimeError('Invalid trial; previous lesson state preserved. Inspect ' + str(report / 'console.log'))
    metadata = {'step': args.step, 'workspace': str(workspace), 'report': str(report),
                'context_snapshot': str(context_root / revision / 'after'),
                'previous_report': parent_report, 'files': list(paths)}
    (report / 'classroom.json').write_text(json.dumps(metadata, indent=2) + '\n')
    state.mkdir(parents=True, exist_ok=True)
    (state / (str(args.step) + '.json')).write_text(json.dumps(metadata, indent=2) + '\n')
    for later in range(args.step + 1, 4):
        (state / (str(later) + '.json')).unlink(missing_ok=True)
    print('step: ' + str(args.step))
    print('result: ' + str(report / 'summary.json'))
    print('help: python3 seminar/run.py view')


def view(args, runner):
    url = 'http://127.0.0.1:' + str(args.port) + '/eval?mode=all'
    try:
        with urllib.request.urlopen(url, timeout=2) as response:
            body = response.read().decode('utf-8', errors='replace')
        if 'promptfoo' not in body.lower():
            raise RuntimeError('Port is used by another service. Use view --port <free-port>.')
        print('url: ' + json.dumps(url))
        print('status: already running')
        return
    except urllib.error.URLError:
        pass
    print('url: ' + json.dumps(url), flush=True)
    print('model_calls: 0', flush=True)
    subprocess.run([str(runner.tooling()), 'view', '--port', str(args.port), '--no'],
                   cwd=runner.ROOT, env=runner.native_env(), check=True)


def dispatch(args, runner):
    workspace = Path(getattr(args, 'workspace', None) or runner.SEM / 'workspace').resolve()
    if args.action == 'start':
        start_workspace(workspace, runner)
    elif args.action == 'compare':
        compare(args, workspace, runner)
    elif args.action == 'check':
        from .check_context import inspect
        require_workspace(workspace)
        problems = inspect(workspace)
        pending = [name for name in CONTEXT[-1] if (workspace / name).is_file()
                   and (workspace / name).read_text().strip() == 'TBD']
        print('problems: ' + str(len(problems)))
        print('pending_context_files: ' + json.dumps(pending))
        for file, reason in problems:
            print('error: ' + json.dumps({'file': file, 'reason': reason}))
        print('result: ' + ('FAIL' if problems else 'PASS'))
        if problems:
            raise RuntimeError('Fix the reported links or commands before comparing.')
    else:
        view(args, runner)

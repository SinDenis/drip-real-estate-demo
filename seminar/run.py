#!/usr/bin/env python3
"""Six local Codex measurements. Python standard library only."""
import argparse
from datetime import datetime, timezone
import hashlib
import io
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tarfile
import tempfile
import time

ROOT = Path(__file__).resolve().parent.parent
SEM = ROOT / 'seminar'
RUNTIME = SEM / '.runtime'
BASE = 'd037c2cee00ca26a7674e4da13e2308eca22bbaf'
MODEL = 'gpt-6.1-sol'
CONTEXT = ('AGENTS.md', 'REPO_MAP.md', 'CONTEXT.md', 'docs/adr/0001-session-creation.md')
TITLES = {1: 'Название в liveness', 2: 'CSV-экспорт сессии', 3: 'Копирование фильтров сессии'}


def sha(data):
    return hashlib.sha256(data).hexdigest()


def command(args, cwd=ROOT, **kwargs):
    return subprocess.run(args, cwd=cwd, check=True, **kwargs)


def environment():
    env = dict(os.environ)
    cached = Path.home() / '.npm/_npx/78441fa2ededf7cf/node_modules/.bin'
    preferred = os.environ.get('CE_TOOLCHAIN_BIN')
    if preferred or cached.is_dir():
        env['PATH'] = str(preferred or cached) + os.pathsep + env['PATH']
    env['GOCACHE'] = str(RUNTIME / 'go-cache')
    env['PYTHONPYCACHEPREFIX'] = str(RUNTIME / 'pycache')
    env['npm_config_offline'] = 'true'
    for name in ('OPENAI_API_KEY', 'CODEX_API_KEY'):
        env.pop(name, None)
    Path(env['GOCACHE']).mkdir(parents=True, exist_ok=True)
    return env


def context_files(ref):
    if ref:
        commit = subprocess.check_output(['git', 'rev-parse', '--verify', '--end-of-options', ref + '^{commit}'], cwd=ROOT, text=True).strip()
        files = subprocess.check_output(['git', 'ls-tree', '-r', '--name-only', commit, '--', *CONTEXT], cwd=ROOT, text=True).splitlines()
        return {name: subprocess.check_output(['git', 'show', commit + ':' + name], cwd=ROOT) for name in files}
    return {name: (ROOT / name).read_bytes() for name in CONTEXT if (ROOT / name).is_file()}


def workspace(context):
    work = Path(tempfile.mkdtemp(prefix='ce-code-')) / 'repo'
    work.mkdir()
    archive = subprocess.check_output(['git', 'archive', BASE], cwd=ROOT)
    with tarfile.open(fileobj=io.BytesIO(archive)) as source:
        for item in source.getmembers():
            if item.issym() or item.islnk() or '..' in Path(item.name).parts or Path(item.name).is_absolute():
                raise RuntimeError('Unsafe base archive member')
        source.extractall(work)
    files = {str(path.relative_to(work)): sha(path.read_bytes()) for path in work.rglob('*') if path.is_file()}
    protected = {name: value for name, value in files.items() if name.endswith(('_test.go', '.test.ts', '.test.tsx')) or name.startswith('tests/')}
    for name, data in context.items():
        target = work / name
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes(data)
        protected[name] = sha(data)
    for args in (['git', 'init', '-q', '--initial-branch=main'],
                 ['git', 'config', 'user.name', 'SinDenis'],
                 ['git', 'config', 'user.email', 'SinDenis@users.noreply.github.com'],
                 ['git', 'add', '.'], ['git', 'commit', '-qm', 'Fixed task input']):
        command(args, cwd=work)
    shutil.copytree(ROOT / 'frontend/node_modules', work / 'frontend/node_modules', symlinks=True)
    return work, sha(json.dumps(files, sort_keys=True).encode()), protected


def measure(args):
    env = environment()
    if not (ROOT / 'frontend/node_modules').is_dir():
        raise RuntimeError('Install project dependencies: make bootstrap PNPM=pnpm')
    binary = shutil.which('codex', path=env['PATH'])
    if not binary:
        raise RuntimeError('Install Codex CLI and run codex login first')
    login = Path(os.environ.get('CE_LOGIN_HOME', str(Path.home() / '.codex'))) / 'auth.json'
    if not login.is_file():
        raise RuntimeError('Run codex login first')
    home = RUNTIME / 'codex-home'
    home.mkdir(parents=True, exist_ok=True)
    if not (home / 'auth.json').exists():
        (home / 'auth.json').symlink_to(login)
    env['CODEX_HOME'] = str(home)
    prompt = (SEM / 'prompts' / (str(args.task) + '.txt')).read_bytes()
    context = context_files(args.context_ref)
    work, code_hash, protected = workspace(context)
    stamp = datetime.now(timezone.utc).strftime('%Y%m%dT%H%M%S%fZ')
    report = SEM / 'reports' / f'{args.task}-{args.condition}-{stamp}'
    report.mkdir(parents=True)
    (report / 'prompt.txt').write_bytes(prompt)
    for name, data in context.items():
        destination = report / 'context' / name
        destination.parent.mkdir(parents=True, exist_ok=True)
        destination.write_bytes(data)
    argv = [binary, 'exec', '--json', '--ephemeral', '--ignore-user-config',
            '--sandbox', 'workspace-write', '--model', MODEL,
            '-c', 'model_reasoning_effort="low"', '-c', 'approval_policy="never"',
            '-c', 'web_search="disabled"', '-c', 'sandbox_workspace_write.network_access=false',
            '--add-dir', env['GOCACHE'], '--add-dir', str(Path.home() / '.npm')]
    for cache in (Path.home() / 'Library/Caches/pnpm', Path.home() / 'Library/pnpm'):
        if cache.is_dir():
            argv.extend(['--add-dir', str(cache)])
    argv.extend(['--cd', str(work), '--output-last-message', str(report / 'answer.md'), '-'])
    manifest = {'task': args.task, 'condition': args.condition, 'base': BASE,
                'prompt_sha256': sha(prompt), 'code_sha256': code_hash,
                'context_sha256': {name: sha(data) for name, data in context.items()},
                'context_bytes': sum(len(data) for data in context.values()),
                'model': MODEL, 'reasoning_effort': 'low', 'command': argv,
                'codex_version': subprocess.check_output([binary, '--version'], text=True).strip(),
                'workspace': str(work), 'context_ref': args.context_ref}
    (report / 'input.json').write_text(json.dumps(manifest, ensure_ascii=False, indent=2) + '\n')
    shutil.copytree(SEM / 'checks', report / 'checker', ignore=shutil.ignore_patterns('__pycache__'))
    print(f'task: {args.task}\ncondition: {args.condition}\nreport: {report}', flush=True)
    started = time.perf_counter()
    with (report / 'events.jsonl').open('wb') as output, (report / 'codex.log').open('wb') as errors:
        process = subprocess.run(argv, input=prompt, cwd=work, env=env, stdout=output, stderr=errors)
    elapsed = time.perf_counter() - started
    events = []
    for line in (report / 'events.jsonl').read_text().splitlines():
        try:
            events.append(json.loads(line))
        except json.JSONDecodeError:
            pass
    turns = [event['usage'] for event in events if event.get('type') == 'turn.completed']
    usage = {name: sum(turn.get(name, 0) for turn in turns) for name in ('input_tokens', 'cached_input_tokens', 'output_tokens')}
    command(['git', 'add', '-A'], cwd=work)
    patch = subprocess.check_output(['git', 'diff', '--cached', '--binary', 'HEAD'], cwd=work)
    (report / 'changes.patch').write_bytes(patch)
    stats = subprocess.check_output(['git', 'diff', '--cached', '--numstat', 'HEAD'], cwd=work, text=True)
    handwritten = 0
    for line in stats.splitlines():
        added, removed, name = line.split('\t', 2)
        if added.isdigit() and removed.isdigit() and not name.endswith(('api.gen.go', 'schema.d.ts')):
            handwritten += int(added) + int(removed)
    # Import the exact frozen checker, never the candidate's code.
    import importlib.util
    spec = importlib.util.spec_from_file_location('external_checks', report / 'checker/check.py')
    checker = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(checker)
    valid = process.returncode == 0 and bool(turns)
    checks = checker.verify(args.task, work, report, env, protected) if valid else {}
    result = {'task': args.task, 'condition': args.condition, 'seconds': round(elapsed, 2),
              **usage, 'total_tokens': usage['input_tokens'] + usage['output_tokens'],
              'uncached_input_tokens': usage['input_tokens'] - usage['cached_input_tokens'],
              'status': ('PASS' if all(checks.values()) else 'FAIL') if valid else 'INVALID',
              'checks': checks, 'handwritten_changed_lines': handwritten,
              'commands': sum(event.get('type') == 'item.completed' and event.get('item', {}).get('type') == 'command_execution' for event in events),
              'exit_code': process.returncode, 'prompt_sha256': sha(prompt), 'code_sha256': code_hash,
              'checker_sha256': {path.name: sha(path.read_bytes()) for path in (report / 'checker').glob('*') if path.is_file()},
              'context_files': list(context)}
    (report / 'result.json').write_text(json.dumps(result, ensure_ascii=False, indent=2) + '\n')
    shutil.rmtree(work / 'frontend/node_modules')
    write_readme()
    print(f'result: {result["status"]}\nseconds: {elapsed:.2f}\ntotal_tokens: {result["total_tokens"]}', flush=True)
    print('help: open seminar/README.md and the saved changes.patch')
    if not valid:
        raise RuntimeError('Codex did not finish a measured turn. Inspect the saved codex.log; this is not a solution failure.')


GUIDE = '''# Семинар 5: три задачи на код, шесть коммитов

Один промпт на задачу, две независимые попытки. Прямой `codex exec`, без Promptfoo.
Каждый запуск получает чистый код d037c2c и выбранные документы. Патчи предыдущего агента не накапливаются.

## Подготовка

Нужны Git, Python 3.9+, Codex CLI 0.160.1 с `codex login`, Go из backend/go.mod, Node 24 и pnpm 11.25.0.
Из корня проекта один раз подготовь зависимости с доступом к сети:

```sh
make bootstrap PNPM=pnpm
(cd backend && go mod download)
```

Агент во время замера работает без сети. Модель вызывается через авторизацию Codex.
Скрипт не требует Python-пакетов, Docker или реальной БД.

## Проход

Открывай коммиты по тегам в таблице. В каждой паре прочитай промпт в `seminar/prompts/N.txt`,
предскажи эффект документов, сравни отчет и `changes.patch`. Все задания требуют изменить код и добавить тесты.
Для повторного запуска из текущей ветки скопируй команду целиком: `--context-ref` берет документы указанного коммита.
Без этого параметра берутся текущие AGENTS.md, REPO_MAP.md, CONTEXT.md и ADR из корня.

| Шаг / тег | Что показывать | Повторить замер |
|---|---|---|
| ce-code-01 | /health/live без AGENTS.md | `python3 seminar/run.py 1 before --context-ref ce-code-01` |
| ce-code-02 | Добавляем короткий AGENTS.md, тот же промпт | `python3 seminar/run.py 1 after --context-ref ce-code-02` |
| ce-code-03 | CSV-экспорт, пока только AGENTS.md | `python3 seminar/run.py 2 before --context-ref ce-code-03` |
| ce-code-04 | Добавляем карту и доменный словарь | `python3 seminar/run.py 2 after --context-ref ce-code-04` |
| ce-code-05 | Копирование фильтров сессии без ADR | `python3 seminar/run.py 3 before --context-ref ce-code-05` |
| ce-code-06 | Добавляем решение о создании неизменяемых сессий | `python3 seminar/run.py 3 after --context-ref ce-code-06` |

Точный текст добавляемого контекста показывают три команды:

```sh
git diff ce-code-01 ce-code-02 -- AGENTS.md
git diff ce-code-03 ce-code-04 -- AGENTS.md REPO_MAP.md CONTEXT.md
git diff ce-code-05 ce-code-06 -- AGENTS.md docs/adr
```

1. **Liveness**: поле service, сохранение status и поведения readiness, согласованные Go/TypeScript контракты.
2. **CSV**: новый GET, авторизация, экранирование, неизвестное значение и настоящий ноль, ошибки, отсутствие изменения данных.
3. **Duplicate**: новый POST, новая сессия по прежним фильтрам, обычные правила создания, идемпотентность и неизменность источника.

Для второй и третьей фич ориентир 200-300 строк с тестами, без генерации. Реальный размер патчей указан ниже.
В AGENTS.md пишем действия и команды. В карте - где искать код и границы слоев. В CONTEXT.md - смысл данных.
ADR объясняет причину прежнего решения и его применение к новой операции; это отмеченная учебная реконструкция.

## Результаты

Время - запуск Codex и его инструменты, без подготовки копии и внешней проверки.
Токены берутся из turn.completed: total = input + output, cached входит в input.
UNC = input - cached. Это расход всего цикла, не размер контекстного окна и не денежная стоимость.
Один прогон на условие иллюстрирует механизм, но не доказывает статистическое превосходство; кеши общие.
PASS дает внешний HTTP-тест, проверка обоих generated контрактов и прежних backend-тестов.
INVALID означает незавершенный запуск инструмента, а не неправильное решение.

'''


def write_readme():
    rows = {}
    for path in sorted((SEM / 'reports').glob('*/result.json')):
        data = json.loads(path.read_text())
        rows[(data['task'], data['condition'])] = (path.parent, data)
    text = GUIDE + '| Задача | Контекст | Секунды | Total | Cached | UNC | Результат | Строк изменений* |\n'
    text += '|---|---|---:|---:|---:|---:|---|---:|\n'
    for task in (1, 2, 3):
        for condition in ('before', 'after'):
            if (task, condition) not in rows:
                continue
            folder, row = rows[(task, condition)]
            link = str(folder.relative_to(SEM))
            text += f'| {task}. {TITLES[task]} | {condition} | {row["seconds"]:.1f} | {row["total_tokens"]:,} | {row["cached_input_tokens"]:,} | {row["uncached_input_tokens"]:,} | [{row["status"]}]({link}/checks.log) | [{row["handwritten_changed_lines"]}]({link}/changes.patch) |\n'
    text += '\n*Добавленные и удаленные строки, включая тесты и YAML; generated Go/TypeScript исключены.\n\n'
    for task in (1, 2, 3):
        pair = [rows.get((task, arm)) for arm in ('before', 'after')]
        if not all(pair):
            continue
        before, after = pair[0][1], pair[1][1]
        same = all(before[key] == after[key] for key in ('prompt_sha256', 'code_sha256', 'checker_sha256'))
        text += f'**{task}. {TITLES[task]}**: {before["status"]} -> {after["status"]}. '
        text += f'Одинаковые промпт, код и проверяющий: {"да" if same else "НЕТ, сравнение некорректно"}. '
        if before['status'] != 'INVALID' and after['status'] != 'INVALID':
            for label, key in (('Время', 'seconds'), ('Total', 'total_tokens'), ('UNC', 'uncached_input_tokens')):
                if before[key]:
                    text += f'{label}: {(after[key] / before[key] - 1) * 100:+.1f}%. '
        for arm, row in zip(('before', 'after'), (before, after)):
            failures = [key for key, value in row['checks'].items() if not value]
            if failures:
                text += arm + ': ' + ', '.join(failures) + '. '
        text = text.rstrip() + '\n\n'
    for folder, row in rows.values():
        if row.get('regrade'):
            original = str(folder.relative_to(SEM)) + '/original-evaluation/checks.log'
            text += ('**Исправление проверяющего в задаче 3:** пробельный ключ оказался допустимым по существующему контракту. '
                     'Тест заменен на ключ длиной 129 символов и проверен на прежней операции создания. '
                     'Готовый патч переоценен без нового вызова модели; время и токены сохранены. '
                     f'[Первоначальная ошибочная оценка]({original}) также сохранена.\n\n')
    if len(rows) == 6 and all(row['status'] == 'PASS' for _, row in rows.values()):
        text += ('Все шесть решений прошли проверку. Эти прогоны показывают изменение затрат, '
                 'но не улучшение корректности от контекста: задачи оказались решаемыми в обоих условиях. '
                 'На семинаре обсуди, какие документы помогли поиску, а какие добавили чтение; '
                 'не подгоняй вывод под ожидание, что больше контекста всегда лучше.\n\n')
    text += '''## Что открыть в отчете

В `reports/` каждый запуск сохранен отдельно: `result.json` - измерения, `changes.patch` - код,
`answer.md` - ответ, `events.jsonl` - действия, `checks.log` - независимые проверки.
Патч сохраняется до генерации и тестов внешнего проверяющего.
`input.json`, `prompt.txt`, `context/` и `checker/` сохраняют точные входы и проверяющий.
Повторные запуски добавляют каталоги, таблица показывает последние результаты каждого условия.
Авторизация и кеши лежат в игнорируемой `.runtime/`; агент получает только код и документы.

Проверь контекст: ссылки ведут в существующий код, команды работают, термины согласованы со схемой,
ADR актуален, а независимая проверка подтверждает результат. Зеленый тест не доказывает каждую фразу документа.

Пересобрать этот файл из сохраненных данных: `python3 seminar/run.py --report`.
Этот Markdown создается скриптом; текст прохода находится в GUIDE в run.py.
Документация: [Codex exec и JSONL usage](https://learn.chatgpt.com/docs/non-interactive-mode).
'''
    (SEM / 'README.md').write_text(text)


def main():
    parser = argparse.ArgumentParser(description='Three coding tasks measured directly with Codex CLI')
    parser.add_argument('task', nargs='?', type=int, choices=(1, 2, 3))
    parser.add_argument('condition', nargs='?', choices=('before', 'after'))
    parser.add_argument('--context-ref', help='Read context from this Git commit or tag; default: current files')
    parser.add_argument('--report', action='store_true', help='Regenerate Markdown without calling Codex')
    args = parser.parse_args()
    if args.report:
        if args.task or args.condition or args.context_ref:
            parser.error('--report cannot be combined with a task or context')
        write_readme()
        print('report: seminar/README.md')
    elif args.task and args.condition:
        measure(args)
    elif args.task or args.condition or args.context_ref:
        parser.error('Supply both task and condition, for example: 1 before')
    else:
        print('bin: ' + str(Path(__file__).resolve()))
        print('description: Three coding tasks; six measured Codex runs')
        print('help: python3 seminar/run.py 1 before')
        print('guide: seminar/README.md')


if __name__ == '__main__':
    try:
        main()
    except (OSError, ValueError, RuntimeError, subprocess.CalledProcessError) as error:
        print('error: ' + json.dumps(str(error), ensure_ascii=False))
        sys.exit(1)

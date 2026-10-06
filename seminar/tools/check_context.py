#!/usr/bin/env python3
"""Check context against the current repository, not against a list of sections."""
import argparse
import json
from pathlib import Path
import re
import sys

def inspect(root):
    problems = []
    targets = set(re.findall(r'^([a-zA-Z0-9_-]+):', (root / 'Makefile').read_text(), re.M))
    context = [root / 'AGENTS.md', root / 'CONTEXT.md', root / 'REPO_MAP.md']
    context += list((root / 'docs').rglob('*.md')) if (root / 'docs').exists() else []
    for file in context:
        if not file.exists(): continue
        text = file.read_text()
        if file.name == 'AGENTS.md' and len(text.encode()) > 6000:
            problems.append((str(file.relative_to(root)), 'AGENTS.md exceeds the chosen 6000-byte budget'))
        for reference in re.findall(r'\[[^\]]*\]\(([^)]+)\)', text):
            if '://' in reference or reference.startswith('#'): continue
            target = (file.parent / reference.split('#')[0]).resolve()
            if not target.exists(): problems.append((str(file.relative_to(root)), 'Broken link: ' + reference))
        for command in re.findall(r'\bmake\s+([a-zA-Z][a-zA-Z0-9_-]*)', text):
            if command not in targets:
                # Explicit descriptions of a missing command are not a recommendation to run it.
                if command == 'deploy' and 'нет команды' in text: continue
                problems.append((str(file.relative_to(root)), 'Unknown Make target: ' + command))
    return problems

def main():
    p=argparse.ArgumentParser(description='Проверить ссылки, команды и бюджет файлов контекста')
    p.add_argument('root', nargs='?', default=str(Path(__file__).resolve().parents[2]))
    args=p.parse_args(); problems=inspect(Path(args.root))
    print(f'problems: {len(problems)}')
    for file,reason in problems: print('error: '+json.dumps({'file':file,'reason':reason},ensure_ascii=False))
    print('result: '+('FAIL' if problems else 'PASS'))
    return bool(problems)

if __name__=='__main__':sys.exit(main())

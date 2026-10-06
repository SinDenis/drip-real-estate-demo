"""Literal, independently worked expectations for the synthetic domain fixture."""
import json
from pathlib import Path
import subprocess

def grade(work: Path, response: str, report: Path):
    try:
        answer = json.loads(response)
    except json.JSONDecodeError:
        return [('structured_answer', False)]
    # 125 + 50 = 175; (125-120) + (50-55) = 0. Object 9003 is unavailable.
    checks = [
        ('monthly_sales_not_identifiable', answer.get('monthly_sales', 'missing') is None),
        ('known_current_sum', answer.get('current_ddu_known_sum') == 175),
        ('incomplete_coverage_not_total', answer.get('current_ddu_all_objects', 'missing') is None),
        ('comparable_delta', answer.get('comparable_pairs_delta') == 0),
        ('missing_not_zero', sorted(answer.get('unavailable_object_ids', [])) == ['9003']),
        ('limitations_explained', len(str(answer.get('limitations', ''))) >= 50),
        ('domain_terms_present', len(answer.get('terms', [])) >= 2),
        ('repository_unchanged', not subprocess.check_output(['git', 'diff', '--cached', '--name-only', 'HEAD'], cwd=work)),
    ]
    (report / 'expected.json').write_text(json.dumps({
        'monthly_sales': None, 'current_ddu_known_sum': 175,
        'current_ddu_all_objects': None, 'comparable_pairs_delta': 0,
        'unavailable_object_ids': ['9003'],
        'reason': 'A published cumulative snapshot is not a flow of sales; one object is unavailable.',
    }, ensure_ascii=False, indent=2) + '\n')
    return checks

#!/usr/bin/env python3
"""Generate the seminar review page from saved reports, without rewriting raw files."""
import json
from pathlib import Path
import statistics

SEM = Path(__file__).resolve().parent
GITHUB = 'https://github.com/SinDenis/drip-real-estate-demo/blob/codex/context-engineering-seminar/'

EXPLANATIONS = {
 '01-http-contract': {'name':'HTTP-контракт','before':'Пустой контекст репозитория','after':'AGENTS.md + описание + workflow','lesson':'Два baseline-ответа оставили TypeScript-контракт устаревшим. Проверка самого Go API этого не обнаруживает.','oracle':'HTTP handler, readiness, Go/TypeScript codegen, исходные HTTP-тесты'},
 '02-domain-metrics': {'name':'Язык предметной области','before':'Базовые инструкции первого примера','after':'Карта + архитектура + CONTEXT.md','lesson':'Обе версии ответили правильно. Добавление документов не дало измеренного выигрыша в точности. Ошибка deleted в заметке отдельно найдена по схеме.','oracle':'175 по известной части, delta 0, неизвестные продажи и полный итог, missing 9003'},
 '03-db-command': {'name':'БД и прежние решения','before':'Исправленный полный контекст второго примера','after':'Два ADR + ссылки на решения','lesson':'Все шесть решений прошли контракт. При дополнительном разборе патчей db-query переиспользован в 1/3 baseline и 3/3 guided. Это наблюдение после запуска, не новый критерий pass.','oracle':'Три реальные synthetic SQL fixture, SELECT-only, один запрос, CSV и прежние команды'},
}

def build_data():
 cases=[]
 for path in sorted((SEM/'examples').glob('*/example.json')):
  cfg=json.loads(path.read_text()); folder=SEM/'reports'/cfg['id']
  report=folder/json.loads((folder/'official.json').read_text())['report']
  raw=json.loads((report/'summary.json').read_text())
  rows=[]
  for trial in raw['trials']:
   name=trial['condition']+'-r'+str(trial['replica']); location=report/name
   checks=json.loads((location/'verification.json').read_text())['checks']
   rows.append({**trial,'folder':str(location.relative_to(SEM)), 'failed_checks':[n for n,v in checks.items() if not v]})
  aggregates={}
  for condition in ['before','after']:
   arm=[t for t in rows if t['condition']==condition]
   aggregates[condition]={'passed':sum(t['success'] for t in arm),'n':len(arm),**{k:statistics.median(t[k] for t in arm) for k in ['total_tokens','uncached_input_tokens','cached_input_tokens','input_tokens','output_tokens','latency_ms','command_count','largest_observed_request_input_tokens','last_request_input_tokens']}}
  cases.append({'id':cfg['id'],'title':cfg['title'],'prompt':(report/'prompt.txt').read_text(),'hash':raw['prompt_sha256'],'report':str(report.relative_to(SEM)),'rows':rows,'aggregate':aggregates,'context':{c:json.loads((report/(c+'-r1')/'input.json').read_text())['context_files'] for c in ['before','after']},**EXPLANATIONS[cfg['id']]})
 return {'cases':cases,'github':GITHUB,'audit':json.loads((SEM/'reports/protocol-audit.json').read_text())}

TEMPLATE = (SEM / 'report-template.html').read_text()

def main():
 data=build_data();target=SEM/'.lavish/seminar-5.html';target.parent.mkdir(exist_ok=True)
 payload=json.dumps(data,ensure_ascii=False).replace('<','\\u003c')
 target.write_text(TEMPLATE.replace('__DATA__',payload))
 print('artifact: '+str(target))

if __name__=='__main__':main()

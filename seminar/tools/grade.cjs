const { spawnSync } = require('node:child_process');
const path = require('node:path');
module.exports = (output, context) => {
  const p = spawnSync('python3', [path.join(__dirname, 'verify.py'), context.vars.exampleId, context.vars.workspaceDir, context.vars.reportDir], {
    input: String(output), encoding: 'utf8', timeout: 300000,
    env: { ...process.env, CE_BASE_COMMIT: context.vars.baseCommit },
  });
  if (p.error || p.status !== 0) return { pass: false, score: 0, reason: `Evaluator error: ${p.error?.message ?? p.stderr.slice(-1500)}` };
  try { return JSON.parse(p.stdout); }
  catch { return { pass: false, score: 0, reason: `Invalid evaluator output: ${p.stdout.slice(-1000)}` }; }
};

/** @type {import('dependency-cruiser').IConfiguration} */
module.exports = {
  forbidden: [
    {
      name: "no-circular-dependencies",
      severity: "error",
      from: {},
      to: { circular: true },
    },
    {
      name: "api-layer-does-not-depend-on-ui",
      severity: "error",
      from: { path: "^src/api" },
      to: { path: "^src/(?!api)" },
    },
    {
      name: "production-does-not-depend-on-tests",
      severity: "error",
      from: { path: "^src/(?!.*\\.test\\.)" },
      to: { path: "\\.test\\." },
    },
  ],
  options: {
    doNotFollow: { path: "node_modules" },
    exclude: "(^|/)node_modules/|\\.d\\.ts$",
    tsConfig: { fileName: "tsconfig.json" },
  },
};

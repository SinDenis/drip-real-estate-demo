import eslint from "@eslint/js";
import reactHooks from "eslint-plugin-react-hooks";
import reactRefresh from "eslint-plugin-react-refresh";
import sonarjs from "eslint-plugin-sonarjs";
import tseslint from "typescript-eslint";

export default tseslint.config(
  { ignores: ["coverage", "dist", "src/api/schema.d.ts"] },
  eslint.configs.recommended,
  ...tseslint.configs.strictTypeChecked,
  reactHooks.configs.flat.recommended,
  {
    files: ["**/*.{ts,tsx}"],
    languageOptions: {
      parserOptions: { projectService: true, tsconfigRootDir: import.meta.dirname },
    },
    plugins: { "react-refresh": reactRefresh, sonarjs },
    rules: {
      complexity: ["error", 10],
      "max-depth": ["error", 4],
      "max-lines": ["error", { max: 400, skipBlankLines: true, skipComments: true }],
      "max-lines-per-function": ["error", { max: 60, skipBlankLines: true, skipComments: true }],
      "no-restricted-globals": [
        "error",
        { name: "fetch", message: "Use the typed src/api client." },
      ],
      "react-refresh/only-export-components": ["warn", { allowConstantExport: true }],
      "sonarjs/cognitive-complexity": ["error", 15],
    },
  },
  {
    files: ["src/api/**/*.{ts,tsx}"],
    rules: { "no-restricted-globals": "off" },
  },
);

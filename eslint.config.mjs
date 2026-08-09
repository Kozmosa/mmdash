import eslint from "@eslint/js";
import globals from "globals";
import tseslint from "typescript-eslint";

export default tseslint.config(
  {
    ignores: [
      "**/.next/**",
      "**/.cache/**",
      "**/.claude/**",
      "**/.pytest_cache/**",
      "**/.testenv/**",
      "**/.tmp/**",
      "**/.venv/**",
      "**/dist/**",
      "**/node_modules/**",
      "archive/**",
      "docs/design/**",
      "Projectmmdash.tmppytest-*/**",
    ],
  },
  eslint.configs.recommended,
  ...tseslint.configs.recommended,
  {
    files: ["**/*.{js,mjs,ts,tsx}"],
    languageOptions: {
      globals: {
        ...globals.browser,
        ...globals.node,
      },
    },
  },
);

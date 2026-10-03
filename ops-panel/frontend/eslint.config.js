import tseslint from "typescript-eslint"

export default tseslint.config(
  { ignores: [".vite", "dist", "node_modules"] },
  ...tseslint.configs.recommended,
  {
    rules: {
      "@typescript-eslint/no-explicit-any": "off",
    },
  },
)

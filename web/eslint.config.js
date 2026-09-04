import js from '@eslint/js';
import globals from 'globals';
import reactHooks from 'eslint-plugin-react-hooks';
import reactRefresh from 'eslint-plugin-react-refresh';
import tseslint from 'typescript-eslint';

export default tseslint.config(
  { ignores: ['dist', 'coverage', 'playwright-report', 'test-results'] },
  {
    extends: [js.configs.recommended, ...tseslint.configs.recommended],
    files: ['**/*.{ts,tsx}'],
    languageOptions: { ecmaVersion: 2023, globals: globals.browser },
    plugins: { 'react-hooks': reactHooks, 'react-refresh': reactRefresh },
    rules: {
      ...reactHooks.configs.recommended.rules,
      'react-refresh/only-export-components': ['warn', { allowConstantExport: true }],
      '@typescript-eslint/no-explicit-any': 'off',
      // React clears a synthetic event's currentTarget as soon as the handler
      // returns. A setState updater runs later (during render) whenever another
      // update is already pending, so reading the event there throws and takes
      // the whole screen down.
      'no-restricted-syntax': ['error', {
        selector: 'CallExpression[callee.name=/^set[A-Z]/] > ArrowFunctionExpression MemberExpression[property.name="currentTarget"]',
        message: 'setState 업데이터 안에서 event.currentTarget을 읽지 마세요. 핸들러에서 값을 먼저 읽어 넘기세요.',
      }],
    },
  },
);

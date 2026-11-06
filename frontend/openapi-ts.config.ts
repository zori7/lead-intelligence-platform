import { defineConfig } from '@hey-api/openapi-ts';

// Generates TypeScript types and zod schemas from the API contract.
// Run `npm run gen:api` (or `make openapi` at the repo root); never edit the output.
export default defineConfig({
	input: '../api/openapi.yaml',
	output: {
		path: 'src/lib/api/gen',
		// SvelteKit's tsconfig extends a virtual `$app/tsconfig` the generator cannot
		// resolve; skip it and state the import style explicitly instead.
		tsConfigPath: null,
		importFileExtension: '.js',
		postProcess: ['prettier']
	},
	plugins: ['@hey-api/typescript', { name: 'zod', compatibilityVersion: 4 }]
});

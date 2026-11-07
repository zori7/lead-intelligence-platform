import tailwindcss from '@tailwindcss/vite';
import adapter from '@sveltejs/adapter-static';
import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vitest/config';

export default defineConfig({
	plugins: [
		tailwindcss(),
		sveltekit({
			compilerOptions: {
				// Force runes mode for the project, except for libraries. Can be removed in svelte 6.
				runes: ({ filename }) =>
					filename.split(/[/\\]/).includes('node_modules') ? undefined : true
			},
			// Pure static SPA: every unknown path falls back to index.html and is routed client-side.
			adapter: adapter({ fallback: 'index.html' }),
			// Emitted as a <meta> tag in index.html, with a hash for SvelteKit's inline
			// bootstrap script. Inline style attributes stay allowed: Svelte transitions,
			// GSAP and toasts set them at runtime. nginx adds the header-only directives.
			csp: {
				mode: 'hash',
				directives: {
					'default-src': ['self'],
					'script-src': ['self'],
					'style-src': ['self', 'unsafe-inline'],
					'img-src': ['self', 'data:'],
					'font-src': ['self'],
					'connect-src': ['self'],
					'object-src': ['none'],
					'base-uri': ['self'],
					'form-action': ['self']
				}
			}
		})
	],
	test: {
		include: ['src/**/*.test.ts']
	},
	server: {
		proxy: {
			// Same-origin API in development; nginx plays this role in Docker.
			'/api': { target: 'http://localhost:8080', changeOrigin: false }
		}
	}
});

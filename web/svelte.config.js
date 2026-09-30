import adapter from '@sveltejs/adapter-static';
import { vitePreprocess } from '@sveltejs/vite-plugin-svelte';

/** @type {import('@sveltejs/kit').Config} */
export default {
	preprocess: vitePreprocess(),
	kit: {
		// Static SPA: the Go binary serves these files; no Node server in production.
		// `200.html` is the SPA shell for client-side routes (see internal/api/static.go).
		adapter: adapter({ pages: 'build', assets: 'build', fallback: '200.html', strict: false })
	}
};

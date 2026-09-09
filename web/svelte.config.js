import adapter from '@sveltejs/adapter-static';
import { vitePreprocess } from '@sveltejs/vite-plugin-svelte';
import { mdsvex } from 'mdsvex';
import { mdsvexShiki } from '@mistweaverco/mdsvex-shiki';

const highlighter = await mdsvexShiki({
	displayLanguage: true,
	displayPath: true
});

/** @type {import('@sveltejs/kit').Config} */
const config = {
	preprocess: [vitePreprocess(), mdsvex({ highlight: { highlighter } })],

	kit: {
		adapter: adapter()
	},

	extensions: ['.svelte', '.svx', '.mdx', '.md']
};

export default config;

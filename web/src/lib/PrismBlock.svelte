<script lang="ts">
	import { afterNavigate } from '$app/navigation';
	import { onMount } from 'svelte';
	import { highlightCode } from '$lib/prism';

	let { code, language = 'yaml' }: { code: string; language?: string } = $props();

	onMount(() => {
		highlightCode();
	});

	afterNavigate(() => {
		highlightCode();
	});
</script>

<div class="text-left">
	<pre><code
			class="language-{language}"
			data-toolbar-order="copy-to-clipboard"
			data-prismjs-copy="📋">{code}</code
		></pre>
</div>

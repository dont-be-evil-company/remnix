<script lang="ts">
	/**
	 * Fallback when the Vite `inline-shiki-codeblocks` plugin has not transformed the page yet.
	 * Production and dev builds normally replace `<CodeBlock>` with inlined Shiki HTML first.
	 */
	let { code }: { code: string; lang: string; meta?: string } = $props();
</script>

<div class="mb-0">
	<pre class="border-base-300 bg-base-200 rounded border p-4 text-sm"><code>{code}</code></pre>
</div>

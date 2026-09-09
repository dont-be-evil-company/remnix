<script lang="ts">
	import type { RemnixColors } from '$lib/themes';

	let { colors }: { colors: RemnixColors } = $props();

	const swatches = $derived([
		colors.accent,
		colors.title,
		colors.badge,
		colors.duration,
		colors.failed,
		colors.syntax.command,
		colors.syntax.string,
		colors.text
	]);
</script>

<div class="flex justify-center gap-2">
	{#each swatches as color, idx (idx)}
		<span
			class="inline-block h-6 w-6 rounded-full border border-white/20 shadow-sm"
			style="background: {color}"
			title={color}
		></span>
	{/each}
</div>

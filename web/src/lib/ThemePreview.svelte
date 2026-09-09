<script lang="ts">
	import type { RemnixColors } from '$lib/themes';

	let { colors, background }: { colors: RemnixColors; background: string } = $props();
</script>

<div
	class="mx-auto max-w-md rounded-xl p-4 text-left font-mono text-sm shadow-inner"
	style="background: {background}; color: {colors.text}"
>
	<div class="mb-3">
		<span class="font-bold" style="color: {colors.title}">remnix</span>
		<span class="mx-2" style="color: {colors.muted}">search</span>
		<span style="color: {colors.badge}">local</span>
	</div>
	<div class="mb-1" style="color: {colors.rule}">────────────────────────</div>
	<div>
		<span class="font-bold" style="color: {colors.accent}">❯</span>
		<span class="font-bold" style="color: {colors.syntax.command}">git</span>
		<span class="font-bold" style="color: {colors.syntax.keyword}">commit</span>
		<span style="color: {colors.syntax.flag}">-m</span>
		<span style="color: {colors.syntax.string}">"ship it"</span>
		<span style="color: {colors.syntax.path}">./app.go</span>
		<span class="float-right" style="color: {colors.duration}">2s</span>
	</div>
	<div class="mt-1">
		<span class="invisible font-bold">❯</span>
		<span style="color: {colors.syntax.variable}">$COUNT</span>
		<span style="color: {colors.syntax.operator}">=</span>
		<span style="color: {colors.syntax.number}">3</span>
		<span style="color: {colors.syntax.comment}"># note</span>
		<span class="float-right" style="color: {colors.failed}">failed</span>
	</div>
</div>

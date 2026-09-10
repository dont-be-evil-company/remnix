<script lang="ts">
	import HeadComponent from '$lib/HeadComponent.svelte';
	import ColorSwatches from '$lib/ColorSwatches.svelte';
	import ThemePreview from '$lib/ThemePreview.svelte';
	import type { Theme } from '$lib/themes';
	import type { Component } from 'svelte';
	import DraculaOfficialYaml from '$lib/theme-code/dracula-official.svelte';
	import OneDarkProYaml from '$lib/theme-code/one-dark-pro.svelte';
	import TokyoNightYaml from '$lib/theme-code/tokyo-night.svelte';
	import Synthwave84Yaml from '$lib/theme-code/synthwave-84.svelte';
	import CatppuccinYaml from '$lib/theme-code/catppuccin.svelte';
	import MonokaiProYaml from '$lib/theme-code/monokai-pro.svelte';

	const yamlBySlug: Record<string, Component<{ flavorId: string }>> = {
		'dracula-official': DraculaOfficialYaml,
		'one-dark-pro': OneDarkProYaml,
		'tokyo-night': TokyoNightYaml,
		'synthwave-84': Synthwave84Yaml,
		catppuccin: CatppuccinYaml,
		'monokai-pro': MonokaiProYaml
	};

	let { theme }: { theme: Theme } = $props();
	const defaultFlavorId = $derived(theme.flavors?.at(-1)?.id ?? '');
	let flavorOverride = $state<string | undefined>();
	const flavorId = $derived(flavorOverride ?? defaultFlavorId);
	const YamlBlock = $derived(yamlBySlug[theme.slug]);

	const active = $derived(
		theme.flavors?.find((flavor) => flavor.id === flavorId) ?? {
			id: theme.slug,
			name: theme.name,
			background: theme.background,
			colors: theme.colors
		}
	);

	const onFlavorChange = (event: Event) => {
		flavorOverride = (event.currentTarget as HTMLSelectElement).value;
	};
</script>

<HeadComponent
	data={{
		title: `${theme.name} · remnix themes`,
		description: theme.description
	}}
/>

<div class="hero bg-base-200 min-h-screen">
	<div class="hero-content w-full max-w-full min-w-0 text-center">
		<div class="w-full max-w-2xl min-w-0 py-10">
			<a href="/themes">
				<img src="/logo.png" alt="remnix logo" class="m-5 mx-auto w-32" />
			</a>
			<h1 class="text-5xl font-bold">{theme.name}</h1>
			<p class="py-6">{theme.description}</p>
			<ColorSwatches colors={active.colors} />
			<p class="py-6">
				Copy the <code>ui.colors</code> section into <code>~/.config/remnix/config.yaml</code>, then
				reload with <code>remnix daemon reload</code>.
			</p>
			{#if theme.flavors}
				<select class="select select-bordered mb-5" value={flavorId} onchange={onFlavorChange}>
					{#each theme.flavors as flavor (flavor.id)}
						<option value={flavor.id}>{flavor.name}</option>
					{/each}
				</select>
			{/if}
			<div class="mb-8">
				<ThemePreview colors={active.colors} background={active.background} />
			</div>
			{#if YamlBlock}
				<div class="text-left">
					<YamlBlock {flavorId} />
				</div>
			{/if}
			<p>
				<a href="/themes"><button class="btn btn-primary mt-8">All themes</button></a>
			</p>
		</div>
	</div>
</div>

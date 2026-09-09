<script lang="ts">
	import HeadComponent from '$lib/HeadComponent.svelte';
	import ColorSwatches from '$lib/ColorSwatches.svelte';
	import PrismBlock from '$lib/PrismBlock.svelte';
	import ThemePreview from '$lib/ThemePreview.svelte';
	import type { Theme } from '$lib/themes';

	let { theme }: { theme: Theme } = $props();
	const defaultFlavorId = $derived(theme.flavors?.at(-1)?.id ?? '');
	let flavorOverride = $state<string | undefined>();
	const flavorId = $derived(flavorOverride ?? defaultFlavorId);

	const active = $derived(
		theme.flavors?.find((flavor) => flavor.id === flavorId) ?? {
			id: theme.slug,
			name: theme.name,
			background: theme.background,
			colors: theme.colors,
			yaml: theme.yaml
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
	<div class="hero-content text-center">
		<div class="max-w-2xl py-10">
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
			{#if theme.flavors}
				{#each theme.flavors as flavor (flavor.id)}
					<div class={flavor.id === flavorId ? '' : 'hidden'}>
						<PrismBlock code={flavor.yaml} />
					</div>
				{/each}
			{:else}
				<PrismBlock code={theme.yaml} />
			{/if}
			<p>
				<a href="/themes"><button class="btn btn-primary mt-8">All themes</button></a>
			</p>
		</div>
	</div>
</div>

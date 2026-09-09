<script lang="ts">
	import HeadComponent from '$lib/HeadComponent.svelte';
	import ColorSwatches from '$lib/ColorSwatches.svelte';
	import { themes } from '$lib/themes';
</script>

<HeadComponent
	data={{
		title: 'Themes · remnix',
		description:
			'Copy-paste ui.colors themes for remnix: Dracula, One Dark Pro, Tokyo Night, SynthWave ’84, Catppuccin, and Monokai Pro.'
	}}
/>

<div id="themes" class="hero bg-base-200 min-h-screen">
	<div class="hero-content text-center">
		<div class="max-w-5xl">
			<a href="/">
				<img src="/logo.png" alt="remnix logo" class="m-5 mx-auto w-32" />
			</a>
			<h1 class="text-5xl font-bold">Themes 🎨</h1>
			<p class="py-6">
				Paste a <code>ui.colors</code> block into <code>~/.config/remnix/config.yaml</code>, then
				reload with <code>remnix daemon reload</code>.
			</p>
			<div class="grid gap-6 md:grid-cols-2 xl:grid-cols-3">
				{#each themes as theme (theme.slug)}
					<a href="/themes/{theme.slug}" class="block">
						<div class="card bg-base-100 mx-auto h-full w-full max-w-96 shadow-sm">
							<div class="card-body">
								<h2 class="card-title justify-center">{theme.name}</h2>
								<ColorSwatches colors={theme.colors} />
								<p>{theme.description}</p>
								<div class="card-actions justify-end">
									<span class="badge badge-outline">ui.colors</span>
								</div>
							</div>
						</div>
					</a>
				{/each}
			</div>
			<p>
				<a href="/"><button class="btn btn-primary mt-8">Back home</button></a>
			</p>
		</div>
	</div>
</div>

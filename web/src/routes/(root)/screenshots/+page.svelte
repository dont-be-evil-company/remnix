<script lang="ts">
	import HeadComponent from '$lib/HeadComponent.svelte';
	import { screenshots } from '$lib/screenshots';
</script>

<HeadComponent
	data={{
		title: 'Screenshots · remnix',
		description:
			'remnix Screenshots: Fuzzy CTRL+R history search, ghost-text and LSP-Style comp-menu.'
	}}
/>

<div id="screenshots" class="hero bg-base-200 min-h-screen">
	<div class="hero-content w-full max-w-full min-w-0 text-center">
		<div class="w-full max-w-5xl min-w-0">
			<a href="/">
				<img src="/logo.png" alt="remnix logo" class="m-5 mx-auto w-32" />
			</a>
			<h1 class="text-5xl font-bold">Screenshots 📸</h1>
			<div class="grid gap-6 md:grid-cols-2 xl:grid-cols-3">
				{#each screenshots as screenshot (screenshot.slug)}
					<a href="/screenshots/{screenshot.slug}" class="block">
						<div class="card bg-base-100 mx-auto h-full w-full max-w-96 shadow-sm">
							<div class="card-body">
								<h2 class="card-title justify-center">{screenshot.name}</h2>
								<p>{screenshot.description}</p>
								<div class="card-actions justify-end">
									<span class="badge badge-outline">screenshot</span>
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

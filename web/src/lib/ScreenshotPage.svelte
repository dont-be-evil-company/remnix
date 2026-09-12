<script lang="ts">
	import HeadComponent from '$lib/HeadComponent.svelte';
	import type { Screenshot } from '$lib/screenshots';
	let { screenshot }: { screenshot: Screenshot } = $props();
</script>

<HeadComponent
	data={{
		title: `${screenshot.name} · remnix screenshots`,
		description: screenshot.description
	}}
/>

<div class="hero bg-base-200 min-h-screen">
	<div class="hero-content w-full max-w-full min-w-0 text-center">
		<div class="w-full max-w-2xl min-w-0 py-10">
			<a href="/screenshots">
				<img src="/logo.png" alt="remnix logo" class="m-5 mx-auto w-32" />
			</a>
			<h1 class="text-5xl font-bold">{screenshot.name}</h1>
			<p class="py-6">{screenshot.description}</p>
			<a href={screenshot.url}>
				<img
					src={screenshot.url}
					alt={screenshot.description}
					class="m-5 mx-auto max-w-full min-w-0"
				/>
			</a>
			<p>
				<a href="/screenshots"><button class="btn btn-primary mt-8">All screenshots</button></a>
			</p>
		</div>
	</div>
</div>

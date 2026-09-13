<script lang="ts">
	import HeadComponent from '$lib/HeadComponent.svelte';
	import CodeBlock from '$lib/CodeBlock.svelte';
	import { onMount } from 'svelte';
	import { browser } from '$app/environment';
	import { redirect } from '@sveltejs/kit';
	const installSystems = [
		{
			name: 'install script (linux/macos)',
			value: 'unix'
		},
		{
			name: 'install script (windows)',
			value: 'windows'
		},
		{
			name: 'manual',
			value: 'manual'
		},
		{
			name: 'Arch Linux x64',
			value: 'aur'
		}
	];
	let installSystem = '';
	let form: HTMLFormElement;
	const onInstallSystemChange = () => {
		form.submit();
	};
	onMount(() => {
		if (!browser) return;
		const urlParams = new URLSearchParams(window.location.search);
		const typeParam = urlParams.get('type');
		if (typeParam && !installSystems.some((system) => system.value === typeParam)) {
			redirect(302, '/install?type=unix');
		}
		if (typeParam) {
			installSystem = typeParam;
		} else {
			installSystem = 'unix';
		}
	});
</script>

<HeadComponent
	data={{
		title: 'Install · remnix',
		description:
			'remnix install: Install remnix via install script, manually or via AUR (Arch Linux).'
	}}
/>

<div id="install" class="hero bg-base-200 min-h-screen">
	<div class="hero-content w-full max-w-full min-w-0 text-center">
		<div class="w-full max-w-md min-w-0">
			<a href="/">
				<img src="/logo.png" alt="remnix logo" class="m-5 mx-auto w-32" />
			</a>
			<h1 class="text-5xl font-bold">Install ⚡</h1>
			<p class="py-6">Install remnix via ...</p>
			<form method="GET" bind:this={form}>
				<select name="type" on:input={onInstallSystemChange} class="select select-bordered mb-5">
					{#each installSystems as system}
						<option value={system.value} selected={installSystem === system.value}
							>{system.name}</option
						>
					{/each}
				</select>
			</form>
			<div class={installSystem === '' ? '' : 'hidden'}>
				<span class="loading loading-spinner text-info loading-xl"></span>
			</div>
			<div class={installSystem === 'unix' ? '' : 'hidden'}>
				<p class="mb-5">Linux / macOS:</p>
				<div class="text-left">
					<CodeBlock lang="sh" code={`curl -sSL https://remnix.app/install.sh | sh`} />
				</div>
			</div>
			<div class={installSystem === 'windows' ? '' : 'hidden'}>
				<p class="mb-5">Windows (PowerShell):</p>
				<div class="text-left">
					<CodeBlock lang="powershell" code={`iwr https://remnix.app/install.ps1 -useb | iex`} />
				</div>
				<p class="mb-5">Update later with <code>remnix update</code>.</p>
			</div>
			<div class={installSystem === 'manual' ? '' : 'hidden'}>
				<p class="mb-5">
					Download the latest release from the <a class="text-secondary" href="/download"
						>releases page</a
					>.
				</p>
			</div>
			<div class={installSystem === 'aur' ? '' : 'hidden'}>
				<p class="mb-5">
					Via AUR, using an AUR helper like <a
						href="https://github.com/Jguer/yay"
						class="text-secondary">yay</a
					>
				</p>
				<div class="text-left">
					<CodeBlock lang="sh" code={`yay -S remnix-bin`} />
				</div>
				<p class="mt-5 mb-5">
					.. or via <a href="https://github.com/morganamilo/paru" class="text-secondary">paru</a>
				</p>
				<div class="text-left">
					<CodeBlock lang="sh" code={`paru -S remnix-bin`} />
				</div>
			</div>
			<p>
				<a href="/"><button class="btn btn-primary mt-8">Back home</button></a>
			</p>
		</div>
	</div>
</div>

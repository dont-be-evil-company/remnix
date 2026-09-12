<script lang="ts">
	import '@fortawesome/fontawesome-free/css/all.css';
	import { onMount } from 'svelte';
	import HeadComponent from '$lib/HeadComponent.svelte';
	import CodeBlock from '$lib/CodeBlock.svelte';
	import mentions from './mentions.json';

	interface Mention {
		title: string;
		contents: string[];
		links: { text: string; url: string }[];
	}

	let downloadLink = '';

	let randomOrderMentions: Mention[] = [];

	let installSystem = 'script';

	const handleAnchorClick = (evt: Event) => {
		evt.preventDefault();
		const link = evt.currentTarget as HTMLAnchorElement;
		const anchorId = new URL(link.href).hash.replace('#', '');
		const anchor = document.getElementById(anchorId);
		window.scrollTo({
			top: anchor?.offsetTop,
			behavior: 'smooth'
		});
		window.history.pushState(null, '', `#${anchorId}`);
	};

	const onInstallSystemChange = (evt: Event) => {
		const select = evt.currentTarget as HTMLSelectElement;
		installSystem = select.value;
	};

	onMount(() => {
		randomOrderMentions = mentions.sort(() => 0.5 - Math.random());
	});
</script>

<HeadComponent
	data={{
		title: 'remnix',
		description:
			'Sync 📡 your shell 🐚 history 📚 across unlimited devices. Fast ⚡ and intelligent 🧠 history search 🔎 with batteries 🔋 included.'
	}}
/>
<div id="start" class="hero bg-base-200 min-h-screen">
	<div class="hero-content w-full max-w-full min-w-0 text-center">
		<div class="w-full max-w-md min-w-0">
			<img src="/logo.png" alt="remnix logo" class="m-5 mx-auto w-64" />
			<h1 class="text-5xl font-bold">remnix</h1>
			<details>
				<summary>What does <span class="badge badge-warning">remnix</span> mean?</summary>
				<p>
					<span class="text-warning">Rem</span>inisce means to talk, write, or think about past
					experiences with pleasure and nostalgia.
				</p>
				<p>
					U<span class="text-warning">nix</span>, the operating system, is a family of multitasking,
					multiuser computer operating systems that derive from the original AT&T Unix.
				</p>
				<p>
					Remnix is a combination of these two words, and has nothing to do with <a
						class="link link-accent"
						href="https://nixos.org/">NixOS</a
					>, the Linux distribution.
				</p>
				<p>It is a play on words, and is meant to be a fun and memorable name for the project.</p>
				<p class="mnemonic">
					<span class="badge badge-soft badge-warning">rem</span>ember u<span
						class="badge badge-soft badge-warning">nix</span
					> (commands) is not the official mnemonic, but it is a fun way to remember the name.
				</p>
			</details>
			<p class="py-6">
				Encrypted, server-free shell history. Commands live in a local SQLite database.
				Synchronization is optional: a background daemon can copy encrypted event bundles to storage
				you already have (Google Drive, Dropbox, S3, a folder, ...).
			</p>
			<p class="py-6">There is no remnix cloud and no account.</p>
			<p class="py-6">
				Think of it as <a class="link link-accent" href="https://atuin.sh/">Atuins</a> nerdy little
				sister, but BYO (Bring Your Own) storage and
				<span
					class="tooltip decoration-info text-info underline decoration-dotted"
					data-tip=".. as in customizable">hackable</span
				>.
			</p>
			<div class="flex flex-wrap justify-center gap-3">
				<a href="#install" on:click={handleAnchorClick}
					><button class="btn btn-accent">Install</button></a
				>
				<a href="/screenshots"> <button class="btn btn-primary">Screenshots</button></a>
				<a href="/themes"><button class="btn btn-secondary">Themes</button></a>
				<a href="/docs"><button class="btn btn-info">Docs</button></a>
			</div>
		</div>
	</div>
</div>
<div id="install" class="hero bg-base-200 min-h-screen">
	<div class="hero-content w-full max-w-full min-w-0 text-center">
		<div class="w-full max-w-md min-w-0">
			<a href="#install" on:click={handleAnchorClick}>
				<h1 class="text-5xl font-bold">Install ⚡</h1>
			</a>
			<p class="py-6">Install remnix ...</p>
			<select on:input={onInstallSystemChange} class="select select-bordered mb-5">
				<option value="script">install script</option>
				<option value="manually">select manually</option>
				<option value="aur">Arch Linux x64</option>
			</select>
			<div class={installSystem === 'script' ? '' : 'hidden'}>
				<p class="mb-5">Linux / macOS:</p>
				<div class="text-left">
					<CodeBlock lang="bash" code={`curl -sSL https://remnix.app/install.sh | sh`} />
				</div>
				<p class="mb-5">Windows (PowerShell):</p>
				<div class="text-left">
					<CodeBlock lang="powershell" code={`iwr https://remnix.app/install.ps1 -useb | iex`} />
				</div>
				<p class="mb-5">Update later with <code>remnix update</code>.</p>
			</div>
			<div class={installSystem === 'manually' ? '' : 'hidden'}>
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
					<CodeBlock lang="bash" code={`yay -S remnix-bin`} />
				</div>
				<p class="mb-5">
					.. or via <a href="https://github.com/morganamilo/paru" class="text-secondary">paru</a>
				</p>
				<div class="text-left">
					<CodeBlock lang="bash" code={`paru -S remnix-bin`} />
				</div>
			</div>
			<div
				class={installSystem !== 'manually' && installSystem !== 'aur' && installSystem !== 'script'
					? ''
					: 'hidden'}
			>
				<p class="mb-5">
					<a href={downloadLink} target="_blank" rel="noopener noreferrer">
						<button class="btn btn-secondary mt-5">Download {installSystem}</button></a
					>
				</p>
			</div>
			<p>
				<a href="#honorable-mentions" on:click={handleAnchorClick}
					><button class="btn btn-primary mt-5">Honorable mentions</button></a
				>
			</p>
		</div>
	</div>
</div>
<div id="honorable-mentions" class="hero bg-base-200 min-h-screen">
	<div class="hero-content w-full max-w-full min-w-0 text-center">
		<div class="w-full max-w-md min-w-0">
			<a href="#honorable-mentions" on:click={handleAnchorClick}>
				<h1 class="text-5xl font-bold">Honorable mentions 🥰</h1>
			</a>
			<p class="py-6">These projects helped make remnix possible:</p>
			{#each randomOrderMentions as mention, idx}
				{#if idx > 0}
					<div class="my-4"></div>
				{/if}
				<div class="card bg-base-100 mx-auto max-w-96 shadow-sm">
					<div class="card-body">
						<h2 class="card-title">
							{mention.title}
						</h2>
						{#each mention.contents as content}
							<p>{content}</p>
						{/each}
						<div class="card-actions justify-end">
							{#each mention.links as link}
								<a class="badge badge-outline" href={link.url}>{link.text}</a>
							{/each}
						</div>
					</div>
				</div>
			{/each}
			<p>
				<a href="#get-involved" on:click={handleAnchorClick}
					><button class="btn btn-primary mt-5">Get involved</button></a
				>
			</p>
		</div>
	</div>
</div>
<div id="get-involved" class="hero bg-base-200 min-h-screen">
	<div class="hero-content w-full max-w-full min-w-0 text-center">
		<div class="w-full max-w-md min-w-0">
			<a href="#get-involved" on:click={handleAnchorClick}>
				<h1 class="text-5xl font-bold">Get involved 📦</h1>
			</a>
			<p class="py-6">remnix is open-source and we welcome contributions.</p>
			<p>
				View the <a class="text-secondary" href="https://github.com/dont-be-evil-company/remnix"
					>code</a
				>, browse
				<a class="text-secondary" href="/themes">themes</a>, and/or check out the
				<a class="text-secondary" href="/docs">docs</a>.
			</p>
		</div>
	</div>
</div>

<style>
	details {
		border-radius: 4px;
		padding: 10px;
	}

	summary {
		font-weight: bold;
		cursor: pointer;
		user-select: none;
		margin-bottom: 10px;
	}

	details {
		overflow: hidden;
	}

	details::details-content {
		block-size: 0;
		opacity: 0;
		overflow: hidden;
		border: 5px solid transparent;
		background-color: #181818;
		transition:
			block-size 300ms ease,
			opacity 200ms ease,
			content-visibility 300ms allow-discrete;
	}

	details[open]::details-content {
		block-size: auto;
		opacity: 1;
		border: 5px solid #181818;
		border-radius: 10px;
	}

	details > summary:first-of-type {
		display: list-item;
		list-style: none;
	}

	details > summary:first-of-type:before {
		content: '📚';
		margin-right: 15px;
		display: inline-block;
	}

	details[open] > summary:first-of-type:before {
		content: '📖';
	}

	details p {
		margin: 10px 20px auto 20px;
		font-size: 16px;
	}

	details p.mnemonic {
		font-size: 14px;
	}

	@supports (interpolate-size: allow-keywords) {
		:root {
			interpolate-size: allow-keywords;
		}
	}
</style>

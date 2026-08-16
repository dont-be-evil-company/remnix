<script lang="ts">
	import '@fortawesome/fontawesome-free/css/all.css';
	import Prism from 'prismjs';
	import 'prismjs/plugins/toolbar/prism-toolbar';
	import 'prismjs/plugins/copy-to-clipboard/prism-copy-to-clipboard';
	import 'prismjs/components/prism-json';
	import 'prismjs/components/prism-yaml';
	import 'prismjs/components/prism-bash';
	import 'prismjs/components/prism-powershell';
	import 'prismjs/themes/prism-okaidia.css';
	import { onMount } from 'svelte';
	import HeadComponent from '$lib/HeadComponent.svelte';
	import mentions from './mentions.json';

	interface Mention {
		title: string;
		contents: string[];
		links: { text: string; url: string }[];
	}

	interface PrismaRegisterButtonContext {
		element: { parentNode: HTMLElement | null };
	}
	const downloadBaseUrl =
		'https://github.com/dont-be-evil-company/remnix/releases/latest/download/';

	let downloadLink = downloadBaseUrl + 'remnix-linux-amd64';

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
		switch (installSystem) {
			case 'linux-amd64':
				downloadLink = downloadBaseUrl + 'remnix-linux-amd64';
				break;
			case 'macos-x64':
				downloadLink = downloadBaseUrl + 'remnix-darwin-amd64.dmg';
				break;
			case 'macos-arm64':
				downloadLink = downloadBaseUrl + 'remnix-darwin-arm64.dmg';
				break;
			case 'windows-amd64':
				downloadLink = downloadBaseUrl + 'remnix-windows-amd64.exe';
				break;
			default:
				downloadLink = '';
		}
	};

	onMount(() => {
		randomOrderMentions = mentions.sort(() => 0.5 - Math.random());
		Prism.plugins.toolbar.registerButton(
			'fullscreen-code',
			function (ctx: PrismaRegisterButtonContext) {
				const button = document.createElement('button');
				button.innerHTML = '🔍';
				button.addEventListener('click', function () {
					ctx.element.parentNode?.requestFullscreen();
				});

				return button;
			}
		);

		Prism.highlightAll();
	});
</script>

<HeadComponent
	data={{
		title: 'remnix',
		description: 'Sync 📡 your shell 🐚 history 📚 across unlimited devices. Fast ⚡ and intelligent 🧠 history search 🔎 with batteries 🔋 included.'
	}}
/>

<div id="start" class="hero bg-base-200 min-h-screen">
	<div class="hero-content text-center">
		<div class="max-w-md">
			<img src="/logo.png" alt="remnix logo" class="m-5 mx-auto w-64" />
			<h1 class="text-5xl font-bold">remnix</h1>
			<p class="py-6">
        Encrypted, server-free shell history. Commands live in a local SQLite database. Synchronization is optional: a background daemon can copy encrypted event bundles to storage you already have (Google Drive, Dropbox, S3, a folder, ...) using an embedded rclone engine. There is no remnix cloud and no account.
			</p>
			<p class="py-6">
        Remote storage is untrusted. Encryption, key wrapping, and merge happen in remnix - rclone only reads and writes objects.
			</p>
			<a href="#screenshots" on:click={handleAnchorClick}
				><button class="btn btn-primary">Screenshots</button></a
			>
		</div>
	</div>
</div>
<div id="screenshots" class="hero bg-base-200 min-h-screen">
	<div class="hero-content text-center">
		<div class="max-w-2xl">
			<a href="#screenshots" on:click={handleAnchorClick}>
				<h1 class="text-5xl font-bold">Screenshots 📸</h1>
			</a>
			<a href="/assets/tapes/cli/ctrlr.gif">
				<img
					src="/assets/tapes/cli/ctrlr.gif"
					alt="Screenshot of the overview"
					class="m-5 mx-auto"
				/>
			</a>
			<p class="py-6">
        Shows the fuzzy search of the history database.
			</p>
			<a href="#install" on:click={handleAnchorClick}
				><button class="btn btn-primary">Install</button></a
			>
		</div>
	</div>
</div>
<div id="install" class="hero bg-base-200 min-h-screen">
	<div class="hero-content text-center">
		<div class="max-w-md">
			<a href="#install" on:click={handleAnchorClick}>
				<h1 class="text-5xl font-bold">Install ⚡</h1>
			</a>
			<p class="py-6">Install remnix ...</p>
			<select on:input={onInstallSystemChange} class="select select-bordered mb-5">
				<option value="script">install script</option>
				<option value="manually">select manually</option>
				<option value="aur">Arch Linux x64</option>
				<option value="linux-amd64">Linux x64</option>
				<option value="macos-amd64">MacOS x64</option>
				<option value="macos-arm64">MacOS arm64</option>
				<option value="windows-amd64">Windows x64</option>
			</select>
			<div class={installSystem === 'script' ? '' : 'hidden'}>
				<p class="mb-5">Linux / macOS:</p>
				<pre><code
						class="language-bash"
						data-toolbar-order="copy-to-clipboard"
						data-prismjs-copy="📋">curl -sSL /install.sh | sh</code
					></pre>
				<p class="mb-5">Windows (PowerShell):</p>
				<pre><code
						class="language-powershell"
						data-toolbar-order="copy-to-clipboard"
						data-prismjs-copy="📋">iwr /install.ps1 -useb | iex</code
					></pre>
				<p class="mb-5">Update later with <code class="language-bash">remnix update</code>.</p>
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
				<pre><code
						class="language-bash"
						data-toolbar-order="copy-to-clipboard"
						data-prismjs-copy="📋">yay -S remnix-bin</code
					></pre>
				<p class="mb-5">
					.. or via <a href="https://github.com/morganamilo/paru" class="text-secondary">paru</a>
				</p>
				<pre><code
						class="language-bash"
						data-toolbar-order="copy-to-clipboard"
						data-prismjs-copy="📋">paru -S remnix-bin</code
					></pre>
			</div>
			<div class={installSystem !== 'manually' && installSystem !== 'aur' && installSystem !== 'script' ? '' : 'hidden'}>
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
	<div class="hero-content text-center">
		<div class="max-w-md">
			<a href="#honorable-mentions" on:click={handleAnchorClick}>
				<h1 class="text-5xl font-bold">Honorable mentions 🥰</h1>
			</a>
			<p class="py-6">These projects helped make remnix possible:</p>
			{#each randomOrderMentions as mention, idx}
				{#if idx > 0}
					<div class="my-4"></div>
				{/if}
				<div class="card bg-base-100 mx-auto w-96 shadow-sm">
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
	<div class="hero-content text-center">
		<div class="max-w-md">
			<a href="#get-involved" on:click={handleAnchorClick}>
				<h1 class="text-5xl font-bold">Get involved 📦</h1>
			</a>
			<p class="py-6">remnix is open-source and we welcome contributions.</p>
			<p>
				View the <a class="text-secondary" href="https://github.com/dont-be-evil-company/remnix"
					>code</a
				>, and/or check out the
				<a class="text-secondary" href="/docs">docs</a>.
			</p>
		</div>
	</div>
</div>

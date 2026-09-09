<script lang="ts">
	import HeadComponent from '$lib/HeadComponent.svelte';
</script>

<HeadComponent
	data={{
		title: 'remnix',
		description:
			'Sync 📡 your shell 🐚 history 📚 across unlimited devices. Fast ⚡ and intelligent 🧠 history search 🔎 with batteries 🔋 included.'
	}}
/>

<div id="start" class="hero bg-base-200 min-h-screen">
	<div class="hero-content text-center">
		<div class="max-w-md">
			<img src="/logo.png" alt="remnix logo" class="m-5 mx-auto w-64" />
			<h1 class="text-5xl font-bold">Timetrack</h1>
			<p class="py-6">
				Encrypted, server-free shell history. Commands live in a local SQLite database.
				Synchronization is optional: a background daemon can copy encrypted event bundles to storage
				you already have (Google Drive, Dropbox, S3, a folder, ...) using an embedded rclone engine.
				There is no remnix cloud and no account.
			</p>
			<p class="py-6">
				Remote storage is untrusted. Encryption, key wrapping, and merge happen in remnix - rclone
				only reads and writes objects.
			</p>
		</div>
	</div>
</div>

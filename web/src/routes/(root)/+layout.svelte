<script lang="ts">
	import '../../app.css';
	import '@mistweaverco/mdsvex-shiki/styles.css';
	import { copyAction } from '@mistweaverco/mdsvex-shiki/copyAction';

	let { children } = $props();
</script>

<div use:copyAction>
	{@render children()}
</div>

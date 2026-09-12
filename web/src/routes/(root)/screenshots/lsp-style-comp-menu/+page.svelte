<script lang="ts">
	import ScreenshotPage from '$lib/ScreenshotPage.svelte';
	import { requireScreenshot } from '$lib/screenshots';

	const screenshot = requireScreenshot('lsp-style-comp-menu');
</script>

<ScreenshotPage {screenshot} />

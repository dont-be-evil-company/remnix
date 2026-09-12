<script lang="ts">
	import ScreenshotPage from '$lib/ScreenshotPage.svelte';
	import { requireScreenshot } from '$lib/screenshots';

	const screenshot = requireScreenshot('ctrlr-fuzzy-history-search');
</script>

<ScreenshotPage {screenshot} />

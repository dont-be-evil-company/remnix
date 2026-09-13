<script lang="ts">
	import ThemePage from '$lib/ThemePage.svelte';
	import { requireTheme } from '$lib/themes';

	const theme = requireTheme('vhs-era');
</script>

<ThemePage {theme} />

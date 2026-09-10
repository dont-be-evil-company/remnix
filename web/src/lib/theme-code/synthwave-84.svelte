<script lang="ts">
	import CodeBlock from '$lib/CodeBlock.svelte';

	let { flavorId: _flavorId }: { flavorId: string } = $props();
</script>

<CodeBlock
	lang="yaml"
	meta="path=~/.config/remnix/config.yaml"
	code={`ui:
    colors:
        accent: "#FF7EDB"
        title: "#FF7EDB"
        muted: "#848BBD"
        rule: "#495495"
        badge: "#03EDF9"
        duration: "#72F1B8"
        failed: "#FE4450"
        time: "#848BBD"
        text: "#FFFFFF"
        syntax:
            command: "#36F9F6"
            keyword: "#FEDE5D"
            flag: "#F97E72"
            string: "#FF8B39"
            comment: "#848BBD"
            operator: "#FEDE5D"
            variable: "#FF7EDB"
            path: "#72F1B8"
            number: "#F97E72"
            argument: "#FFFFFF"`}
/>

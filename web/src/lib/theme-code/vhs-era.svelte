<script lang="ts">
	import CodeBlock from '$lib/CodeBlock.svelte';

	let { flavorId: _flavorId }: { flavorId: string } = $props();
</script>

<CodeBlock
	lang="yaml"
	meta="path=~/.config/remnix/config.yaml"
	code={`ui:
    colors:
        accent: "#EE5396"
        title: "#C49BFF"
        muted: "#8D8D8D"
        rule: "#393939"
        badge: "#33B1FF"
        duration: "#55E07A"
        failed: "#FF4D6D"
        time: "#8D8D8D"
        text: "#DDE1E6"
        syntax:
            command: "#33B1FF"
            keyword: "#EE5396"
            flag: "#FFB655"
            string: "#55E07A"
            comment: "#8D8D8D"
            operator: "#FF7EB6"
            variable: "#C49BFF"
            path: "#78C6FF"
            number: "#FFD970"
            argument: "#DDE1E6"`}
/>

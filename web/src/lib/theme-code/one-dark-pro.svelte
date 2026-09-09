<script lang="ts">
	import CodeBlock from '$lib/CodeBlock.svelte';

	let { flavorId: _flavorId }: { flavorId: string } = $props();
</script>

<CodeBlock
	lang="yaml"
	code={`ui:
    colors:
        accent: "#C678DD"
        title: "#61AFEF"
        muted: "#5C6370"
        rule: "#3E4451"
        badge: "#61AFEF"
        duration: "#98C379"
        failed: "#E06C75"
        time: "#7F848E"
        text: "#ABB2BF"
        syntax:
            command: "#61AFEF"
            keyword: "#C678DD"
            flag: "#D19A66"
            string: "#98C379"
            comment: "#5C6370"
            operator: "#56B6C2"
            variable: "#E06C75"
            path: "#56B6C2"
            number: "#D19A66"
            argument: "#ABB2BF"`}
/>

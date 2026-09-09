<script lang="ts">
	import CodeBlock from '$lib/CodeBlock.svelte';

	let { flavorId: _flavorId }: { flavorId: string } = $props();
</script>

<CodeBlock
	lang="yaml"
	code={`ui:
    colors:
        accent: "#FF6188"
        title: "#AB9DF2"
        muted: "#727072"
        rule: "#403E41"
        badge: "#78DCE8"
        duration: "#A9DC76"
        failed: "#FF6188"
        time: "#939293"
        text: "#FCFCFA"
        syntax:
            command: "#A9DC76"
            keyword: "#FF6188"
            flag: "#FC9867"
            string: "#FFD866"
            comment: "#727072"
            operator: "#FF6188"
            variable: "#78DCE8"
            path: "#78DCE8"
            number: "#AB9DF2"
            argument: "#FCFCFA"`}
/>

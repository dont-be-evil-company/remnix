<script lang="ts">
	import CodeBlock from '$lib/CodeBlock.svelte';

	let { flavorId: _flavorId }: { flavorId: string } = $props();
</script>

<CodeBlock
	lang="yaml"
	code={`ui:
    colors:
        accent: "#FF79C6"
        title: "#BD93F9"
        muted: "#6272A4"
        rule: "#44475A"
        badge: "#8BE9FD"
        duration: "#50FA7B"
        failed: "#FF5555"
        time: "#6272A4"
        text: "#F8F8F2"
        syntax:
            command: "#50FA7B"
            keyword: "#FF79C6"
            flag: "#FFB86C"
            string: "#F1FA8C"
            comment: "#6272A4"
            operator: "#FF79C6"
            variable: "#8BE9FD"
            path: "#8BE9FD"
            number: "#BD93F9"
            argument: "#F8F8F2"`}
/>

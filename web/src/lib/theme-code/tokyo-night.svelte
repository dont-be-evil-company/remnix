<script lang="ts">
	import CodeBlock from '$lib/CodeBlock.svelte';

	let { flavorId: _flavorId }: { flavorId: string } = $props();
</script>

<CodeBlock
	lang="yaml"
	code={`ui:
    colors:
        accent: "#BB9AF7"
        title: "#7AA2F7"
        muted: "#565F89"
        rule: "#414868"
        badge: "#7AA2F7"
        duration: "#9ECE6A"
        failed: "#F7768E"
        time: "#565F89"
        text: "#C0CAF5"
        syntax:
            command: "#7AA2F7"
            keyword: "#BB9AF7"
            flag: "#FF9E64"
            string: "#9ECE6A"
            comment: "#565F89"
            operator: "#7DCFFF"
            variable: "#C0CAF5"
            path: "#73DACA"
            number: "#FF9E64"
            argument: "#A9B1D6"`}
/>

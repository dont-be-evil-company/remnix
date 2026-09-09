<script lang="ts">
	import CodeBlock from '$lib/CodeBlock.svelte';

	let { flavorId }: { flavorId: string } = $props();
</script>

<div class={flavorId === 'latte' ? '' : 'hidden'}>
	<CodeBlock
		lang="yaml"
		code={`ui:
    colors:
        accent: "#EA76CB"
        title: "#8839EF"
        muted: "#ACB0BE"
        rule: "#CCD0DA"
        badge: "#1E66F5"
        duration: "#40A02B"
        failed: "#D20F39"
        time: "#8C8FA1"
        text: "#4C4F69"
        syntax:
            command: "#1E66F5"
            keyword: "#8839EF"
            flag: "#FE640B"
            string: "#40A02B"
            comment: "#9CA0B0"
            operator: "#D20F39"
            variable: "#04A5E5"
            path: "#179299"
            number: "#DF8E1D"
            argument: "#4C4F69"`}
	/>
</div>
<div class={flavorId === 'frappe' ? '' : 'hidden'}>
	<CodeBlock
		lang="yaml"
		code={`ui:
    colors:
        accent: "#F4B8E4"
        title: "#CA9EE6"
        muted: "#626880"
        rule: "#414559"
        badge: "#8CAAEE"
        duration: "#A6D189"
        failed: "#E78284"
        time: "#838BA7"
        text: "#C6D0F5"
        syntax:
            command: "#8CAAEE"
            keyword: "#CA9EE6"
            flag: "#EF9F76"
            string: "#A6D189"
            comment: "#737994"
            operator: "#E78284"
            variable: "#99D1DB"
            path: "#81C8BE"
            number: "#E5C890"
            argument: "#C6D0F5"`}
	/>
</div>
<div class={flavorId === 'macchiato' ? '' : 'hidden'}>
	<CodeBlock
		lang="yaml"
		code={`ui:
    colors:
        accent: "#F5BDE6"
        title: "#C6A0F6"
        muted: "#5B6078"
        rule: "#363A4F"
        badge: "#8AADF4"
        duration: "#A6DA95"
        failed: "#ED8796"
        time: "#8087A2"
        text: "#CAD3F5"
        syntax:
            command: "#8AADF4"
            keyword: "#C6A0F6"
            flag: "#F5A97F"
            string: "#A6DA95"
            comment: "#6E738D"
            operator: "#ED8796"
            variable: "#91D7E3"
            path: "#8BD5CA"
            number: "#EED49F"
            argument: "#CAD3F5"`}
	/>
</div>
<div class={flavorId === 'mocha' ? '' : 'hidden'}>
	<CodeBlock
		lang="yaml"
		code={`ui:
    colors:
        accent: "#F5C2E7"
        title: "#CBA6F7"
        muted: "#585B70"
        rule: "#313244"
        badge: "#89B4FA"
        duration: "#A6E3A1"
        failed: "#F38BA8"
        time: "#7F849C"
        text: "#CDD6F4"
        syntax:
            command: "#89B4FA"
            keyword: "#CBA6F7"
            flag: "#FAB387"
            string: "#A6E3A1"
            comment: "#6C7086"
            operator: "#F38BA8"
            variable: "#89DCEB"
            path: "#94E2D5"
            number: "#F9E2AF"
            argument: "#CDD6F4"`}
	/>
</div>

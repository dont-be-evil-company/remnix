<script>
	export let data;
</script>

<svelte:head>
	<title>{data.title}</title>
	<meta name="description" content={data.description} />
	<meta property="og:image" content="/opengraph.png" />
	<meta property="og:title" content={data.title} />
</svelte:head>

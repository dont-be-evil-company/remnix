export type Screenshot = {
	slug: string;
	name: string;
	description: string;
	url: string;
};

export const screenshots: Screenshot[] = [
	{
		slug: 'ctrlr-fuzzy-history-search',
		name: 'Ctrlr Fuzzy History Search',
		description:
			'A screenshot showcasing the fuzzy history search feature in Ctrlr, allowing users to quickly find and navigate through their command history.',
		url: '/assets/tapes/cli/ctrlr.gif'
	},
	{
		slug: 'ghost-text',
		name: 'Ghost Text',
		description:
			'A screenshot demonstrating the Ghost Text feature, which provides a subtle and non-intrusive way to display placeholder text or suggestions in your shell.',
		url: '/assets/screenshots/cli/ghost-text.webp'
	},
	{
		slug: 'lsp-style-comp-menu',
		name: 'LSP-Style Comp-Menu',
		description: 'A screenshot illustrating the LSP-Style comp-menu.',
		url: '/assets/screenshots/cli/lsp-style-comp-menu.webp'
	}
];

export function getScreenshot(slug: string): Screenshot | undefined {
	return screenshots.find((screenshots) => screenshots.slug === slug);
}

export function requireScreenshot(slug: string): Screenshot {
	const screenshot = getScreenshot(slug);
	if (!screenshot) {
		throw new Error(`Unknown screenshot: ${slug}`);
	}
	return screenshot;
}

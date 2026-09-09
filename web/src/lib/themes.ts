export type SyntaxColors = {
	command: string;
	keyword: string;
	flag: string;
	string: string;
	comment: string;
	operator: string;
	variable: string;
	path: string;
	number: string;
	argument: string;
};

export type RemnixColors = {
	accent: string;
	title: string;
	muted: string;
	rule: string;
	badge: string;
	duration: string;
	failed: string;
	time: string;
	text: string;
	syntax: SyntaxColors;
};

export type ThemeFlavor = {
	id: string;
	name: string;
	background: string;
	colors: RemnixColors;
	yaml: string;
};

export type Theme = {
	slug: string;
	name: string;
	description: string;
	background: string;
	colors: RemnixColors;
	yaml: string;
	flavors?: ThemeFlavor[];
};

function hex(value: string): string {
	return value.startsWith('#') ? value.toUpperCase() : value;
}

function colorsToYaml(colors: RemnixColors): string {
	return `ui:
    colors:
        accent: "${hex(colors.accent)}"
        title: "${hex(colors.title)}"
        muted: "${hex(colors.muted)}"
        rule: "${hex(colors.rule)}"
        badge: "${hex(colors.badge)}"
        duration: "${hex(colors.duration)}"
        failed: "${hex(colors.failed)}"
        time: "${hex(colors.time)}"
        text: "${hex(colors.text)}"
        syntax:
            command: "${hex(colors.syntax.command)}"
            keyword: "${hex(colors.syntax.keyword)}"
            flag: "${hex(colors.syntax.flag)}"
            string: "${hex(colors.syntax.string)}"
            comment: "${hex(colors.syntax.comment)}"
            operator: "${hex(colors.syntax.operator)}"
            variable: "${hex(colors.syntax.variable)}"
            path: "${hex(colors.syntax.path)}"
            number: "${hex(colors.syntax.number)}"
            argument: "${hex(colors.syntax.argument)}"`;
}

function palette(
	background: string,
	colors: RemnixColors
): { background: string; colors: RemnixColors; yaml: string } {
	return { background, colors, yaml: colorsToYaml(colors) };
}

function flavor(id: string, name: string, background: string, colors: RemnixColors): ThemeFlavor {
	return { id, name, ...palette(background, colors) };
}

const catppuccinLatte = flavor('latte', 'Latte', '#EFF1F5', {
	accent: '#EA76CB',
	title: '#8839EF',
	muted: '#ACB0BE',
	rule: '#CCD0DA',
	badge: '#1E66F5',
	duration: '#40A02B',
	failed: '#D20F39',
	time: '#8C8FA1',
	text: '#4C4F69',
	syntax: {
		command: '#1E66F5',
		keyword: '#8839EF',
		flag: '#FE640B',
		string: '#40A02B',
		comment: '#9CA0B0',
		operator: '#D20F39',
		variable: '#04A5E5',
		path: '#179299',
		number: '#DF8E1D',
		argument: '#4C4F69'
	}
});

const catppuccinFrappe = flavor('frappe', 'Frappé', '#303446', {
	accent: '#F4B8E4',
	title: '#CA9EE6',
	muted: '#626880',
	rule: '#414559',
	badge: '#8CAAEE',
	duration: '#A6D189',
	failed: '#E78284',
	time: '#838BA7',
	text: '#C6D0F5',
	syntax: {
		command: '#8CAAEE',
		keyword: '#CA9EE6',
		flag: '#EF9F76',
		string: '#A6D189',
		comment: '#737994',
		operator: '#E78284',
		variable: '#99D1DB',
		path: '#81C8BE',
		number: '#E5C890',
		argument: '#C6D0F5'
	}
});

const catppuccinMacchiato = flavor('macchiato', 'Macchiato', '#24273A', {
	accent: '#F5BDE6',
	title: '#C6A0F6',
	muted: '#5B6078',
	rule: '#363A4F',
	badge: '#8AADF4',
	duration: '#A6DA95',
	failed: '#ED8796',
	time: '#8087A2',
	text: '#CAD3F5',
	syntax: {
		command: '#8AADF4',
		keyword: '#C6A0F6',
		flag: '#F5A97F',
		string: '#A6DA95',
		comment: '#6E738D',
		operator: '#ED8796',
		variable: '#91D7E3',
		path: '#8BD5CA',
		number: '#EED49F',
		argument: '#CAD3F5'
	}
});

const catppuccinMocha = flavor('mocha', 'Mocha', '#1E1E2E', {
	accent: '#F5C2E7',
	title: '#CBA6F7',
	muted: '#585B70',
	rule: '#313244',
	badge: '#89B4FA',
	duration: '#A6E3A1',
	failed: '#F38BA8',
	time: '#7F849C',
	text: '#CDD6F4',
	syntax: {
		command: '#89B4FA',
		keyword: '#CBA6F7',
		flag: '#FAB387',
		string: '#A6E3A1',
		comment: '#6C7086',
		operator: '#F38BA8',
		variable: '#89DCEB',
		path: '#94E2D5',
		number: '#F9E2AF',
		argument: '#CDD6F4'
	}
});

const dracula = palette('#282A36', {
	accent: '#FF79C6',
	title: '#BD93F9',
	muted: '#6272A4',
	rule: '#44475A',
	badge: '#8BE9FD',
	duration: '#50FA7B',
	failed: '#FF5555',
	time: '#6272A4',
	text: '#F8F8F2',
	syntax: {
		command: '#50FA7B',
		keyword: '#FF79C6',
		flag: '#FFB86C',
		string: '#F1FA8C',
		comment: '#6272A4',
		operator: '#FF79C6',
		variable: '#8BE9FD',
		path: '#8BE9FD',
		number: '#BD93F9',
		argument: '#F8F8F2'
	}
});

const oneDarkPro = palette('#282C34', {
	accent: '#C678DD',
	title: '#61AFEF',
	muted: '#5C6370',
	rule: '#3E4451',
	badge: '#61AFEF',
	duration: '#98C379',
	failed: '#E06C75',
	time: '#7F848E',
	text: '#ABB2BF',
	syntax: {
		command: '#61AFEF',
		keyword: '#C678DD',
		flag: '#D19A66',
		string: '#98C379',
		comment: '#5C6370',
		operator: '#56B6C2',
		variable: '#E06C75',
		path: '#56B6C2',
		number: '#D19A66',
		argument: '#ABB2BF'
	}
});

const tokyoNight = palette('#1A1B26', {
	accent: '#BB9AF7',
	title: '#7AA2F7',
	muted: '#565F89',
	rule: '#414868',
	badge: '#7AA2F7',
	duration: '#9ECE6A',
	failed: '#F7768E',
	time: '#565F89',
	text: '#C0CAF5',
	syntax: {
		command: '#7AA2F7',
		keyword: '#BB9AF7',
		flag: '#FF9E64',
		string: '#9ECE6A',
		comment: '#565F89',
		operator: '#7DCFFF',
		variable: '#C0CAF5',
		path: '#73DACA',
		number: '#FF9E64',
		argument: '#A9B1D6'
	}
});

const synthwave84 = palette('#262335', {
	accent: '#FF7EDB',
	title: '#FF7EDB',
	muted: '#848BBD',
	rule: '#495495',
	badge: '#03EDF9',
	duration: '#72F1B8',
	failed: '#FE4450',
	time: '#848BBD',
	text: '#FFFFFF',
	syntax: {
		command: '#36F9F6',
		keyword: '#FEDE5D',
		flag: '#F97E72',
		string: '#FF8B39',
		comment: '#848BBD',
		operator: '#FEDE5D',
		variable: '#FF7EDB',
		path: '#72F1B8',
		number: '#F97E72',
		argument: '#FFFFFF'
	}
});

const monokaiPro = palette('#2D2A2E', {
	accent: '#FF6188',
	title: '#AB9DF2',
	muted: '#727072',
	rule: '#403E41',
	badge: '#78DCE8',
	duration: '#A9DC76',
	failed: '#FF6188',
	time: '#939293',
	text: '#FCFCFA',
	syntax: {
		command: '#A9DC76',
		keyword: '#FF6188',
		flag: '#FC9867',
		string: '#FFD866',
		comment: '#727072',
		operator: '#FF6188',
		variable: '#78DCE8',
		path: '#78DCE8',
		number: '#AB9DF2',
		argument: '#FCFCFA'
	}
});

export const themes: Theme[] = [
	{
		slug: 'dracula-official',
		name: 'Dracula Official',
		description:
			'A dark theme with bold, vivid purples, pinks, and greens that works across dozens of editors and has millions of downloads.',
		...dracula
	},
	{
		slug: 'one-dark-pro',
		name: 'One Dark Pro',
		description:
			"Based on Atom's iconic dark design, this is a clean, classic default choice for many web and software developers.",
		...oneDarkPro
	},
	{
		slug: 'tokyo-night',
		name: 'Tokyo Night',
		description:
			'A sleek dark theme capturing the neon lights of Tokyo at night with soft blue and purple tones.',
		...tokyoNight
	},
	{
		slug: 'synthwave-84',
		name: "SynthWave '84",
		description:
			'A glowing, 1980s synthwave-inspired aesthetic featuring neon pinks and cyan glows for a retro-futuristic look.',
		...synthwave84
	},
	{
		slug: 'catppuccin',
		name: 'Catppuccin',
		description:
			'A community-driven pastel theme offering four soothing, creamy shades that are gentle during long night sessions.',
		background: catppuccinMocha.background,
		colors: catppuccinMocha.colors,
		yaml: catppuccinMocha.yaml,
		flavors: [catppuccinLatte, catppuccinFrappe, catppuccinMacchiato, catppuccinMocha]
	},
	{
		slug: 'monokai-pro',
		name: 'Monokai Pro',
		description:
			'A refined, professional spin on the classic Monokai color palette with carefully filtered, distraction-free tones.',
		...monokaiPro
	}
];

export function getTheme(slug: string): Theme | undefined {
	return themes.find((theme) => theme.slug === slug);
}

export function requireTheme(slug: string): Theme {
	const theme = getTheme(slug);
	if (!theme) {
		throw new Error(`Unknown theme: ${slug}`);
	}
	return theme;
}

import Prism from 'prismjs';
import 'prismjs/plugins/toolbar/prism-toolbar';
import 'prismjs/plugins/copy-to-clipboard/prism-copy-to-clipboard';
import 'prismjs/components/prism-yaml';
import 'prismjs/themes/prism-okaidia.css';

interface PrismRegisterButtonContext {
	element: { parentNode: HTMLElement | null };
}

let buttonRegistered = false;

export function highlightCode() {
	if (typeof window === 'undefined') {
		return;
	}

	if (!buttonRegistered && Prism.plugins.toolbar) {
		Prism.plugins.toolbar.registerButton(
			'fullscreen-code',
			function (ctx: PrismRegisterButtonContext) {
				const button = document.createElement('button');
				button.innerHTML = '🔍';
				button.addEventListener('click', function () {
					ctx.element.parentNode?.requestFullscreen();
				});
				return button;
			}
		);
		buttonRegistered = true;
	}

	Prism.highlightAll();
}

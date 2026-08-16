import { redirect } from '@sveltejs/kit';

export function load() {
	redirect(302, 'https://github.com/dont-be-evil-company/remnix/releases/latest');
}

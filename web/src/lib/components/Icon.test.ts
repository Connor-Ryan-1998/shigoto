import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import Icon from './Icon.svelte';

describe('Icon', () => {
	it('is decorative by default', () => {
		const { container } = render(Icon, { name: 'invoices' });
		const svg = container.querySelector('svg');
		expect(svg?.getAttribute('aria-hidden')).toBe('true');
		expect(svg?.getAttribute('stroke')).toBe('currentColor');
		expect(svg?.getAttribute('width')).toBe('18');
		expect(svg?.querySelector('path, rect, circle')).toBeTruthy();
	});

	it('exposes a label when given one', () => {
		render(Icon, { name: 'invoices', label: 'Overdue' });
		expect(screen.getByRole('img', { name: 'Overdue' })).toBeTruthy();
	});
});

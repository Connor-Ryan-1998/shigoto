import { describe, expect, it } from 'vitest';
import { apiMock, deferred } from '../../test/mock-api';
import { render, screen } from '@testing-library/svelte';

import ProjectCosts from './ProjectCosts.svelte';

const costs = (over: Record<string, unknown> = {}) => ({
	quoted_labour: 30000, quoted_materials: 18000, quoted_total: 48000, invoiced: 20000, expenses_total: 10000,
	expenses: [{ expense_id: 4, spent_on: '2026-12-01', description: 'Switchboard parts', category: 'Equipment', supplier: 'Sparky Supplies', amount: 10000 }],
	...over
});

describe('ProjectCosts', () => {
	it('compares quoted, invoiced and spent, ex GST', async () => {
		apiMock.mockResolvedValue(costs());
		render(ProjectCosts, { org: 'acme', projectId: 5 });
		expect(await screen.findByText('All amounts exclude GST.')).toBeTruthy();
		expect(apiMock).toHaveBeenCalledWith('GET', '/api/v1/orgs/acme/projects/5/costs');
		expect(screen.getByText('$480.00')).toBeTruthy();
		expect(screen.getByText('$200.00')).toBeTruthy();
		expect(screen.getByText('$380.00')).toBeTruthy();
		expect(screen.getByRole('link', { name: 'Switchboard parts' }).getAttribute('href')).toBe('/acme/expenses/4');
	});

	it('flags spending over the quote', async () => {
		apiMock.mockResolvedValue(costs({ quoted_total: 5000, expenses_total: 8000 }));
		render(ProjectCosts, { org: 'acme', projectId: 5 });
		const over = await screen.findByText('$30.00 over quote');
		expect(over.className).toContain('text-error');
	});

	it('ignores a response for a project it no longer shows', async () => {
		const first = deferred();
		apiMock.mockReturnValueOnce(first.promise).mockResolvedValueOnce(costs({ quoted_total: 100, quoted_labour: 100, quoted_materials: 0 }));
		const { rerender } = render(ProjectCosts, { org: 'acme', projectId: 5 });
		await rerender({ org: 'acme', projectId: 6 });
		await screen.findByText('$1.00', { selector: '.stat-value' });
		first.resolve(costs());
		await Promise.resolve();
		expect(screen.queryByText('$480.00')).toBeNull();
	});
});

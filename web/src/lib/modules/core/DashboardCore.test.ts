import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import DashboardCore from './DashboardCore.svelte';
import { signIn } from '../../test/session';

const core = {
	open_count: 3,
	open_outstanding: 123456,
	overdue_count: 1,
	overdue_outstanding: 7890,
	draft_count: 2,
	invoice_count: 7
};

describe('DashboardCore', () => {
	it('renders the receivables split and org-scoped invoice links', () => {
		render(DashboardCore, { core, org: 'acme' });
		expect(screen.getByRole('heading', { name: 'Total Receivables' })).toBeTruthy();
		expect(screen.getByText('$1,234.56')).toBeTruthy();
		expect(screen.getByText('$1,155.66')).toBeTruthy();
		expect(screen.getByText('$78.90')).toBeTruthy();
		expect(screen.getByRole('link', { name: /current/i }).getAttribute('href')).toBe('/acme/invoices?status=open');
		expect(screen.getByRole('link', { name: /overdue/i }).getAttribute('href')).toBe('/acme/invoices?status=overdue');
		expect(screen.queryByText('Drafts')).toBeNull();
	});

	it('renders a finite neutral bar at zero balance', () => {
		const { container } = render(DashboardCore, { core: { ...core, open_outstanding: 0, overdue_outstanding: 0 }, org: 'acme' });
		expect(screen.getByRole('heading', { name: 'Total Receivables' })).toBeTruthy();
		expect(container.innerHTML).not.toMatch(/NaN|Infinity/);
		expect(screen.getByRole('link', { name: /current.*\$0.00/i })).toBeTruthy();
	});

	it('can show an entirely overdue balance', () => {
		render(DashboardCore, { core: { ...core, overdue_outstanding: core.open_outstanding }, org: 'acme' });
		expect(screen.getByRole('link', { name: /current.*\$0.00/i })).toBeTruthy();
		expect(screen.getByRole('link', { name: /overdue.*\$1,234.56/i })).toBeTruthy();
	});

	it('offers invoice creation only with write permission', () => {
		signIn();
		render(DashboardCore, { core, org: 'acme' });
		expect(screen.getByRole('link', { name: /new invoice/i }).getAttribute('href')).toBe('/acme/invoices/new');
	});

	it('hides invoice creation for read-only users', () => {
		signIn({ role: 'bookkeeper', perms: ['invoices_read'] });
		render(DashboardCore, { core, org: 'acme' });
		expect(screen.queryByRole('link', { name: /new invoice/i })).toBeNull();
	});
});

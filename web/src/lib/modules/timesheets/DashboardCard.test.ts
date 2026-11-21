import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import DashboardCard from './DashboardCard.svelte';

const data = {
	unbilled_minutes: 1095, unbilled_estimate: 219000,
	project_count: 1,
	projects: [{ id: 7, name: 'HTM Data Platform', client_name: 'Rizzware', billed_minutes: 90, unbilled_minutes: 30 }]
};

describe('Projects dashboard card', () => {
	it('shows header totals independently of the displayed rows and labels billed progress', () => {
		render(DashboardCard, { data, org: 'acme' });
		expect(screen.getByRole('heading', { name: 'Projects' })).toBeTruthy();
		expect(screen.getByText('18:15')).toBeTruthy();
		expect(screen.getByText('$2,190.00')).toBeTruthy();
		expect(screen.getByRole('progressbar', { name: /HTM Data Platform.*billed/i }).getAttribute('aria-valuenow')).toBe('75');
		expect(screen.getByText('75%')).toBeTruthy();
		expect(screen.getByText('Rizzware')).toBeTruthy();
		expect(screen.getByRole('link', { name: /HTM Data Platform/i }).getAttribute('href')).toBe('/acme/projects/7');
		expect(screen.queryByRole('link', { name: 'View all projects' })).toBeNull();
	});

	it('offers all projects when the server has more than five', () => {
		render(DashboardCard, { data: { ...data, project_count: 6 }, org: 'acme' });
		expect(screen.getByRole('link', { name: 'View all projects' }).getAttribute('href')).toBe('/acme/projects');
	});

	it('explains an empty active-project list', () => {
		render(DashboardCard, { data: { unbilled_minutes: 0, unbilled_estimate: 0, project_count: 0, projects: [] }, org: 'acme' });
		expect(screen.getByText('No active projects with unbilled time.')).toBeTruthy();
		expect(screen.queryByRole('progressbar')).toBeNull();
	});
});

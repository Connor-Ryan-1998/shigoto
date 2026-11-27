import { describe, expect, it, vi } from 'vitest';
import { apiMock } from '../../test/mock-api';
import { fireEvent, render, screen } from '@testing-library/svelte';

import ProjectDialog from './ProjectDialog.svelte';
import type { Project } from './types';

const project: Project = {
	id: 5, client_contact_id: 9, name: 'Website', hourly_rate: 12000, gst_code: 'GST', status: 'in_progress',
	description: 'Old', start_date: '2026-01-01', end_date: null, billing_method: 'tm', active: true
};
const contacts = [{ id: 9, name: 'Rizzware', kind: 'client' }, { id: 10, name: 'Other', kind: 'client' }, { id: 11, name: 'Tenant', kind: 'tenant' }];

describe('ProjectDialog', () => {
	it('sends a full replace on edit', async () => {
		apiMock.mockResolvedValue(project);
		const onsaved = vi.fn();
		render(ProjectDialog, { open: true, org: 'acme', contacts, project, onsaved });
		expect(screen.queryByRole('option', { name: 'Tenant' })).toBeNull();
		await fireEvent.change(screen.getByLabelText('Status'), { target: { value: 'completed' } });
		await fireEvent.change(screen.getByLabelText('Billing method'), { target: { value: 'fixed' } });
		await fireEvent.input(screen.getByLabelText('Base rate ($/h)'), { target: { value: '150.50' } });
		await fireEvent.input(screen.getByLabelText('Start date'), { target: { value: '' } });
		await fireEvent.click(screen.getByRole('button', { name: 'Save project' }));
		await vi.waitFor(() => expect(onsaved).toHaveBeenCalled());
		expect(apiMock).toHaveBeenCalledWith('PUT', '/api/v1/orgs/acme/projects/5', {
			name: 'Website', client_contact_id: 9, hourly_rate: 15050, gst_code: 'GST', status: 'completed',
			description: 'Old', start_date: null, end_date: null, billing_method: 'fixed',
			summary: '', goals: '', stakeholders: ''
		});
	});

	it('edits the client-facing summary, goals and stakeholders', async () => {
		apiMock.mockResolvedValue(project);
		const onsaved = vi.fn();
		render(ProjectDialog, { open: true, org: 'acme', contacts, project: { ...project, summary: 'Old summary' }, onsaved });
		expect((screen.getByLabelText('Client summary') as HTMLTextAreaElement).value).toBe('Old summary');
		await fireEvent.input(screen.getByLabelText('Client summary'), { target: { value: 'Rebuild the site' } });
		await fireEvent.input(screen.getByLabelText('Goals'), { target: { value: 'Bookings live\nFast mobile' } });
		await fireEvent.input(screen.getByLabelText('Primary stakeholders'), { target: { value: 'Priya Nair — Sponsor' } });
		await fireEvent.click(screen.getByRole('button', { name: 'Save project' }));
		await vi.waitFor(() => expect(onsaved).toHaveBeenCalled());
		expect(apiMock.mock.calls[0][2]).toMatchObject({
			summary: 'Rebuild the site', goals: 'Bookings live\nFast mobile', stakeholders: 'Priya Nair — Sponsor'
		});
	});

	it('rejects a bad rate without calling the API', async () => {
		render(ProjectDialog, { open: true, org: 'acme', contacts, onsaved: vi.fn() });
		await fireEvent.input(screen.getByLabelText('Project name'), { target: { value: 'New' } });
		await fireEvent.input(screen.getByLabelText('Base rate ($/h)'), { target: { value: '0' } });
		await fireEvent.click(screen.getByRole('button', { name: 'Create project' }));
		await screen.findByText('Base rate must be greater than zero.');
		expect(apiMock).not.toHaveBeenCalled();
	});

	it('lets a fixed-price project have no rate', async () => {
		apiMock.mockResolvedValue({ ...project, hourly_rate: null, billing_method: 'fixed' });
		const onsaved = vi.fn();
		render(ProjectDialog, { open: true, org: 'acme', contacts, onsaved });
		await fireEvent.input(screen.getByLabelText('Project name'), { target: { value: 'Quoted job' } });
		await fireEvent.change(screen.getByLabelText('Billing method'), { target: { value: 'fixed' } });
		await fireEvent.click(screen.getByRole('button', { name: 'Create project' }));
		await vi.waitFor(() => expect(onsaved).toHaveBeenCalled());
		expect(apiMock.mock.calls[0][2]).toMatchObject({ hourly_rate: null, billing_method: 'fixed' });
	});

	it('shows a null rate as blank when editing', async () => {
		render(ProjectDialog, { open: true, org: 'acme', contacts, project: { ...project, hourly_rate: null, billing_method: 'fixed' }, onsaved: vi.fn() });
		expect((screen.getByLabelText('Base rate ($/h)') as HTMLInputElement).value).toBe('');
	});

	it('still needs a rate for time and materials', async () => {
		render(ProjectDialog, { open: true, org: 'acme', contacts, onsaved: vi.fn() });
		await fireEvent.input(screen.getByLabelText('Project name'), { target: { value: 'Hourly' } });
		await fireEvent.click(screen.getByRole('button', { name: 'Create project' }));
		await screen.findByText('Enter an hourly rate.');
		expect(apiMock).not.toHaveBeenCalled();
	});
});

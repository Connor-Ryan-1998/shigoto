import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, within } from '@testing-library/svelte';
import { setPage } from '../../test/app.svelte';
import { apiError, apiMock, mockApi } from '../../test/mock-api';
import { signIn } from '../../test/session';
import StrataSection from './StrataSection.svelte';
import type { PropertyDetail, StrataScheme } from './types';

const schemes = '/api/v1/orgs/acme/strata-schemes';
const link = '/api/v1/orgs/acme/properties/1/strata';
const harbour: StrataScheme = {
	id: 3, name: 'Harbour View CTS', cts_number: '12345', is_chairperson: false, is_secretary: true, is_treasurer: false,
	properties: [{ id: 1, name: 'Cottage' }]
};
const bayside: StrataScheme = {
	id: 4, name: 'Bayside', cts_number: '', is_chairperson: false, is_secretary: false, is_treasurer: false, properties: []
};
const property = (strata_scheme: StrataScheme | null): PropertyDetail => ({
	id: 1, name: 'Cottage', address: '1 Main St', kind: 'house', residential: true, contract_date: null, settlement_date: null,
	purchase_price: null, stamp_duty: null, purchase_legal_costs: null, year_built: null, lot_plan: '', land_area_m2: null,
	council_account_ref: '', water_account_ref: '', notes: '', current_tenancy: null, strata_scheme
});

let onsaved: ReturnType<typeof vi.fn<(scheme: StrataScheme | null) => void>>;
function mount(p: PropertyDetail) {
	onsaved = vi.fn<(scheme: StrataScheme | null) => void>();
	render(StrataSection, { props: { org: 'acme', property: p, onsaved } });
}
const toggle = () => screen.getByLabelText('Part of a strata scheme', { exact: true });
const save = () => fireEvent.click(screen.getByRole('button', { name: 'Save strata' }));

beforeEach(() => {
	setPage({ params: { org: 'acme', id: '1' } });
	signIn();
});

describe('strata section', () => {
	it('links an unlinked property to an existing scheme', async () => {
		const linked = { ...harbour, properties: [{ id: 1, name: 'Cottage' }] };
		mockApi({ [`GET ${schemes}`]: [harbour, bayside], [`PUT ${link}`]: { strata_scheme: linked } });
		mount(property(null));
		await fireEvent.click(toggle());
		const select = await screen.findByLabelText('Scheme', { exact: true });
		await fireEvent.change(select, { target: { value: '3' } });
		await save();
		await vi.waitFor(() => expect(onsaved).toHaveBeenCalledWith(linked));
		expect(apiMock).toHaveBeenCalledWith('PUT', link, { strata_scheme_id: 3 });
		expect(apiMock.mock.calls.filter(([m]) => m === 'PUT')).toHaveLength(1);
		expect(await screen.findByText('Strata saved.')).toBeTruthy();
	});

	it('unlinks without touching the scheme', async () => {
		mockApi({ [`GET ${schemes}`]: [harbour], [`PUT ${link}`]: { strata_scheme: null } });
		mount(property(harbour));
		await screen.findByLabelText('Scheme name', { exact: true });
		await fireEvent.click(toggle());
		await save();
		await vi.waitFor(() => expect(onsaved).toHaveBeenCalledWith(null));
		expect(apiMock.mock.calls.filter(([m]) => m === 'PUT')).toEqual([['PUT', link, { strata_scheme_id: null }]]);
	});

	it('saves scheme edits without relinking', async () => {
		const saved = { ...harbour, name: 'Harbour View', is_treasurer: true };
		mockApi({ [`GET ${schemes}`]: [harbour], [`PUT ${schemes}/3`]: saved });
		mount(property(harbour));
		await fireEvent.input(await screen.findByLabelText('Scheme name', { exact: true }), { target: { value: 'Harbour View' } });
		await fireEvent.click(screen.getByLabelText('Treasurer', { exact: true }));
		await save();
		await vi.waitFor(() => expect(onsaved).toHaveBeenCalledWith(saved));
		expect(apiMock.mock.calls.filter(([m]) => m === 'PUT')).toEqual([['PUT', `${schemes}/3`, {
			name: 'Harbour View', cts_number: '12345', is_chairperson: false, is_secretary: true, is_treasurer: true
		}]]);
	});

	it('requires a scheme when enabled', async () => {
		mockApi({ [`GET ${schemes}`]: [harbour] });
		mount(property(null));
		await fireEvent.click(toggle());
		await screen.findByLabelText('Scheme', { exact: true });
		await save();
		expect(await screen.findByText('Choose or create a scheme.')).toBeTruthy();
		expect(apiMock.mock.calls.filter(([m]) => m === 'PUT')).toHaveLength(0);
	});

	it('shows a duplicate CTS number against its field', async () => {
		mockApi({ [`GET ${schemes}`]: [harbour],
			[`PUT ${schemes}/3`]: apiError(422, 'invalid input: cts_number is already used by another scheme in this organisation') });
		mount(property(harbour));
		const cts = await screen.findByLabelText('CTS number', { exact: true });
		await fireEvent.input(cts, { target: { value: '999' } });
		await save();
		expect(await screen.findByText('Another scheme already uses this CTS number.')).toBeTruthy();
		expect((cts as HTMLInputElement).value).toBe('999');
		expect(cts.getAttribute('aria-invalid')).toBe('true');
	});

	it('shows other properties sharing the scheme', async () => {
		const shared = { ...harbour, properties: [{ id: 1, name: 'Cottage' }, { id: 2, name: 'Unit 2' }] };
		mockApi({ [`GET ${schemes}`]: [shared] });
		mount(property(shared));
		expect(await screen.findByText('Shared with: Unit 2. Changes apply to every linked property.')).toBeTruthy();
	});

	it('creates a scheme from the dialog and selects it', async () => {
		mockApi({ [`GET ${schemes}`]: [harbour], [`POST ${schemes}`]: bayside });
		mount(property(null));
		await fireEvent.click(toggle());
		await fireEvent.click(await screen.findByRole('button', { name: 'New scheme…' }));
		const dialog = screen.getByRole('dialog', { name: 'New strata scheme' });
		await fireEvent.input(within(dialog).getByLabelText('Scheme name', { exact: true }), { target: { value: 'Bayside' } });
		await fireEvent.click(within(dialog).getByRole('button', { name: 'Create scheme' }));
		await vi.waitFor(() => expect((screen.getByLabelText('Scheme', { exact: true }) as HTMLSelectElement).value).toBe('4'));
		expect(apiMock).toHaveBeenCalledWith('POST', schemes, expect.objectContaining({ name: 'Bayside' }));
		expect(apiMock.mock.calls.filter(([m]) => m === 'PUT')).toHaveLength(0);
	});

	it('is read-only without property_write', async () => {
		signIn({ role: 'bookkeeper', perms: ['property_read'] });
		mockApi({});
		mount(property(harbour));
		expect(await screen.findByDisplayValue('Harbour View CTS')).toBeTruthy();
		expect(screen.queryByRole('button', { name: 'Save strata' })).toBeNull();
		expect(toggle().matches(':disabled')).toBe(true);
	});
});

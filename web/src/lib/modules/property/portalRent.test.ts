import { describe, expect, it } from 'vitest';
import { fyRange, statementHref } from './portalRent';

describe('portal rent helpers', () => {
	it('maps a financial year to its first and last day', () => {
		expect(fyRange(2027)).toEqual({ from: '2026-07-01', to: '2027-06-30' });
	});

	it('builds statement download links with the range', () => {
		expect(statementHref('acme', 5, 'pdf', { from: '2026-07-01', to: '2027-06-30' })).toBe(
			'/api/v1/portal/acme/leases/5/statement.pdf?from=2026-07-01&to=2027-06-30'
		);
	});
});

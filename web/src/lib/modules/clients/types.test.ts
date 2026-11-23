import { describe, expect, it } from 'vitest';
import { documentActions, statusLabel } from './types';

describe('client document helpers', () => {
	it('offers only actions valid for each status', () => {
		expect(documentActions({ kind: 'signature', status: 'draft' })).toEqual(['send', 'delete']);
		expect(documentActions({ kind: 'signature', status: 'sent' })).toEqual(['withdraw', 'replace']);
		expect(documentActions({ kind: 'shared', status: 'sent' })).toEqual(['withdraw', 'replace']);
		for (const status of ['signed', 'withdrawn', 'superseded'] as const) {
			expect(documentActions({ kind: 'signature', status })).toEqual([]);
		}
	});

	it('labels sent documents by kind', () => {
		expect(statusLabel({ kind: 'signature', status: 'sent' })).toBe('Awaiting signature');
		expect(statusLabel({ kind: 'shared', status: 'sent' })).toBe('Shared');
		expect(statusLabel({ kind: 'signature', status: 'superseded' })).toBe('Replaced');
	});
});

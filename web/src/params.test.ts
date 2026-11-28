import { describe, expect, it } from 'vitest';
import { params } from './params';

const matches = (value: string) => !(params.org['~standard'].validate(value) as { issues?: unknown }).issues;

describe('org param matcher', () => {
	it('accepts organisation slugs', () => {
		expect(matches('acme')).toBe(true);
		expect(matches('kaizen-atelier')).toBe(true);
	});

	it('rejects api so /api/... links load from the server instead of the SPA', () => {
		expect(matches('api')).toBe(false);
	});
});

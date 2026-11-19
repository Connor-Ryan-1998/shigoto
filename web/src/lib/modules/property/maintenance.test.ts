import { describe, expect, it } from 'vitest';
import { visitWindow, windowLabel, localParts } from './maintenance';

describe('maintenance formatting', () => {
	it('shows visit times as written in org time, whatever the viewer timezone', () => {
		expect(visitWindow('2026-11-24T09:00:00+10:00', '2026-11-24T11:30:00+10:00')).toBe('Tue 24 Nov, 9am – 11:30am');
		expect(visitWindow('2026-11-30T23:00:00+10:00', '2026-12-01T01:00:00+10:00')).toBe('Mon 30 Nov, 11pm – 1 Dec, 1am');
	});
	it('labels availability windows', () => {
		expect(windowLabel({ date: '2026-11-24', slot: 'morning', start: null, end: null })).toBe('Tue 24 Nov · Morning (8am–12pm)');
		expect(windowLabel({ date: '2026-11-26', slot: 'custom', start: '15:00', end: '17:30' })).toBe('Thu 26 Nov · 3pm – 5:30pm');
	});
	it('splits an org-time instant for inputs', () => {
		expect(localParts('2026-11-24T09:05:00+10:00')).toEqual({ date: '2026-11-24', time: '09:05' });
	});
});

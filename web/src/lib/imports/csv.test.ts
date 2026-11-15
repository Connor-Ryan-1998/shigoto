import { describe, expect, it } from 'vitest';
import { guessMapping, readSample } from './csv';

describe('readSample', () => {
	it('reads headers and rows with quotes, escaped quotes, BOM and CRLF', () => {
		const text = '﻿Invoice,"Customer, Name",Note\r\nA-1,"Acme, Pty","say ""hi"""\r\n\r\nA-2,Beta,\r\n';
		expect(readSample(text)).toEqual({
			headers: ['Invoice', 'Customer, Name', 'Note'],
			rows: [
				['A-1', 'Acme, Pty', 'say "hi"'],
				['A-2', 'Beta', '']
			]
		});
	});

	it('stops after limit rows', () => {
		const text = 'A\n1\n2\n3\n';
		expect(readSample(text, 2).rows).toEqual([['1'], ['2']]);
	});

	it('handles a quoted newline inside a cell', () => {
		expect(readSample('A,B\n"x\ny",z\n').rows).toEqual([['x\ny', 'z']]);
	});

	it('returns no headers for an empty file', () => {
		expect(readSample('')).toEqual({ headers: [], rows: [] });
	});
});

describe('guessMapping', () => {
	it('matches a header equal to a field label or key, ignoring case and spaces', () => {
		const fields = [
			{ key: 'number', label: 'Invoice number', required: true },
			{ key: 'gst', label: 'GST', required: true },
			{ key: 'paid_date', label: 'Paid date', required: false }
		];
		expect(guessMapping(fields, [' invoice NUMBER ', 'gst', 'Other'])).toEqual({
			number: ' invoice NUMBER ',
			gst: 'gst',
			paid_date: ''
		});
	});
});

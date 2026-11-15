import type { ImportField } from './types';

// readSample parses a CSV's header row and up to limit non-blank data
// rows, only to offer the headers for mapping and show a few sample rows.
// The server re-parses the whole file; this never decides what is
// imported. Handles quoted cells, doubled quotes, quoted newlines, a
// leading BOM and CRLF line endings.
export function readSample(text: string, limit = 5): { headers: string[]; rows: string[][] } {
	const records: string[][] = [];
	let row: string[] = [];
	let cell = '';
	let quoted = false;
	let i = text.charCodeAt(0) === 0xfeff ? 1 : 0;

	const endRow = () => {
		row.push(cell);
		cell = '';
		if (row.some((c) => c.trim() !== '')) records.push(row);
		row = [];
	};

	for (; i < text.length && records.length <= limit; i++) {
		const ch = text[i];
		if (quoted) {
			if (ch === '"' && text[i + 1] === '"') {
				cell += '"';
				i++;
			} else if (ch === '"') {
				quoted = false;
			} else {
				cell += ch;
			}
		} else if (ch === '"') {
			quoted = true;
		} else if (ch === ',') {
			row.push(cell);
			cell = '';
		} else if (ch === '\n' || ch === '\r') {
			if (ch === '\r' && text[i + 1] === '\n') i++;
			endRow();
		} else {
			cell += ch;
		}
	}
	if (records.length <= limit && (cell !== '' || row.length > 0)) endRow();

	const [headers = [], ...rows] = records;
	return { headers, rows: rows.slice(0, limit) };
}

const norm = (s: string) => s.trim().toLowerCase();

// guessMapping maps each field to the first header equal to its label or
// key (trimmed, case-insensitive), or '' (unmapped).
export function guessMapping(fields: ImportField[], headers: string[]): Record<string, string> {
	const mapping: Record<string, string> = {};
	for (const field of fields) {
		mapping[field.key] = headers.find((h) => norm(h) === norm(field.label) || norm(h) === norm(field.key)) ?? '';
	}
	return mapping;
}

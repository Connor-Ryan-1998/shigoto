import type { ImportProblem } from '../api';

export type { ImportProblem };

// ImportField is one field a CSV import maps a column to.
export type ImportField = { key: string; label: string; required: boolean };

// ImportSummary is a CSV import preview's or apply's response.
export type ImportSummary = {
	new: number;
	existing: number;
	total_cents?: number;
	total_minutes?: number;
	problems: ImportProblem[];
};

// The date layouts the server accepts (Go layouts), with display labels.
export const dateFormats = [
	{ value: '02/01/2006', label: 'DD/MM/YYYY' },
	{ value: '2006-01-02', label: 'YYYY-MM-DD' },
	{ value: '2 Jan 2006', label: 'D Mon YYYY' }
];

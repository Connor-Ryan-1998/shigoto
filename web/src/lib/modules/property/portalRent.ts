// Types and helpers for the portal Rent tab
// (internal/portal/rent.go's account and receipt responses).

export type PortalLease = {
	id: number;
	property_name: string;
	property_address: string;
	start_date: string;
	end_date: string | null;
	active: boolean;
};

export type AccountEntry = {
	date: string;
	kind: 'charge' | 'payment' | 'write_off' | 'write_off_reversed';
	description: string;
	debit: number;
	credit: number;
	balance: number;
	payment_id?: number;
};

export type LeaseAccount = {
	lease: { id: number; property_address: string; start_date: string; end_date: string | null; frequency: string; rent_amount: number };
	summary: { paid_to: string | null; owing_now: number; next_due: { date: string; amount: number } | null; balance: number };
	entries: AccountEntry[];
};

export type Receipt = {
	org_name: string;
	org_abn: string;
	tenant_names: string[];
	property_address: string;
	payment: { id: number; amount: number; paid_on: string; method: string; reference: string };
	invoice: { id: number; number: number | null; total: number; period: string };
	balance_after: number;
	generated_at: string;
};

export type DateRange = { from: string; to: string };

export const METHOD_LABEL: Record<string, string> = {
	stripe: 'Card',
	bank_transfer: 'Bank transfer',
	payid: 'PayID',
	cash: 'Cash',
	other: 'Other'
};

// fyRange is the first and last day of the July-start financial year fy
// (named by its end year, as lib/modules/property/fy.ts does).
export function fyRange(fy: number): DateRange {
	return { from: `${fy - 1}-07-01`, to: `${fy}-06-30` };
}

export function statementHref(org: string, leaseId: number, format: 'pdf' | 'csv', range: DateRange): string {
	const q = new URLSearchParams({ from: range.from, to: range.to });
	return `/api/v1/portal/${org}/leases/${leaseId}/statement.${format}?${q}`;
}

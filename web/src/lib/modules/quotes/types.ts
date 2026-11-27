// Shapes of the quotes API (internal/quotes/routes.go).

export type QuoteLineKind = 'labour' | 'material';

// QuoteLine is a line as the create/update request wants it.
export type QuoteLine = {
	kind: QuoteLineKind;
	description: string;
	quantity_milli: number;
	unit_amount: number;
	gst_code: string;
};

export type StoredQuoteLine = QuoteLine & { id: number; gst_amount: number; amount: number };

export type QuoteStatus = 'draft' | 'sent' | 'accepted' | 'declined' | 'superseded';
export type DisplayStatus = QuoteStatus | 'expired';

export type Quote = {
	id: number;
	contact_id: number;
	contact_name: string;
	project_id: number | null;
	project_name: string;
	number: number;
	revision: number;
	number_label: string;
	parent_id: number | null;
	title: string;
	terms: string;
	status: QuoteStatus;
	display_status: DisplayStatus;
	issue_date: string;
	valid_until: string;
	subtotal: number;
	gst_total: number;
	total: number;
	sent_at: string | null;
	decided_at: string | null;
};

export type QuoteAttachment = {
	id: number;
	created_at: string;
	file: { id: number; mime: string; size_bytes: number; original_name: string };
};

export type QuoteDetail = Quote & {
	lines: StoredQuoteLine[];
	attachments: QuoteAttachment[];
	revisions: { id: number; number_label: string; status: QuoteStatus }[];
};

// QuoteRequest is the POST/PUT body.
export type QuoteRequest = {
	contact_id: number;
	project_id: number | null;
	title: string;
	terms: string;
	issue_date: string;
	valid_until: string;
	lines: QuoteLine[];
};

export const STATUS_LABELS: Record<DisplayStatus, string> = {
	draft: 'Draft',
	sent: 'Sent',
	accepted: 'Accepted',
	declined: 'Declined',
	superseded: 'Superseded',
	expired: 'Expired'
};

export const STATUS_TONES: Record<DisplayStatus, 'success' | 'warning' | 'danger' | 'info'> = {
	draft: 'info',
	sent: 'warning',
	accepted: 'success',
	declined: 'danger',
	superseded: 'info',
	expired: 'danger'
};

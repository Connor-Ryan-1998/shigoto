// Client portal records staff manage for a client contact, and their
// portal views. Shapes mirror internal/clients and internal/portal/client.go.

export type DocumentKind = 'signature' | 'shared';
export type DocumentCategory = 'general' | 'discovery';
export type DocumentStatus = 'draft' | 'sent' | 'signed' | 'withdrawn' | 'superseded';

export type FileInfo = { id: number; mime: string; size_bytes: number; original_name: string };
export type SignatureInfo = { typed_name: string; email: string; signed_at: string };

export type ClientDocument = {
	id: number;
	contact_id: number;
	project_id: number | null;
	title: string;
	kind: DocumentKind;
	category: DocumentCategory;
	status: DocumentStatus;
	sent_at: string | null;
	created_at: string;
	supersedes_id: number | null;
	file: FileInfo;
	signature: SignatureInfo | null;
};

export type Welcome = { notes: string; updated_at: string | null };

export type ChecklistItem = {
	id: number;
	position: number;
	action: string;
	owner: string;
	due_date: string | null;
	done_at: string | null;
};

export const DOCUMENT_STATUS_LABELS: Record<DocumentStatus, string> = {
	draft: 'Draft',
	sent: 'Sent',
	signed: 'Signed',
	withdrawn: 'Withdrawn',
	superseded: 'Replaced'
};

export const DOCUMENT_STATUS_TONES: Record<DocumentStatus, 'info' | 'success' | 'warning' | 'danger'> = {
	draft: 'info',
	sent: 'warning',
	signed: 'success',
	withdrawn: 'danger',
	superseded: 'danger'
};

// statusLabel is a document's status for staff: a sent shared document is
// "Shared", a sent signature document is "Awaiting signature".
export function statusLabel(d: Pick<ClientDocument, 'kind' | 'status'>): string {
	if (d.status === 'sent') return d.kind === 'shared' ? 'Shared' : 'Awaiting signature';
	return DOCUMENT_STATUS_LABELS[d.status];
}

export const KIND_LABELS: Record<DocumentKind, string> = { signature: 'Needs signature', shared: 'Shared only' };
export const CATEGORY_LABELS: Record<DocumentCategory, string> = { general: 'General', discovery: 'Discovery' };

// documentActions lists the staff actions valid for d's status.
export function documentActions(d: Pick<ClientDocument, 'kind' | 'status'>): ('send' | 'withdraw' | 'replace' | 'delete')[] {
	switch (d.status) {
		case 'draft':
			return ['send', 'delete'];
		case 'sent':
			return ['withdraw', 'replace'];
		default:
			return [];
	}
}

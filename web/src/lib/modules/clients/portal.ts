// Client portal API shapes (internal/portal/client.go).
import type { ChecklistItem, DocumentCategory, DocumentKind, DocumentStatus, FileInfo, SignatureInfo } from './types';

export type PortalDocument = {
	id: number;
	project_id: number | null;
	project_name: string;
	title: string;
	kind: DocumentKind;
	category: DocumentCategory;
	status: DocumentStatus;
	sent_at: string | null;
	applies_to_all: boolean;
	file: FileInfo;
	signature: SignatureInfo | null;
	consent_text?: string;
};

export type ProjectStatus = 'pending' | 'in_progress' | 'completed' | 'cancelled';

export type HubProject = {
	id: number;
	name: string;
	status: ProjectStatus;
	start_date: string | null;
	end_date: string | null;
	to_sign: number;
};

export type Hub = {
	welcome: { notes: string };
	checklist: ChecklistItem[];
	projects: HubProject[];
	documents: { to_sign: number; signed: number; shared: number };
};

export type PortalProject = {
	id: number;
	name: string;
	status: ProjectStatus;
	summary: string;
	goals: string[];
	stakeholders: string[];
	start_date: string | null;
	end_date: string | null;
	documents: Record<'to_sign' | 'signed' | 'discovery' | 'shared' | 'inactive', PortalDocument[]>;
	invoices: { id: number; number: number | null; status: string; total: number; due_date: string | null }[];
};

export const PROJECT_STATUS_LABELS: Record<ProjectStatus, string> = {
	pending: 'Pending',
	in_progress: 'In progress',
	completed: 'Completed',
	cancelled: 'Cancelled'
};

export const PROJECT_STATUS_TONES: Record<ProjectStatus, 'info' | 'success' | 'warning' | 'danger'> = {
	pending: 'info',
	in_progress: 'success',
	completed: 'warning',
	cancelled: 'danger'
};

// portalStatus is a document's status as the client sees it.
export function portalStatus(d: Pick<PortalDocument, 'kind' | 'status'>): string {
	switch (d.status) {
		case 'sent':
			return d.kind === 'signature' ? 'To sign' : 'Shared';
		case 'signed':
			return 'Signed';
		case 'withdrawn':
			return 'Withdrawn';
		case 'superseded':
			return 'Replaced';
		default:
			return '';
	}
}

export const isToSign = (d: Pick<PortalDocument, 'kind' | 'status'>) => d.kind === 'signature' && d.status === 'sent';

export const documentFileUrl = (org: string, id: number) => `/api/v1/portal/${org}/documents/${id}/file`;
export const certificateUrl = (org: string, id: number) => `/api/v1/portal/${org}/documents/${id}/certificate`;

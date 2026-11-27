import { api } from '../../api';
import type { ClientOption, ProjectOption } from './QuoteForm.svelte';

export type QuoteOptions = {
	clients: ClientOption[];
	projects: ProjectOption[];
	defaultTerms: string;
	gstRegistered: boolean;
};

// loadQuoteOptions loads what the quote editor offers, skipping lists the
// user can't read: client contacts and settings (default terms, GST
// registration) need invoices_read, projects need timesheets_read.
export async function loadQuoteOptions(org: string, perms: string[]): Promise<QuoteOptions> {
	const base = `/api/v1/orgs/${org}`;
	const canContacts = perms.includes('invoices_read');
	const [clients, projects, settings] = await Promise.all([
		canContacts ? api<ClientOption[]>('GET', `${base}/contacts?kind=client`) : Promise.resolve([]),
		perms.includes('timesheets_read') ? api<ProjectOption[]>('GET', `${base}/projects`) : Promise.resolve([]),
		canContacts
			? api<{ gst_registered: boolean; quote_terms?: string }>('GET', `${base}/settings`)
			: Promise.resolve({ gst_registered: true, quote_terms: '' })
	]);
	return { clients, projects, defaultTerms: settings.quote_terms ?? '', gstRegistered: settings.gst_registered };
}

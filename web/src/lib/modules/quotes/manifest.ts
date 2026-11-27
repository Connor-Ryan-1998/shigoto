import type { ModuleManifest } from '../registry';

export const quotes: ModuleManifest = {
	name: 'quotes',
	label: 'Quotes',
	nav: [{ href: 'quotes', label: 'Quotes', perm: 'quotes_read', group: 'projects', icon: 'invoices' }]
};

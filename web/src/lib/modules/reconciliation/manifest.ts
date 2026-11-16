import type { ModuleManifest } from '../registry';

export const reconciliation: ModuleManifest = {
	name: 'reconciliation',
	label: 'Reconciliation',
	nav: [
		{ href: 'reconciliation', label: 'Reconciliation', perm: 'reports_read', group: 'finance', icon: 'reconciliation' },
		{ href: 'expenses', label: 'Expenses', perm: 'reports_read', group: 'finance', icon: 'expenses' }
	],
	dashboardCard: () => import('./DashboardCard.svelte')
};

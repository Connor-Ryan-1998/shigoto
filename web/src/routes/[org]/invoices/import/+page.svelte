<script lang="ts">
	import { page } from '$app/state';
	import PageHeader from '../../../../lib/components/PageHeader.svelte';
	import CsvImport from '../../../../lib/imports/CsvImport.svelte';
	import type { ImportField, ImportSummary } from '../../../../lib/imports/types';
	import { money } from '../../../../lib/format';
	import { session } from '../../../../lib/session.svelte';

	const org = $derived(page.params.org ?? '');
	const canImport = $derived(session.currentOrg?.slug === org && (session.currentOrg?.perms.includes('import') ?? false));

	const fields: ImportField[] = [
		{ key: 'number', label: 'Invoice number', required: true },
		{ key: 'customer', label: 'Customer', required: true },
		{ key: 'issue_date', label: 'Issue date', required: true },
		{ key: 'due_date', label: 'Due date', required: true },
		{ key: 'subtotal', label: 'Amount ex GST', required: true },
		{ key: 'gst', label: 'GST', required: true },
		{ key: 'total', label: 'Total', required: true },
		{ key: 'outstanding', label: 'Outstanding', required: true },
		{ key: 'paid_date', label: 'Paid date', required: false }
	];

	const summary = (s: ImportSummary) => `${s.new} new ${s.new === 1 ? 'invoice' : 'invoices'} totalling ${money(s.total_cents ?? 0)}`;
</script>

<PageHeader title="Import invoices" back={{ href: `/${org}/invoices`, label: 'Back to invoices' }} />

{#if !canImport}
	<p role="alert">Only the organisation owner can import.</p>
{:else}
	<p class="mb-4 max-w-3xl text-sm text-base-content/70">
		One row per invoice, matched to client contacts by name. Invoices keep their own dates and are never sent. Paid date is
		required when anything has been paid.
	</p>
	{#key org}
		<CsvImport
			{fields}
			previewPath={`/api/v1/orgs/${org}/invoices/import/preview`}
			applyPath={`/api/v1/orgs/${org}/invoices/import`}
			backHref={`/${org}/invoices`}
			noun="invoices"
			summaryLabel={summary}
		/>
	{/key}
{/if}

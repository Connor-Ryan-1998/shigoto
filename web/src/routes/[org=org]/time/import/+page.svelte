<script lang="ts">
	import { page } from '$app/state';
	import PageHeader from '../../../../lib/components/PageHeader.svelte';
	import CsvImport from '../../../../lib/imports/CsvImport.svelte';
	import type { ImportField, ImportSummary } from '../../../../lib/imports/types';
	import { minutes } from '../../../../lib/format';
	import { session } from '../../../../lib/session.svelte';

	const org = $derived(page.params.org ?? '');
	const canImport = $derived(session.currentOrg?.slug === org && (session.currentOrg?.perms.includes('import') ?? false));

	const fields: ImportField[] = [
		{ key: 'date', label: 'Date', required: true },
		{ key: 'hours', label: 'Hours', required: true },
		{ key: 'project', label: 'Project', required: true },
		{ key: 'task', label: 'Task', required: true },
		{ key: 'person', label: 'Person (email)', required: true },
		{ key: 'invoice', label: 'Invoice number', required: true },
		{ key: 'note', label: 'Note', required: false }
	];

	const summary = (s: ImportSummary) => `${s.new} new ${s.new === 1 ? 'entry' : 'entries'} totalling ${minutes(s.total_minutes ?? 0)}`;
</script>

<PageHeader title="Import time" back={{ href: `/${org}/time`, label: 'Back to time' }} />

{#if !canImport}
	<p role="alert">Only the organisation owner can import.</p>
{:else}
	<p class="mb-4 max-w-3xl text-sm text-base-content/70">
		One row per time entry, matched to projects and tasks by name and to people by email. Import the invoices first: each
		row is linked to its invoice, so imported time is already billed.
	</p>
	{#key org}
		<CsvImport
			{fields}
			previewPath={`/api/v1/orgs/${org}/time-entries/import/preview`}
			applyPath={`/api/v1/orgs/${org}/time-entries/import`}
			backHref={`/${org}/time`}
			noun="time"
			summaryLabel={summary}
		/>
	{/key}
{/if}

<script lang="ts" module>
	export type ClientOption = { id: number; name: string };
	export type ProjectOption = { id: number; name: string; client_contact_id: number; status: string };
</script>

<script lang="ts">
	// QuoteForm edits a draft quote: customer, optional project, title,
	// dates, terms and lines. The page owns loading and saving; this form
	// only validates and hands back the request body.
	import { todayLocal } from '../../format';
	import Field from '../../components/Field.svelte';
	import Select from '../../components/Select.svelte';
	import FormActions from '../../components/FormActions.svelte';
	import QuoteLineEditor, { emptyRow, parseRows, rowsFromLines, type LineRow } from './QuoteLineEditor.svelte';
	import type { QuoteDetail, QuoteRequest } from './types';

	let {
		clients,
		projects,
		initial = null,
		defaultTerms = '',
		gstRegistered = true,
		busy = false,
		error = '',
		onSubmit,
		oncancel,
		onchange
	}: {
		clients: ClientOption[];
		projects: ProjectOption[];
		initial?: QuoteDetail | null;
		defaultTerms?: string;
		gstRegistered?: boolean;
		busy?: boolean;
		error?: string;
		onSubmit: (body: QuoteRequest) => void;
		oncancel: () => void;
		onchange?: () => void;
	} = $props();

	function addDays(iso: string, days: number): string {
		const d = new Date(`${iso}T00:00:00Z`);
		d.setUTCDate(d.getUTCDate() + days);
		return d.toISOString().slice(0, 10);
	}

	// Seeded once: the page remounts the form to reset it.
	const seed = (() => {
		const today = todayLocal();
		return {
			contact: String(initial?.contact_id ?? clients[0]?.id ?? ''),
			project: initial?.project_id != null ? String(initial.project_id) : '',
			title: initial?.title ?? '',
			terms: initial?.terms ?? defaultTerms,
			issue: initial?.issue_date ?? today,
			until: initial?.valid_until ?? addDays(today, 30),
			rows: initial ? rowsFromLines(initial.lines) : [emptyRow('labour', gstRegistered ? 'GST' : 'FRE')]
		};
	})();

	let contact = $state(seed.contact);
	let project = $state(seed.project);
	let title = $state(seed.title);
	let terms = $state(seed.terms);
	let issueDate = $state(seed.issue);
	let validUntil = $state(seed.until);
	let rows = $state<LineRow[]>(seed.rows);
	let formError = $state('');

	const clientOptions = $derived(clients.map((c) => ({ value: String(c.id), label: c.name })));
	// Only the chosen customer's open projects, plus the one already linked.
	const projectOptions = $derived([
		{ value: '', label: 'No project' },
		...projects
			.filter((p) => String(p.client_contact_id) === contact && (p.status === 'pending' || p.status === 'in_progress' || String(p.id) === project))
			.map((p) => ({ value: String(p.id), label: p.name }))
	]);

	function changeCustomer(value: string) {
		contact = value;
		if (!projects.some((p) => String(p.id) === project && String(p.client_contact_id) === value)) project = '';
	}

	function submit(e: SubmitEvent) {
		e.preventDefault();
		if (busy) return;
		formError = '';
		if (contact === '') {
			formError = 'Choose a customer.';
			return;
		}
		if (title.trim() === '') {
			formError = 'Enter a title.';
			return;
		}
		if (validUntil < issueDate) {
			formError = 'Valid until must not be before the issue date.';
			return;
		}
		const lines = parseRows(rows);
		if (lines === null) {
			formError = 'Complete every line before saving.';
			return;
		}
		onSubmit({
			contact_id: Number(contact),
			project_id: project === '' ? null : Number(project),
			title: title.trim(),
			terms,
			issue_date: issueDate,
			valid_until: validUntil,
			lines
		});
	}
</script>

<form class="flex min-w-0 max-w-5xl flex-col gap-5" onsubmit={submit}>
	<fieldset disabled={busy} class="grid gap-4 sm:grid-cols-2" oninput={() => onchange?.()} onchange={() => onchange?.()}>
		<legend class="sr-only">Quote details</legend>
		<Field label="Customer" id="quote-customer">
			<Select id="quote-customer" value={contact} options={clientOptions} onchange={changeCustomer} />
		</Field>
		<Field label="Project" id="quote-project" hint="Optional. Accepting the quote creates a project when none is chosen.">
			<Select id="quote-project" bind:value={project} options={projectOptions} />
		</Field>
		<div class="sm:col-span-2">
			<Field label="Title" id="quote-title">
				<input class="input w-full" id="quote-title" bind:value={title} maxlength="200" />
			</Field>
		</div>
		<Field label="Issue date" id="quote-issue">
			<input class="input w-full" id="quote-issue" type="date" bind:value={issueDate} />
		</Field>
		<Field label="Valid until" id="quote-valid">
			<input class="input w-full" id="quote-valid" type="date" bind:value={validUntil} />
		</Field>
	</fieldset>

	<section aria-labelledby="quote-lines-heading" class="rounded-box border border-base-300 bg-base-100 p-4">
		<h2 id="quote-lines-heading" class="mt-0 mb-3 text-lg font-semibold">Lines</h2>
		<QuoteLineEditor bind:rows {busy} defaultGst={gstRegistered ? 'GST' : 'FRE'} {onchange} />
	</section>

	<fieldset disabled={busy} oninput={() => onchange?.()}>
		<legend class="sr-only">Terms</legend>
		<Field label="Terms" id="quote-terms" hint="Printed on the quote.">
			<textarea class="textarea w-full" id="quote-terms" rows="4" bind:value={terms}></textarea>
		</Field>
	</fieldset>

	{#if formError || error}<p role="alert" class="alert alert-error alert-soft">{formError || error}</p>{/if}
	<FormActions {busy} submitLabel="Save draft" {oncancel} />
</form>

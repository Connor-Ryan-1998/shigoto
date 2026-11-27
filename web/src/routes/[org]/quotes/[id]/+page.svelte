<script lang="ts">
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { api, ApiError } from '../../../../lib/api';
	import { session } from '../../../../lib/session.svelte';
	import { money } from '../../../../lib/format';
	import PageHeader from '../../../../lib/components/PageHeader.svelte';
	import LoadState from '../../../../lib/components/LoadState.svelte';
	import Button from '../../../../lib/components/Button.svelte';
	import DateText from '../../../../lib/components/DateText.svelte';
	import StatusBadge from '../../../../lib/components/StatusBadge.svelte';
	import AttachmentsPanel from '../../../../lib/modules/quotes/AttachmentsPanel.svelte';
	import { STATUS_LABELS, STATUS_TONES, type QuoteDetail, type StoredQuoteLine } from '../../../../lib/modules/quotes/types';

	const org = $derived(page.params.org ?? '');
	const id = $derived(page.params.id ?? '');
	const scope = $derived(`${org}:${id}`);
	const perms = $derived(session.currentOrg?.slug === org ? session.currentOrg.perms : []);
	const canWrite = $derived(perms.includes('quotes_write'));
	// Accepting creates a project, which needs timesheets_admin too.
	const canAccept = $derived(canWrite && perms.includes('timesheets_admin'));

	let quote = $state<QuoteDetail | null>(null);
	let loaded = $state('');
	let error = $state('');
	let actionError = $state('');
	let busy = $state(false);
	let seq = 0;

	async function load() {
		const token = ++seq;
		const s = scope;
		error = '';
		try {
			const q = await api<QuoteDetail>('GET', `/api/v1/orgs/${org}/quotes/${id}`);
			if (token !== seq || s !== scope) return;
			quote = q;
			loaded = s;
		} catch (e) {
			if (token === seq) error = e instanceof ApiError ? e.message : 'Could not load the quote.';
		}
	}

	$effect(() => {
		scope;
		quote = null;
		loaded = '';
		actionError = '';
		load();
		return () => { seq++; };
	});

	// run performs one action at a time; a second click while busy is
	// ignored. An action that leaves the page (navigates) stays busy after
	// success, so a repeat click can't send it twice.
	async function run<T>(fn: () => Promise<T>, then: (result: T) => Promise<void> | void, confirmText?: string, navigates = false) {
		if (busy || (confirmText && !confirm(confirmText))) return;
		const s = scope;
		busy = true;
		actionError = '';
		let ok = false;
		try {
			const result = await fn();
			if (s !== scope) return;
			ok = true;
			await then(result);
		} catch (e) {
			if (s === scope) actionError = e instanceof ApiError ? e.message : 'Something went wrong.';
		} finally {
			if (!(ok && navigates)) busy = false;
		}
	}

	const base = $derived(`/api/v1/orgs/${org}/quotes/${id}`);
	const send = () => run(() => api('POST', `${base}/send`), load, 'Mark this quote as sent? Its PDF is saved now and won\'t change afterwards.');
	const decline = () => run(() => api('POST', `${base}/decline`), load, 'Mark this quote as declined?');
	const accept = () => run(
		() => api<{ project: { id: number } }>('POST', `${base}/accept`),
		(r) => goto(`/${org}/projects/${r.project.id}`),
		'Accept this quote? This creates the project and its tasks.',
		true
	);
	const revise = () => run(() => api<{ id: number }>('POST', `${base}/revise`), (r) => goto(`/${org}/quotes/${r.id}/edit`), undefined, true);
	const remove = () => run(() => api('DELETE', base), () => goto(`/${org}/quotes`), 'Delete this draft quote?', true);

	const labour = $derived(quote?.lines.filter((l) => l.kind === 'labour') ?? []);
	const materials = $derived(quote?.lines.filter((l) => l.kind === 'material') ?? []);
	const qty = (l: StoredQuoteLine) => String(l.quantity_milli / 1000);
</script>

{#snippet lineTable(title: string, qtyLabel: string, unitLabel: string, lines: StoredQuoteLine[])}
	{#if lines.length > 0}
		<section class="rounded-box border border-base-300 bg-base-100 p-4">
			<h2 class="mt-0 mb-3 text-lg font-semibold">{title}</h2>
			<div class="overflow-x-auto">
				<table class="table">
					<thead><tr><th scope="col">Description</th><th scope="col" class="text-right">{qtyLabel}</th><th scope="col" class="text-right">{unitLabel}</th><th scope="col">GST</th><th scope="col" class="text-right">Amount</th></tr></thead>
					<tbody>
						{#each lines as l (l.id)}
							<tr><td>{l.description}</td><td class="text-right tabular-nums">{qty(l)}</td><td class="text-right tabular-nums">{money(l.unit_amount)}</td><td>{l.gst_code}</td><td class="text-right tabular-nums">{money(l.amount)}</td></tr>
						{/each}
					</tbody>
				</table>
			</div>
		</section>
	{/if}
{/snippet}

<PageHeader title={quote ? `${quote.number_label} · ${quote.title}` : 'Quote'} back={{ href: `/${org}/quotes`, label: 'Back to quotes' }}>
	{#snippet actions()}
		{#if quote}
			<a class="btn btn-outline" href={`${base}/pdf`} download>Download PDF</a>
			{#if canWrite && quote.status === 'draft'}
				<Button variant="secondary" disabled={busy} onclick={() => goto(`/${org}/quotes/${id}/edit`)}>Edit</Button>
				<Button variant="danger" disabled={busy} onclick={remove}>Delete</Button>
				<Button {busy} onclick={send}>Mark sent</Button>
			{/if}
			{#if canWrite && quote.status === 'sent'}
				<Button variant="secondary" disabled={busy} onclick={decline}>Decline</Button>
			{/if}
			{#if canWrite && (quote.status === 'sent' || quote.status === 'declined')}
				<Button variant="secondary" disabled={busy} onclick={revise}>Revise</Button>
			{/if}
			{#if canAccept && quote.status === 'sent'}
				<Button {busy} onclick={accept}>Accept</Button>
			{/if}
		{/if}
	{/snippet}
</PageHeader>

<LoadState loading={loaded !== scope} {error} loadingLabel="Loading quote…" onretry={load}>
	{#if quote}
		{#if actionError}<p role="alert" class="alert alert-error alert-soft mb-4">{actionError}</p>{/if}
		<div class="grid max-w-5xl gap-5">
			<dl class="grid gap-4 rounded-box border border-base-300 bg-base-100 p-4 sm:grid-cols-3">
				<div><dt class="text-sm text-base-content/70">Status</dt><dd><StatusBadge status={STATUS_LABELS[quote.display_status]} tone={STATUS_TONES[quote.display_status]} /></dd></div>
				<div><dt class="text-sm text-base-content/70">Customer</dt><dd>{quote.contact_name}</dd></div>
				<div>
					<dt class="text-sm text-base-content/70">Project</dt>
					<dd>
						{#if quote.project_id != null}
							{quote.project_name}
							{#if quote.status === 'accepted'} · <a class="link" href={`/${org}/projects/${quote.project_id}`}>Open project</a>{/if}
						{:else}—{/if}
					</dd>
				</div>
				<div><dt class="text-sm text-base-content/70">Issued</dt><dd><DateText iso={quote.issue_date} /></dd></div>
				<div><dt class="text-sm text-base-content/70">Valid until</dt><dd><DateText iso={quote.valid_until} /></dd></div>
				<div><dt class="text-sm text-base-content/70">Total</dt><dd class="text-lg font-semibold tabular-nums">{money(quote.total)}</dd></div>
			</dl>

			{@render lineTable('Labour', 'Hours', 'Rate', labour)}
			{@render lineTable('Materials', 'Qty', 'Unit price', materials)}

			<dl class="ml-auto grid w-full max-w-xs grid-cols-2 gap-1 tabular-nums">
				<dt>Subtotal</dt><dd class="text-right">{money(quote.subtotal)}</dd>
				{#if quote.gst_total !== 0}<dt>GST</dt><dd class="text-right">{money(quote.gst_total)}</dd>{/if}
				<dt class="font-semibold">Total</dt><dd class="text-right font-semibold">{money(quote.total)}</dd>
			</dl>

			{#if quote.terms.trim()}
				<section class="rounded-box border border-base-300 bg-base-100 p-4">
					<h2 class="mt-0 mb-2 text-lg font-semibold">Terms</h2>
					<p class="whitespace-pre-line">{quote.terms}</p>
				</section>
			{/if}

			<AttachmentsPanel {org} quoteId={quote.id} attachments={quote.attachments} {canWrite} onchange={load} />

			{#if quote.revisions.length > 1}
				<section class="rounded-box border border-base-300 bg-base-100 p-4">
					<h2 class="mt-0 mb-2 text-lg font-semibold">Revisions</h2>
					<ul class="grid gap-1">
						{#each quote.revisions as r (r.id)}
							<li>
								{#if r.id === quote.id}<strong>{r.number_label}</strong>{:else}<a class="link" href={`/${org}/quotes/${r.id}`}>{r.number_label}</a>{/if}
								· {STATUS_LABELS[r.status]}
							</li>
						{/each}
					</ul>
				</section>
			{/if}
		</div>
	{/if}
</LoadState>

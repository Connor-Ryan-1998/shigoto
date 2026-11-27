<script lang="ts">
	// Client project page: summary, goals and stakeholders, then documents
	// to sign, signed, discovery and shared (withdrawn/replaced greyed with
	// no Sign action), then the project's invoices.
	import { onDestroy } from 'svelte';
	import { page } from '$app/state';
	import { api, ApiError } from '../../../../../lib/api';
	import { date, money } from '../../../../../lib/format';
	import PageHeader from '../../../../../lib/components/PageHeader.svelte';
	import StatusBadge from '../../../../../lib/components/StatusBadge.svelte';
	import EmptyState from '../../../../../lib/components/EmptyState.svelte';
	import Button from '../../../../../lib/components/Button.svelte';
	import PortalQuotes from '../../../../../lib/modules/quotes/PortalQuotes.svelte';
	import { session } from '../../../../../lib/session.svelte';
	import {
		PROJECT_STATUS_LABELS,
		PROJECT_STATUS_TONES,
		documentFileUrl,
		portalStatus,
		type PortalDocument,
		type PortalProject
	} from '../../../../../lib/modules/clients/portal';

	const org = $derived(page.params.org ?? '');
	const id = $derived(page.params.id ?? '');
	let project = $state<PortalProject | null>(null);
	let error = $state('');
	let seq = 0;
	onDestroy(() => { seq++; });

	async function load() {
		const s = ++seq;
		error = '';
		try {
			const p = await api<PortalProject>('GET', `/api/v1/portal/${org}/projects/${id}`);
			if (s === seq) project = p;
		} catch (e) {
			if (s === seq) error = e instanceof ApiError ? (e.status === 404 ? 'Project not found.' : e.message) : 'Something went wrong.';
		}
	}
	$effect(() => { if (org && id) load(); });
</script>

{#snippet docList(docs: PortalDocument[], sign: boolean)}
	<ul class="menu w-full rounded-box bg-base-100 shadow-sm">
		{#each docs as d (d.id)}
			{@const inactive = d.status === 'withdrawn' || d.status === 'superseded'}
			<li class={inactive ? 'opacity-60' : ''}>
				<div class="flex flex-wrap items-center gap-2">
					<span class="font-medium">{d.title}</span>
					{#if d.applies_to_all}<span class="badge badge-ghost">Applies to all projects</span>{/if}
					{#if inactive}<span class="badge badge-ghost">{portalStatus(d)}</span>{/if}
					{#if d.signature}<span class="text-sm text-base-content/70">Signed by {d.signature.typed_name}, {date(d.signature.signed_at.slice(0, 10))}</span>{/if}
					<span class="ml-auto flex gap-2">
						{#if sign}
							<a class="btn btn-primary btn-sm" href={`/portal/${org}/documents/${d.id}`} aria-label={`Review and sign ${d.title}`}>Review &amp; sign</a>
						{:else}
							<a class="btn btn-ghost btn-sm" href={`/portal/${org}/documents/${d.id}`} aria-label={`View ${d.title}`}>View</a>
							<a class="btn btn-ghost btn-sm" href={documentFileUrl(org, d.id)} aria-label={`Download ${d.title}`}>PDF</a>
						{/if}
					</span>
				</div>
			</li>
		{/each}
	</ul>
{/snippet}

{#if error}
	<EmptyState title="Couldn't load this project" description={error}>
		{#snippet action()}<Button onclick={load}>Retry</Button>{/snippet}
	</EmptyState>
{:else if !project}
	<p aria-live="polite">Loading…</p>
{:else}
	<PageHeader title={project.name} back={{ href: `/portal/${org}/hub`, label: 'Back to hub' }}>
		{#snippet actions()}
			{#if project}<StatusBadge status={PROJECT_STATUS_LABELS[project.status]} tone={PROJECT_STATUS_TONES[project.status]} />{/if}
		{/snippet}
	</PageHeader>
	<div class="grid max-w-5xl gap-6 lg:grid-cols-[2fr_1fr]">
		<div class="flex flex-col gap-6">
			<section class="card bg-base-100 shadow-sm" aria-labelledby="summary-heading">
				<div class="card-body">
					<h2 id="summary-heading" class="text-lg font-semibold">Summary</h2>
					<p class="whitespace-pre-wrap">{project.summary || 'No summary yet.'}</p>
					{#if project.start_date || project.end_date}
						<p class="text-sm text-base-content/70">{project.start_date ? date(project.start_date) : '—'} → {project.end_date ? date(project.end_date) : '—'}</p>
					{/if}
					{#if project.goals.length}
						<h3 class="mt-2 font-semibold">Goals</h3>
						<ul class="list-disc pl-5">{#each project.goals as g, i (i)}<li>{g}</li>{/each}</ul>
					{/if}
				</div>
			</section>

			{#if project.documents.to_sign.length}
				<section aria-labelledby="to-sign-heading">
					<h2 id="to-sign-heading" class="mb-2 text-lg font-semibold"><span aria-hidden="true">✍ </span>To sign</h2>
					<p class="mb-2 text-sm text-base-content/70">You need to sign these before kickoff.</p>
					{@render docList(project.documents.to_sign, true)}
				</section>
			{/if}
			{#if project.documents.signed.length}
				<section aria-labelledby="signed-heading">
					<h2 id="signed-heading" class="mb-2 text-lg font-semibold">Signed</h2>
					{@render docList(project.documents.signed, false)}
				</section>
			{/if}
			{#if project.documents.discovery.length}
				<section aria-labelledby="discovery-heading">
					<h2 id="discovery-heading" class="mb-2 text-lg font-semibold">Discovery</h2>
					{@render docList(project.documents.discovery, false)}
				</section>
			{/if}
			{#if project.documents.shared.length}
				<section aria-labelledby="shared-heading">
					<h2 id="shared-heading" class="mb-2 text-lg font-semibold">Shared with you</h2>
					{@render docList(project.documents.shared, false)}
				</section>
			{/if}
			{#if project.documents.inactive.length}
				<section aria-labelledby="inactive-heading">
					<h2 id="inactive-heading" class="mb-2 text-lg font-semibold">Withdrawn or replaced</h2>
					{@render docList(project.documents.inactive, false)}
				</section>
			{/if}
		</div>
		<div class="flex flex-col gap-6">
			<section class="card bg-base-100 shadow-sm" aria-labelledby="stakeholders-heading">
				<div class="card-body">
					<h2 id="stakeholders-heading" class="text-lg font-semibold">Primary stakeholders</h2>
					{#if project.stakeholders.length}
						<ul>{#each project.stakeholders as st, i (i)}<li>{st}</li>{/each}</ul>
					{:else}
						<p class="text-base-content/70">—</p>
					{/if}
				</div>
			</section>
			{#if session.modules.includes('quotes')}
				<section class="card bg-base-100 shadow-sm" aria-labelledby="quotes-heading">
					<div class="card-body">
						<h2 id="quotes-heading" class="text-lg font-semibold">Quotes</h2>
						<PortalQuotes {org} projectId={project.id} />
					</div>
				</section>
			{/if}
			<section class="card bg-base-100 shadow-sm" aria-labelledby="invoices-heading">
				<div class="card-body">
					<h2 id="invoices-heading" class="text-lg font-semibold">Invoices</h2>
					{#if project.invoices.length}
						<ul class="flex flex-col gap-2">
							{#each project.invoices as inv (inv.id)}
								<li class="flex justify-between gap-2">
									<a class="link" href={`/portal/${org}/invoices/${inv.id}`}>Invoice {inv.number ?? inv.id}</a>
									<span class="tabular-nums">{money(inv.total)}</span>
								</li>
							{/each}
						</ul>
					{:else}
						<p class="text-base-content/70">No invoices for this project yet.</p>
					{/if}
				</div>
			</section>
		</div>
	</div>
{/if}

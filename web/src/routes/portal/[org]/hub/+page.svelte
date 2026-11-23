<script lang="ts">
	// Client hub: what a client sees first - documents waiting on their
	// signature, the studio's welcome notes, the kickoff checklist, their
	// projects and their documents.
	import { onDestroy } from 'svelte';
	import { page } from '$app/state';
	import { api, ApiError } from '../../../../lib/api';
	import { date } from '../../../../lib/format';
	import PageHeader from '../../../../lib/components/PageHeader.svelte';
	import StatusBadge from '../../../../lib/components/StatusBadge.svelte';
	import EmptyState from '../../../../lib/components/EmptyState.svelte';
	import Button from '../../../../lib/components/Button.svelte';
	import {
		PROJECT_STATUS_LABELS,
		PROJECT_STATUS_TONES,
		portalStatus,
		type Hub,
		type PortalDocument
	} from '../../../../lib/modules/clients/portal';

	const org = $derived(page.params.org ?? '');
	let hub = $state<Hub | null>(null);
	let docs = $state<PortalDocument[]>([]);
	let error = $state('');
	let showCancelled = $state(false);
	let seq = 0;
	onDestroy(() => { seq++; });

	const active = $derived(hub?.projects.filter((p) => p.status !== 'cancelled') ?? []);
	const cancelled = $derived(hub?.projects.filter((p) => p.status === 'cancelled') ?? []);
	const toSign = $derived(hub?.documents.to_sign ?? 0);

	async function load() {
		const s = ++seq;
		error = '';
		try {
			const [h, d] = await Promise.all([
				api<Hub>('GET', `/api/v1/portal/${org}/hub`),
				api<PortalDocument[]>('GET', `/api/v1/portal/${org}/documents`)
			]);
			if (s !== seq) return;
			hub = h;
			docs = d;
		} catch (e) {
			if (s === seq) error = e instanceof ApiError ? e.message : 'Something went wrong.';
		}
	}
	$effect(() => { if (org) load(); });
</script>

<PageHeader title="Welcome" />
{#if error}
	<EmptyState title="Couldn't load your hub" description={error}>
		{#snippet action()}<Button onclick={load}>Retry</Button>{/snippet}
	</EmptyState>
{:else if !hub}
	<p aria-live="polite">Loading…</p>
{:else}
	<div class="flex max-w-5xl flex-col gap-6">
		{#if toSign > 0}
			<a class="alert alert-warning" href={`/portal/${org}/documents?filter=to_sign`}>
				<strong>{toSign} {toSign === 1 ? 'document needs' : 'documents need'} your signature</strong>
				<span class="ml-auto">Review</span>
			</a>
		{/if}

		<section class="card bg-base-100 shadow-sm" aria-labelledby="welcome-heading">
			<div class="card-body">
				<h2 id="welcome-heading" class="text-lg font-semibold">How we work together</h2>
				{#if hub.welcome.notes}
					<p class="whitespace-pre-wrap">{hub.welcome.notes}</p>
				{:else}
					<p class="text-base-content/70">Your welcome notes will appear here.</p>
				{/if}
			</div>
		</section>

		{#if hub.checklist.length > 0}
			<section class="card bg-base-100 shadow-sm" aria-labelledby="checklist-heading">
				<div class="card-body">
					<h2 id="checklist-heading" class="text-lg font-semibold">Client action checklist</h2>
					<p class="text-sm text-base-content/70">What we need from you before kickoff. We'll tick each item once it's done.</p>
					<div class="overflow-x-auto">
						<table class="table">
							<thead><tr><th><span class="sr-only">Done</span></th><th>Action</th><th>Owner</th><th>Due</th></tr></thead>
							<tbody>
								{#each hub.checklist as it (it.id)}
									<tr>
										<td>{#if it.done_at}<span class="text-success" aria-label="Done">✓</span>{:else}<span aria-label="Not done yet">○</span>{/if}</td>
										<td class={it.done_at ? 'text-base-content/60 line-through' : ''}>{it.action}</td>
										<td>{it.owner || '—'}</td>
										<td>{it.due_date ? date(it.due_date) : '—'}</td>
									</tr>
								{/each}
							</tbody>
						</table>
					</div>
				</div>
			</section>
		{/if}

		{#if hub.projects.length > 0}
			<section aria-labelledby="projects-heading">
				<h2 id="projects-heading" class="mb-2 text-lg font-semibold">Projects</h2>
				<ul class="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
					{#each showCancelled ? [...active, ...cancelled] : active as p (p.id)}
						<li>
							<a class="card h-full bg-base-100 shadow-sm transition hover:shadow-md" href={`/portal/${org}/projects/${p.id}`}>
								<div class="card-body gap-2">
									<div class="flex flex-wrap gap-2">
										<StatusBadge status={PROJECT_STATUS_LABELS[p.status]} tone={PROJECT_STATUS_TONES[p.status]} />
										{#if p.to_sign > 0}<span class="badge badge-warning badge-soft">✍ {p.to_sign} to sign</span>{/if}
									</div>
									<span class="font-semibold">{p.name}</span>
									{#if p.start_date}<span class="text-sm text-base-content/70">From {date(p.start_date)}</span>{/if}
								</div>
							</a>
						</li>
					{/each}
				</ul>
				{#if cancelled.length > 0 && !showCancelled}
					<button type="button" class="btn btn-ghost btn-sm mt-2" onclick={() => (showCancelled = true)}>Show cancelled ({cancelled.length})</button>
				{/if}
			</section>
		{/if}

		<section aria-labelledby="documents-heading">
			<div class="mb-2 flex items-center justify-between">
				<h2 id="documents-heading" class="text-lg font-semibold">Documents</h2>
				<a class="link" href={`/portal/${org}/documents`}>All documents</a>
			</div>
			{#if docs.length === 0}
				<p class="text-base-content/70">No documents yet.</p>
			{:else}
				<ul class="menu w-full rounded-box bg-base-100 shadow-sm">
					{#each docs.slice(0, 5) as d (d.id)}
						<li>
							<a href={`/portal/${org}/documents/${d.id}`} class="flex justify-between">
								<span>{d.title}</span>
								<span class="text-sm text-base-content/70">{portalStatus(d)}</span>
							</a>
						</li>
					{/each}
				</ul>
			{/if}
		</section>
	</div>
{/if}

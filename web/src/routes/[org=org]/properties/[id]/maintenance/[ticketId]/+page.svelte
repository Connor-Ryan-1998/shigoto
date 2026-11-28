<script lang="ts">
	import { page } from '$app/state';
	import { api, ApiError } from '../../../../../../lib/api';
	import { session } from '../../../../../../lib/session.svelte';
	import Button from '../../../../../../lib/components/Button.svelte';
	import LoadState from '../../../../../../lib/components/LoadState.svelte';
	import StatusBadge from '../../../../../../lib/components/StatusBadge.svelte';
	import MediaGallery from '../../../../../../lib/modules/property/MediaGallery.svelte';
	import AssignSupplierDialog from '../../../../../../lib/modules/property/AssignSupplierDialog.svelte';
	import VisitDialog from '../../../../../../lib/modules/property/VisitDialog.svelte';
	import EntryPanel from '../../../../../../lib/modules/property/EntryPanel.svelte';
	import TicketMessageDialog, { type MessageAction } from '../../../../../../lib/modules/property/TicketMessageDialog.svelte';
	import {
		CATEGORY_LABEL, STATUS_LABEL, STATUS_TONE, URGENCY_LABEL, URGENCY_TONE, eventLabel, isOpen, visitWindow, windowLabel,
		type Ticket, type TicketEvent, type TicketFile, type Visit, type Window
	} from '../../../../../../lib/modules/property/maintenance';

	type Detail = {
		ticket: Ticket;
		tenants: { id: number; name: string; email: string; phone: string }[];
		files: TicketFile[];
		events: TicketEvent[];
		entry_preference: string | null;
		availability: Window[];
		open_visit: Visit | null;
		visits: Visit[];
	};

	const org = $derived(page.params.org ?? '');
	const propertyId = $derived(page.params.id ?? '');
	const ticketId = $derived(page.params.ticketId ?? '');
	const member = $derived(session.currentOrg?.slug === org ? session.currentOrg : null);
	const canWrite = $derived(member?.perms.includes('property_write') ?? false);
	const apiBase = $derived(`/api/v1/orgs/${org}/maintenance/${ticketId}`);

	let detail = $state<Detail | null>(null);
	let loading = $state(true);
	let error = $state('');
	let assigning = $state(false);
	let messageOpen = $state(false);
	let action = $state<MessageAction>('note');
	let visitOpen = $state(false);
	let rescheduling = $state(false);
	let visitError = $state('');

	async function visitDone(v: Visit) {
		visitError = '';
		try {
			await api('POST', `${apiBase}/visits/${v.id}/done`);
			await load();
		} catch (e) {
			visitError = e instanceof ApiError ? e.message : 'Could not update the visit.';
		}
	}
	let seq = 0;

	async function load() {
		const token = ++seq;
		error = '';
		try {
			const d = await api<Detail>('GET', apiBase);
			if (token !== seq) return;
			detail = d;
			loading = false;
			// Opening a new ticket tells the tenant it has been seen - only
			// for staff who can act on it (not bookkeepers).
			if (canWrite && d.ticket.status === 'new') {
				const viewed = await api<Ticket>('POST', `${apiBase}/viewed`);
				if (token === seq && detail) detail.ticket = viewed;
			}
		} catch (e) {
			if (token === seq) { error = e instanceof ApiError ? e.message : 'Could not load the repair.'; loading = false; }
		}
	}
	$effect(() => { apiBase; loading = true; detail = null; load(); return () => { seq++; }; });

	function openMessage(a: MessageAction) { action = a; messageOpen = true; }
</script>

<a class="link mb-3 inline-block" href={`/${org}/properties/${propertyId}/maintenance`}>← Back to repairs</a>

<LoadState {loading} {error} loadingLabel="Loading repair…" onretry={load}>
	{#if detail}
		{@const t = detail.ticket}
		<div class="mb-4 flex flex-wrap items-start gap-3">
			<div>
				<h2 class="text-xl font-semibold">{t.ref} · {t.title}</h2>
				<p class="text-sm text-base-content/70">{CATEGORY_LABEL[t.category] ?? t.category} · raised by {t.raised_by_name || 'staff'} · {new Date(t.created_at).toLocaleString()}</p>
			</div>
			<div class="flex gap-2">
				<StatusBadge status={URGENCY_LABEL[t.urgency]} tone={URGENCY_TONE[t.urgency]} />
				<StatusBadge status={STATUS_LABEL[t.status]} tone={STATUS_TONE[t.status]} />
			</div>
			{#if canWrite}
				<div class="ml-auto flex flex-wrap gap-2">
					{#if isOpen(t)}
						<Button variant="secondary" onclick={() => (assigning = true)}>{t.supplier ? 'Change tradesperson' : 'Assign tradesperson'}</Button>
						<Button variant="secondary" onclick={() => openMessage('comment')}>Message tenant</Button>
					{/if}
					<Button variant="secondary" onclick={() => openMessage('note')}>Add note</Button>
					{#if isOpen(t)}
						<Button onclick={() => openMessage('complete')}>Mark complete</Button>
						<Button variant="danger" onclick={() => openMessage('cancel')}>Cancel repair</Button>
					{/if}
				</div>
			{/if}
		</div>

		<div class="grid gap-4 lg:grid-cols-[2fr_1fr]">
			<div class="flex flex-col gap-4">
				<section class="card bg-base-100 shadow-sm" aria-labelledby="ticket-issue">
					<div class="card-body">
						<h3 id="ticket-issue" class="font-semibold">Issue</h3>
						<p class="whitespace-pre-line">{t.description}</p>
						<h4 class="mt-2 font-semibold">Photos and recordings</h4>
						<MediaGallery base={`${apiBase}/files`} files={detail.files} />
					</div>
				</section>
				<section class="card bg-base-100 shadow-sm" aria-labelledby="ticket-timeline">
					<div class="card-body">
						<h3 id="ticket-timeline" class="font-semibold">Timeline</h3>
						<ol class="flex flex-col gap-3">
							{#each [...detail.events].reverse() as e (e.id)}
								<li class={['rounded-box p-3', e.tenant_visible ? 'bg-base-200' : 'border border-dashed border-warning bg-warning/5']}>
									<div class="flex flex-wrap items-center gap-2 text-sm">
										<span class="font-semibold">{eventLabel(e)}</span>
										{#if !e.tenant_visible}<span class="badge badge-warning badge-soft badge-sm">Internal</span>{/if}
										<span class="text-base-content/70">{e.actor_name} · {new Date(e.created_at).toLocaleString()}</span>
									</div>
									{#if e.body}<p class="mt-1 whitespace-pre-line">{e.body}</p>{/if}
								</li>
							{/each}
						</ol>
					</div>
				</section>
			</div>
			<aside class="flex flex-col gap-4">
				<section class="card bg-base-100 shadow-sm" aria-labelledby="ticket-visit">
					<div class="card-body gap-2">
						<h3 id="ticket-visit" class="font-semibold">Visit</h3>
						{#if detail.open_visit}
							{@const v = detail.open_visit}
							<p class="font-semibold">{visitWindow(v.starts_at, v.ends_at)}</p>
							<p class="text-sm">{v.supplier.name} · {v.status === 'confirmed' ? 'Confirmed by tenant' : v.status === 'change_requested' ? 'Tenant asked for a different time' : 'Waiting for tenant to confirm'}</p>
							{#if v.change_note}<p class="rounded-box bg-warning/10 p-2 text-sm">“{v.change_note}”</p>{/if}
						{:else}
							<p class="text-base-content/70">No visit booked.</p>
						{/if}
						<EntryPanel visit={detail.open_visit} preference={detail.entry_preference} accessNotes={t.access_notes} />
						{#if detail.availability.length}
							<h4 class="mt-1 text-sm font-semibold">Times that suit the tenant</h4>
							<ul class="text-sm">{#each detail.availability as w, i (i)}<li>{windowLabel(w)}</li>{/each}</ul>
						{/if}
						{#if visitError}<p class="text-sm text-error" role="alert">{visitError}</p>{/if}
						{#if canWrite && isOpen(t)}
							<div class="flex flex-wrap gap-2">
								{#if detail.open_visit}
									{@const v = detail.open_visit}
									<Button size="sm" variant="secondary" onclick={() => { rescheduling = true; visitOpen = true; }}>Reschedule</Button>
									<Button size="sm" variant="secondary" onclick={() => visitDone(v)}>Mark visit done</Button>
									<Button size="sm" variant="danger" onclick={() => openMessage('cancel_visit')}>Cancel visit</Button>
								{:else if t.supplier}
									<Button size="sm" onclick={() => { rescheduling = false; visitOpen = true; }}>Book visit</Button>
								{/if}
							</div>
						{/if}
					</div>
				</section>
				<section class="card bg-base-100 shadow-sm" aria-labelledby="ticket-people">
					<div class="card-body gap-2">
						<h3 id="ticket-people" class="font-semibold">Tenants</h3>
						{#each detail.tenants as tn (tn.id)}
							<div><p>{tn.name}</p>{#if tn.email}<p class="text-sm"><a class="link" href={`mailto:${tn.email}`}>{tn.email}</a></p>{/if}{#if tn.phone}<p class="text-sm"><a class="link" href={`tel:${tn.phone}`}>{tn.phone}</a></p>{/if}</div>
						{:else}
							<p class="text-base-content/70">No lease on this repair.</p>
						{/each}
						<h3 class="mt-2 font-semibold">Tradesperson</h3>
						<p>{t.supplier?.name ?? 'Not assigned'}</p>
					</div>
				</section>
			</aside>
		</div>

		{#if canWrite}
			<AssignSupplierDialog bind:open={assigning} {org} ticket={t} onsaved={load} />
			<TicketMessageDialog bind:open={messageOpen} {org} ticketId={t.id} visitId={detail.open_visit?.id} {action} onsaved={load} />
			<VisitDialog bind:open={visitOpen} {org} ticket={t} visit={rescheduling ? detail.open_visit : null} onsaved={load} />
		{/if}
	{/if}
</LoadState>

<script lang="ts">
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { api, ApiError } from '../../../../../lib/api';
	import { useUnsavedChanges } from '../../../../../lib/forms/unsaved.svelte';
	import PageHeader from '../../../../../lib/components/PageHeader.svelte';
	import Field from '../../../../../lib/components/Field.svelte';
	import Button from '../../../../../lib/components/Button.svelte';
	import LoadState from '../../../../../lib/components/LoadState.svelte';
	import MediaPicker from '../../../../../lib/modules/property/MediaPicker.svelte';
	import { CATEGORY_OPTIONS, URGENCY_LABEL, type Ticket, type Urgency } from '../../../../../lib/modules/property/maintenance';

	type List = { emergency_phone: string; leases: { id: number; property_address: string; active: boolean }[] };

	const org = $derived(page.params.org ?? '');
	const back = $derived(`/portal/${org}/repairs`);

	let info = $state<List | null>(null);
	let loadError = $state('');
	let leaseId = $state('');
	let title = $state('');
	let description = $state('');
	let category = $state('plumbing');
	let urgency = $state<Urgency>('routine');
	let accessNotes = $state('');
	let media = $state<{ busy: boolean; pendingIds: number[] }>({ busy: false, pendingIds: [] });
	let saving = $state(false);
	let error = $state('');

	const activeLeases = $derived(info?.leases.filter((l) => l.active) ?? []);
	const dirty = $derived(!!(title || description || accessNotes || media.pendingIds.length || media.busy));
	const { allowLeave } = useUnsavedChanges(() => dirty && !saving, () => saving);

	async function load() {
		loadError = '';
		try {
			info = await api<List>('GET', `/api/v1/portal/${org}/maintenance`);
			leaseId = String(info.leases.find((l) => l.active)?.id ?? '');
		} catch (e) {
			loadError = e instanceof ApiError ? e.message : 'Could not load your leases.';
		}
	}
	$effect(() => { org; load(); });

	async function submit(e: SubmitEvent) {
		e.preventDefault();
		if (saving || media.busy) return;
		saving = true; error = '';
		try {
			const t = await api<Ticket>('POST', `/api/v1/portal/${org}/maintenance`, {
				lease_id: Number(leaseId), title, description, category, urgency, access_notes: accessNotes, pending_file_ids: media.pendingIds
			});
			saving = false;
			title = description = accessNotes = '';
			media = { busy: false, pendingIds: [] };
			goto(`/portal/${org}/repairs/${t.id}`);
		} catch (err) {
			error = err instanceof ApiError ? err.message : 'Could not submit the repair.';
			saving = false;
		}
	}

	function cancel() { if (allowLeave()) goto(back); }
	const tel = (p: string) => `tel:${p.replace(/[^\d+]/g, '')}`;
</script>

<PageHeader title="Report a repair" back={{ href: back, label: 'Back to repairs' }} />

<LoadState loading={!info && !loadError} error={loadError} loadingLabel="Loading…" onretry={load}>
	{#if info}
		{#if activeLeases.length === 0}
			<p class="text-base-content/70">You can report repairs for a current lease.</p>
		{:else}
			<form class="flex max-w-xl flex-col gap-4" onsubmit={submit}>
				{#if error}<p class="alert alert-error alert-soft" role="status">{error}</p>{/if}
				<fieldset disabled={saving} class="flex flex-col gap-4">
					{#if activeLeases.length > 1}
						<Field label="Property" id="repair-lease">
							<select class="select w-full" id="repair-lease" bind:value={leaseId}>
								{#each activeLeases as l (l.id)}<option value={String(l.id)}>{l.property_address}</option>{/each}
							</select>
						</Field>
					{/if}
					<Field label="What's wrong?" id="repair-title" hint="A short summary, like “Kitchen tap leaking”.">
						<input class="input w-full" id="repair-title" maxlength="120" bind:value={title} required />
					</Field>
					<Field label="Details" id="repair-description" hint="Where it is, when it started and what you've tried.">
						<textarea class="textarea w-full" id="repair-description" rows="4" maxlength="4000" bind:value={description} required></textarea>
					</Field>
					<Field label="Type" id="repair-category">
						<select class="select w-full" id="repair-category" bind:value={category}>
							{#each CATEGORY_OPTIONS as o (o.value)}<option value={o.value}>{o.label}</option>{/each}
						</select>
					</Field>
					<fieldset class="fieldset">
						<legend class="fieldset-legend">How urgent?</legend>
						<div class="join w-full">
							{#each Object.entries(URGENCY_LABEL) as [value, label] (value)}
								<input class={['join-item btn flex-1', value === 'urgent' && urgency === 'urgent' && 'btn-error']} type="radio" name="urgency" {value} aria-label={label} bind:group={urgency} />
							{/each}
						</div>
					</fieldset>
					{#if urgency === 'urgent'}
						<div class="alert alert-error alert-soft flex-col items-start" role="alert">
							{#if info.emergency_phone}
								<p><strong>Burst pipe, gas leak, no power or hot water, or anything unsafe?</strong> Call <a class="link font-semibold" href={tel(info.emergency_phone)}>{info.emergency_phone}</a> now, then log it here.</p>
							{:else}
								<p><strong>Burst pipe, gas leak, no power or hot water, or anything unsafe?</strong> Phone your property manager now, then log it here.</p>
							{/if}
						</div>
					{/if}
					<fieldset class="fieldset">
						<legend class="fieldset-legend">Photos, video or a voice note</legend>
						<MediaPicker uploadUrl={`/api/v1/portal/${org}/maintenance/uploads`} deleteUrl={(id) => `/api/v1/portal/${org}/maintenance/uploads/${id}`} disabled={saving} onchange={(m) => (media = m)} />
					</fieldset>
					<Field label="Access notes (optional)" id="repair-access" hint="Keys, alarm, pets or parking.">
						<textarea class="textarea w-full" id="repair-access" rows="2" maxlength="1000" bind:value={accessNotes}></textarea>
					</Field>
				</fieldset>
				<div class="flex gap-2">
					<Button variant="secondary" disabled={saving} onclick={cancel}>Cancel</Button>
					<Button type="submit" busy={saving} disabled={media.busy}>{media.busy ? 'Waiting for uploads…' : saving ? 'Submitting…' : 'Submit repair'}</Button>
				</div>
			</form>
		{/if}
	{/if}
</LoadState>

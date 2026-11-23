<script lang="ts">
	// AskPanel asks a strata role's assistant one standalone question about
	// the scheme's documents and shows the cited answer. Nothing is saved:
	// the next question replaces the answer, and Cancel abandons a question
	// in flight (aborting the request, which stops the server's work).
	import { api, ApiError } from '../../../api';
	import Button from '../../../components/Button.svelte';
	import { date } from '../../../format';
	import { renderMarkdown } from '../../../markdown';
	import { session } from '../../../session.svelte';

	type Role = 'chairperson' | 'treasurer';
	type Citation = { document_id: number; title: string; document_date: string; page_no: number };
	type Answer = { answer: string; citations: Citation[]; grounded: boolean };

	let { org, schemeId, role }: { org: string; schemeId: number; role: Role } = $props();

	const MAX = 2000;
	const placeholders: Record<Role, string> = {
		chairperson: 'e.g. What did the committee decide about the roof in 2024?',
		treasurer: 'e.g. What was the sinking fund balance at the end of FY25?'
	};

	let question = $state('');
	let answer = $state<Answer | null>(null);
	let error = $state('');
	let busy = $state(false);
	let controller: AbortController | null = null;
	let seq = 0;
	const fieldId = $derived(`strata-ask-${role}-${schemeId}`);

	// A different scheme or org abandons the question in flight.
	$effect(() => {
		org; schemeId;
		return () => { seq++; controller?.abort(); controller = null; busy = false; answer = null; error = ''; };
	});

	async function submit(e: SubmitEvent) {
		e.preventDefault();
		const q = question.trim();
		if (busy || q === '') return;
		const token = ++seq;
		controller = new AbortController();
		busy = true; error = '';
		try {
			const got = await api<Answer>('POST', `/api/v1/orgs/${org}/strata-schemes/${schemeId}/ask`,
				{ role, question: q }, { signal: controller.signal });
			if (token === seq) answer = got;
		} catch (err) {
			if (token !== seq || (err instanceof DOMException && err.name === 'AbortError')) return;
			error = err instanceof ApiError ? err.message : 'Could not get an answer. Try again.';
		} finally {
			if (token === seq) { busy = false; controller = null; }
		}
	}

	function cancel() {
		seq++;
		controller?.abort();
		controller = null;
		busy = false;
	}

	const citationHref = (c: Citation) =>
		`/api/v1/orgs/${org}/strata-schemes/${schemeId}/documents/${c.document_id}/file#page=${c.page_no}`;
</script>

{#if !session.strataAssistant}
	<p class="text-base-content/70">The assistant is not configured.</p>
{:else}
	<form class="grid gap-2" onsubmit={submit} aria-busy={busy || undefined}>
		<label class="font-medium" for={fieldId}>Question</label>
		<textarea class="textarea w-full" id={fieldId} rows="3" maxlength={MAX} placeholder={placeholders[role]}
			bind:value={question} readonly={busy}></textarea>
		<div class="flex flex-wrap gap-2">
			<Button type="submit" {busy} disabled={question.trim() === ''}>{busy ? 'Asking…' : 'Ask'}</Button>
			{#if busy}<Button variant="secondary" onclick={cancel}>Cancel</Button>{/if}
		</div>
	</form>
	{#if busy}<p role="status" class="mt-2 text-base-content/70">Reading the scheme's documents…</p>{/if}
	{#if error}<p class="alert alert-error alert-soft mt-2" role="alert">{error}</p>{/if}
	{#if answer && !busy}
		<div class="mt-3 grid gap-2" aria-live="polite">
			{#if !answer.grounded}
				<p class="alert alert-warning alert-soft" role="note">No cited source supports this answer — check the documents.</p>
			{/if}
			<!-- renderMarkdown sanitises the model's markdown before insertion. -->
			<div class="grid gap-2 [&_ol]:list-decimal [&_ol]:pl-6 [&_table]:table [&_table]:table-sm [&_ul]:list-disc [&_ul]:pl-6">{@html renderMarkdown(answer.answer)}</div>
			{#if answer.citations.length}
				<ol class="list-decimal pl-6 text-sm">
					{#each answer.citations as c (`${c.document_id}-${c.page_no}`)}
						<li><a class="link" href={citationHref(c)} target="_blank" rel="noopener noreferrer">{c.title} — {date(c.document_date)}, p. {c.page_no}</a></li>
					{/each}
				</ol>
			{/if}
		</div>
	{/if}
{/if}

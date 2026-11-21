<script lang="ts" module>
	// Shape of the dashboard response's "timesheets" section
	// (internal/timesheets/routes.go's timesheetsDashboard).
	export type TimesheetsDashboard = {
		unbilled_minutes: number;
		unbilled_estimate: number;
		project_count: number;
		projects: {
			id: number;
			name: string;
			client_name: string;
			billed_minutes: number;
			unbilled_minutes: number;
		}[];
	};
</script>

<script lang="ts">
	import Money from '../../components/Money.svelte';
	import { hhmm } from './types';

	let { data, org }: { data: TimesheetsDashboard; org: string } = $props();
</script>

<section class="card min-w-0 overflow-hidden border border-base-300 bg-base-100 shadow-sm">
	<header class="border-b border-base-300 bg-base-200/50 px-5 py-3">
		<h2 class="m-0 text-lg font-semibold">Projects</h2>
	</header>
	<div class="border-t-2 border-primary px-5 py-4 text-center">
		<p class="m-0 text-sm text-base-content/70">Unbilled Hours</p>
		<p class="m-0 mt-1 text-xl font-semibold tabular-nums">{hhmm(data.unbilled_minutes)}</p>
		<p class="m-0 mt-1 text-sm text-base-content/70">Estimated value: <Money cents={data.unbilled_estimate} /></p>
	</div>
	{#if data.projects.length === 0}
		<p class="m-0 px-5 pb-5 text-sm text-base-content/70">No active projects with unbilled time.</p>
	{:else}
		<ul class="m-0 space-y-5 p-5 pt-2">
			{#each data.projects as project (project.id)}
				{@const total = project.billed_minutes + project.unbilled_minutes}
				{@const percent = total > 0 ? Math.round(project.billed_minutes / total * 100) : 0}
				<li>
					<a class="flex min-w-0 items-center gap-5 rounded-field hover:bg-base-200/60" href={`/${org}/projects/${project.id}`}>
						<div class="radial-progress shrink-0 text-primary shadow-[inset_0_0_0_0.4rem_var(--color-base-300)]" style={`--value:${percent}; --size:4.5rem; --thickness:0.4rem;`} role="progressbar" aria-label={`${project.name} hours billed`} aria-valuenow={percent} aria-valuemin="0" aria-valuemax="100">
							<span class="text-sm font-medium text-base-content">{percent}%</span>
						</div>
						<div class="min-w-0">
							<p class="m-0 break-words text-sm font-medium">{project.name}</p>
							<p class="m-0 mt-1 break-words text-sm text-base-content/70">{project.client_name}</p>
						</div>
					</a>
				</li>
			{/each}
		</ul>
	{/if}
	{#if data.project_count > 5}
		<div class="mt-auto border-t border-base-300 px-5 py-3">
			<a class="link link-primary text-sm" href={`/${org}/projects`}>View all projects</a>
		</div>
	{/if}
</section>

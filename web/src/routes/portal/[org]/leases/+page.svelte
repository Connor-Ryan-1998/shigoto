<script lang="ts">
	import { page } from '$app/state';
	import { goto } from '$app/navigation';

	// The Leases tab moved into Rent; keep old links working.
	$effect(() => {
		goto(`/portal/${page.params.org}/rent`, { replaceState: true });
	});
</script>

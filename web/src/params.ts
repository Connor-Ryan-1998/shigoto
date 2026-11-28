import { defineParams } from '@sveltejs/kit/params';

// org is an organisation slug in a staff URL. `api` is never one: without
// this the `[org=org]/[...rest]` catch-all matches /api/... links, so the
// client router renders "Not found" instead of letting the browser fetch
// the file. Returning undefined rejects the segment.
export const org = (param: string) => (param === 'api' ? undefined : param);

export const params = defineParams({ org });

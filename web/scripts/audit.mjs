// Runs `npm audit` and fails on any advisory that is not explicitly excepted below.
// Each exception is scoped to one advisory on one package and expires; expired
// exceptions fail the audit so they must be re-reviewed rather than forgotten.
import { execFileSync } from 'node:child_process';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const exceptions = [
	{
		advisory: 'GHSA-gcqx-w48m-w2h4',
		package: 'deepmerge',
		expires: '2027-02-15',
		owner: 'Connor Ryan',
		reason:
			'Build-time only: @sveltejs/vite-plugin-svelte merges repository-controlled Svelte config. ' +
			'Not in the shipped bundle or Go binary. deepmerge 4.3.1 is the latest release and the ' +
			'latest plugin still depends on it; no fixed version exists.'
	}
];

const webDir = resolve(dirname(fileURLToPath(import.meta.url)), '..');

function runAudit() {
	try {
		return execFileSync('npm', ['audit', '--json'], { cwd: webDir, encoding: 'utf8' });
	} catch (error) {
		// npm audit exits nonzero when it finds advisories; the JSON is still on stdout.
		if (typeof error.stdout === 'string' && error.stdout.trim().startsWith('{')) return error.stdout;
		throw error;
	}
}

const report = JSON.parse(runAudit());
if (report.error) {
	console.error(`npm audit failed: ${JSON.stringify(report.error)}`);
	process.exit(1);
}

const today = new Date().toISOString().slice(0, 10);
const failures = [];
const used = new Set();
const matched = new Set();

for (const vulnerability of Object.values(report.vulnerabilities ?? {})) {
	for (const via of vulnerability.via) {
		// String entries point at another package's advisory and are judged there.
		if (typeof via === 'string') continue;
		const id = via.url?.split('/').pop() ?? String(via.source);
		const exception = exceptions.find((e) => e.advisory === id && e.package === via.name);
		if (exception) matched.add(exception);
		if (!exception) {
			failures.push(`${via.name}: ${via.title} (${via.url}) [${via.severity}]`);
		} else if (exception.expires < today) {
			failures.push(`${via.name}: exception for ${id} expired on ${exception.expires}; re-review it`);
		} else {
			used.add(exception);
		}
	}
}

for (const exception of used) {
	console.log(
		`Excepted ${exception.advisory} in ${exception.package} until ${exception.expires} (${exception.owner}): ${exception.reason}`
	);
}
for (const exception of exceptions.filter((e) => !matched.has(e))) {
	console.log(`Note: exception ${exception.advisory} no longer matches any finding; remove it.`);
}

if (failures.length > 0) {
	console.error('npm audit found advisories without an exception:');
	for (const failure of failures) console.error(`  - ${failure}`);
	process.exit(1);
}
console.log('npm audit: no unexcepted advisories.');

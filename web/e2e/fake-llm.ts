// fake-llm is a scripted OpenAI-compatible provider for Playwright runs of
// the strata assistant: extraction requests get a valid transcription of
// the requested pages (with one roof motion for minutes), the agent first
// calls find_motions and then answers citing the first motion it got back,
// and embeddings are deterministic 1536-length vectors.

import { createServer, type IncomingMessage } from 'node:http';
import { once } from 'node:events';
import type { AddressInfo } from 'node:net';

type Message = { role: string; content: unknown };

function readBody(req: IncomingMessage): Promise<string> {
	return new Promise((ok, fail) => {
		let data = '';
		req.on('data', (c) => (data += c));
		req.on('end', () => ok(data));
		req.on('error', fail);
	});
}

function texts(content: unknown): string[] {
	if (typeof content === 'string') return [content];
	if (Array.isArray(content)) return content.map((p) => (typeof p?.text === 'string' ? p.text : ''));
	return [];
}

function extraction(messages: Message[]) {
	const instruction = messages.flatMap((m) => texts(m.content)).find((t) => /pages \d+-\d+/.test(t)) ?? '';
	const [, a, b] = instruction.match(/pages (\d+)-(\d+)/) ?? ['', '1', '1'];
	const from = Number(a), to = Number(b);
	const pages = [];
	for (let p = from; p <= to; p++) pages.push({ page_no: p, text: `Page ${p}. Motion 5: that the body corporate replace the roof membrane. Carried.` });
	const minutes = instruction.includes('"minutes"');
	return {
		pages,
		...(minutes
			? { motions: [{ meeting_date: '2024-09-12', meeting_kind: 'agm', motion_no: '5', text: 'Replace the roof membrane', outcome: 'carried', page_no: from }] }
			: {})
	};
}

function agentReply(messages: Message[]) {
	const tool = [...messages].reverse().find((m) => m.role === 'tool');
	if (!tool) {
		return { content: null, tool_calls: [{ id: 'call_1', type: 'function', function: { name: 'find_motions', arguments: '{"query":"roof"}' } }] };
	}
	const result = texts(tool.content).join('');
	const doc = result.match(/"document_id":(\d+)/)?.[1];
	const page = result.match(/"page_no":(\d+)/)?.[1];
	const cite = doc && page ? ` [doc:${doc} p:${page}]` : '';
	return { content: `At the 2024 AGM the committee resolved to replace the roof membrane (motion 5, carried)${cite}.` };
}

function vector(input: string): number[] {
	const v: number[] = [];
	let h = 2166136261;
	for (let i = 0; i < 1536; i++) {
		for (const ch of `${i}:${input}`) h = Math.imul(h ^ ch.charCodeAt(0), 16777619);
		v.push(((h >>> 0) % 2000) / 1000 - 1);
	}
	return v;
}

export async function startFakeLLM(): Promise<{ url: string; stop(): Promise<void> }> {
	const server = createServer(async (req, res) => {
		const body = JSON.parse((await readBody(req)) || '{}');
		res.setHeader('Content-Type', 'application/json');
		if (req.url === '/v1/chat/completions') {
			const messages = (body.messages ?? []) as Message[];
			const message = body.response_format
				? { content: JSON.stringify(extraction(messages)) }
				: agentReply(messages);
			res.end(JSON.stringify({ choices: [{ index: 0, message: { role: 'assistant', ...message } }], usage: { prompt_tokens: 1, completion_tokens: 1 } }));
		} else if (req.url === '/v1/embeddings') {
			const inputs = (body.input ?? []) as string[];
			res.end(JSON.stringify({ data: inputs.map((s, index) => ({ index, embedding: vector(s) })) }));
		} else {
			res.statusCode = 404;
			res.end('{"error":"not found"}');
		}
	});
	server.listen(0, '127.0.0.1');
	await once(server, 'listening');
	const { port } = server.address() as AddressInfo;
	return {
		url: `http://127.0.0.1:${port}/v1`,
		async stop() {
			server.close();
			await once(server, 'close');
		}
	};
}

// pdf builds a minimal valid PDF with one page per entry, like
// internal/testkit.PDF.
export function pdf(pages: string[]): Buffer {
	const objs = [
		'<< /Type /Catalog /Pages 2 0 R >>',
		`<< /Type /Pages /Kids [${pages.map((_, i) => `${4 + 2 * i} 0 R`).join(' ')}] /Count ${pages.length} >>`,
		'<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>'
	];
	pages.forEach((text, i) => {
		const stream = `BT /F1 12 Tf 72 720 Td (${text.replace(/[\\()]/g, (c) => `\\${c}`)}) Tj ET`;
		objs.push(`<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 3 0 R >> >> /Contents ${5 + 2 * i} 0 R >>`);
		objs.push(`<< /Length ${stream.length} >>\nstream\n${stream}\nendstream`);
	});
	let out = '%PDF-1.4\n';
	const offsets: number[] = [];
	objs.forEach((o, i) => {
		offsets.push(out.length);
		out += `${i + 1} 0 obj\n${o}\nendobj\n`;
	});
	const xref = out.length;
	out += `xref\n0 ${objs.length + 1}\n0000000000 65535 f \n`;
	for (const off of offsets) out += `${String(off).padStart(10, '0')} 00000 n \n`;
	out += `trailer\n<< /Size ${objs.length + 1} /Root 1 0 R >>\nstartxref\n${xref}\n%%EOF\n`;
	return Buffer.from(out, 'latin1');
}

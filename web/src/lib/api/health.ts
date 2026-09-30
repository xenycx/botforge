export type Readiness = {
	status: string;
	checks?: Record<string, string>;
};

/** GET /api/v1/readyz. A 503 still carries a JSON body describing failed checks. */
export async function fetchReadiness(signal?: AbortSignal): Promise<Readiness> {
	const res = await fetch('/api/v1/readyz', { signal, headers: { accept: 'application/json' } });
	const ct = res.headers.get('content-type') ?? '';
	if (!ct.includes('application/json')) throw new Error(`unexpected response (${res.status})`);
	return (await res.json()) as Readiness;
}

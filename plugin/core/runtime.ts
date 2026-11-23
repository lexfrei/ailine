export type FileInfo = {
	kind: "file" | "directory" | "other";
	size: number;
	mtimeMs: number;
};

export type FileReader = {
	stat(path: string): Promise<FileInfo | undefined>;
	read(
		path: string,
		offset?: number,
		length?: number,
	): Promise<Uint8Array | undefined>;
};

export type CacheKey =
	| "status"
	| "usage"
	| "usage-last-good"
	| "usage-retry-after"
	| "usage-auth-failed";

// Storage failures are cache misses; writes are best effort and must not reject.
export type CacheStore = {
	read(
		key: CacheKey,
		ttlMs: number | undefined,
		nowMs: number,
	): Promise<string | undefined>;
	write(key: CacheKey, value: string): Promise<void>;
};

export type HttpResponse = {
	status: number;
	headers: Readonly<Record<string, string>>;
	body: string;
};

export type HttpGet = (
	url: string,
	headers: Readonly<Record<string, string>>,
	timeoutMs: number,
) => Promise<HttpResponse>;

export type StatusRuntime = {
	cache: CacheStore;
	httpGet: HttpGet;
	now(): number;
};

export type UsageRuntime = StatusRuntime & {
	getToken(): Promise<string | undefined>;
	hashToken(value: string): Promise<string>;
};

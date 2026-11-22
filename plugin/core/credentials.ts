import { toWellFormed } from "./text";
import { isRecord, parseJson } from "./json";

export type CredentialCommand = (
	argv: readonly string[],
	timeoutMs: number,
) => Promise<{ exitCode: number; stdout: string }>;

export function parseCredentialToken(raw: string): string | undefined {
	let value: unknown;
	try {
		value = parseJson(raw);
	} catch {
		throw new Error("parsing keychain data");
	}
	if (value === null) return undefined;
	if (!isRecord(value)) throw new Error("parsing keychain data");
	const oauth = value.claudeAiOauth;
	if (oauth === undefined || oauth === null) return undefined;
	if (!isRecord(oauth)) throw new Error("parsing keychain data");
	const token = oauth.accessToken;
	if (token === undefined || token === null || token === "") return undefined;
	if (typeof token !== "string") throw new Error("parsing keychain data");
	return toWellFormed(token);
}

export async function readKeychain(
	run: CredentialCommand,
	username: string,
): Promise<string | undefined> {
	if (username === "") return undefined;
	let result: { exitCode: number; stdout: string };
	try {
		result = await run(
			[
				"/usr/bin/security",
				"find-generic-password",
				"-s",
				"Claude Code-credentials",
				"-a",
				username,
				"-w",
			],
			5000,
		);
	} catch {
		throw new Error("reading keychain");
	}
	if (result.exitCode !== 0) throw new Error("reading keychain");
	return parseCredentialToken(result.stdout);
}

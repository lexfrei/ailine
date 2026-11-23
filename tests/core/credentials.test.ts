import { expect, test } from "bun:test";
import {
	parseCredentialToken,
	readKeychain,
	type CredentialCommand,
} from "../../plugin/core/credentials";

for (const body of [
	"{}",
	"null",
	'{"claudeAiOauth":null}',
	'{"claudeAiOauth":{}}',
	'{"claudeAiOauth":{"accessToken":""}}',
]) {
	test(`missing OAuth token ${body} remains unavailable`, () => {
		expect(parseCredentialToken(body)).toBeUndefined();
	});
}

test("reads only the nested OAuth access token", () => {
	expect(
		parseCredentialToken(
			'{"claudeAiOauth":{"accessToken":"test-token","refreshToken":"ignored-test-data"}}\n',
		),
	).toBe("test-token");
});

for (const body of [
	"malformed private-test-data",
	"[]",
	'{"claudeAiOauth":true}',
	'{"claudeAiOauth":{"accessToken":3}}',
]) {
	test(`invalid credential structures fail without echoing contents: ${body.length}`, () => {
		try {
			parseCredentialToken(body);
			expect.unreachable();
		} catch (error) {
			expect(error).toBeInstanceOf(Error);
			expect(String(error)).not.toContain(body);
			expect(String(error)).not.toContain("private-test-data");
		}
	});
}

test("uses the fixed OS executable, literal arguments and bounded timeout", async () => {
	const calls: { argv: readonly string[]; timeoutMs: number }[] = [];
	const run: CredentialCommand = async (argv, timeoutMs) => {
		calls.push({ argv, timeoutMs });
		return {
			exitCode: 0,
			stdout: '{"claudeAiOauth":{"accessToken":"test-token"}}',
		};
	};
	expect(await readKeychain(run, "test-user; not-a-command")).toBe(
		"test-token",
	);
	expect(calls).toEqual([
		{
			argv: [
				"/usr/bin/security",
				"find-generic-password",
				"-s",
				"Claude Code-credentials",
				"-a",
				"test-user; not-a-command",
				"-w",
			],
			timeoutMs: 5000,
		},
	]);
});

test("failed command does not parse or expose partial credential output", async () => {
	try {
		await readKeychain(
			async () => ({ exitCode: 1, stdout: "private-partial-output" }),
			"test-user",
		);
		expect.unreachable();
	} catch (error) {
		expect(String(error)).toContain("reading keychain");
		expect(String(error)).not.toContain("private-partial-output");
	}
});

test("missing account prevents starting the OS command", async () => {
	let calls = 0;
	const token = await readKeychain(async () => {
		calls++;
		return { exitCode: 0, stdout: "{}" };
	}, "");
	expect(token).toBeUndefined();
	expect(calls).toBe(0);
});

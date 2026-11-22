import { expect, test } from "bun:test";
import { getToken, runCredentialCommand } from "../../cmd/ailine/keychain";

const body = '{"claudeAiOauth":{"accessToken":"test-token"}}';

test("non-macOS platforms never inspect the account or start a process", async () => {
	let calls = 0;
	const token = await getToken(
		"linux",
		() => {
			calls++;
			return "test-user";
		},
		async () => {
			calls++;
			return { exitCode: 0, stdout: body };
		},
	);
	expect(token).toBeUndefined();
	expect(calls).toBe(0);
});

test("account lookup errors do not expose output or start a process", async () => {
	let calls = 0;
	try {
		await getToken(
			"darwin",
			() => {
				throw new Error("private-test-account");
			},
			async () => {
				calls++;
				return { exitCode: 0, stdout: body };
			},
		);
		expect.unreachable();
	} catch (error) {
		expect(String(error)).toContain("getting current user");
		expect(String(error)).not.toContain("private-test-account");
		expect(calls).toBe(0);
	}
});

test("macOS reads only through the injected command runner", async () => {
	const calls: (readonly string[])[] = [];
	expect(
		await getToken(
			"darwin",
			() => "test-user",
			async (argv) => {
				calls.push(argv);
				return { exitCode: 0, stdout: body };
			},
		),
	).toBe("test-token");
	expect(calls[0]?.[0]).toBe("/usr/bin/security");
});

test("command runner captures stdout without requiring a shell", async () => {
	const result = await runCredentialCommand(
		[process.execPath, "--eval", 'process.stdout.write("test-output")'],
		2000,
	);
	expect(result).toEqual({ exitCode: 0, stdout: "test-output" });
});

test("command runner retains nonzero exit status for its caller", async () => {
	const result = await runCredentialCommand(
		[
			process.execPath,
			"--eval",
			'process.stdout.write("partial-test-data"); process.exitCode = 3',
		],
		2000,
	);
	expect(result).toEqual({ exitCode: 3, stdout: "partial-test-data" });
});

test("command timeout kills a nonterminating test child", async () => {
	const started = Date.now();
	const result = await runCredentialCommand(
		[process.execPath, "--eval", "setInterval(() => {}, 1000)"],
		50,
	);
	expect(result.exitCode).not.toBe(0);
	expect(Date.now() - started).toBeLessThan(2000);
});

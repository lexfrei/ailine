import { userInfo } from "node:os";
import {
	readKeychain,
	type CredentialCommand,
} from "../../plugin/core/credentials";

export const runCredentialCommand: CredentialCommand = async (
	argv,
	timeoutMs,
) => {
	const child = Bun.spawn({
		cmd: [...argv],
		stdin: "ignore",
		stdout: "pipe",
		stderr: "ignore",
		timeout: timeoutMs,
		killSignal: "SIGKILL",
	});
	const [stdout, exitCode] = await Promise.all([
		new Response(child.stdout).text(),
		child.exited,
	]);
	return { exitCode, stdout };
};

export async function getToken(
	platform: string = process.platform,
	username: () => string = () => userInfo().username,
	run: CredentialCommand = runCredentialCommand,
): Promise<string | undefined> {
	if (platform !== "darwin") return undefined;
	let account: string;
	try {
		account = username();
	} catch {
		throw new Error("getting current user");
	}
	return readKeychain(run, account);
}

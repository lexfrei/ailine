import { toWellFormed, trimSpace } from "./text";

const terminalsWithHyperlinks = new Set([
	"iTerm.app",
	"WezTerm",
	"mintty",
	"ghostty",
	"vscode",
	"Hyper",
	"rio",
	"Tabby",
	"WarpTerminal",
]);

function atLeast(raw: string | undefined, minimum: number): boolean {
	const trimmed = trimSpace(raw ?? "");
	if (!/^[+-]?\d+$/.test(trimmed)) return false;
	const value = BigInt(trimmed);
	return value >= BigInt(minimum) && value <= 9223372036854775807n;
}

export function supportsHyperlinks(
	env: Readonly<Record<string, string | undefined>>,
): boolean {
	if (env.TMUX || env.STY || env.ZELLIJ) return false;
	if (terminalsWithHyperlinks.has(env.TERM_PROGRAM ?? "")) return true;
	const term = env.TERM ?? "";
	if (
		term === "xterm-kitty" ||
		term.startsWith("foot") ||
		term.startsWith("alacritty")
	)
		return true;
	if (env.KITTY_WINDOW_ID || env.WT_SESSION || env.DOMTERM) return true;
	return atLeast(env.VTE_VERSION, 5000) || atLeast(env.KONSOLE_VERSION, 200400);
}

export function link(text: string, url: string, enabled: boolean): string {
	if (!enabled || (!url.startsWith("https://") && !url.startsWith("http://")))
		return text;
	// Lone UTF-16 surrogates cannot represent the valid UTF-8 Go requires.
	if (toWellFormed(url) !== url) return text;
	for (const character of url) {
		const code = character.codePointAt(0) ?? 0;
		if (code < 0x20 || (code >= 0x7f && code <= 0x9f)) return text;
	}
	return `\x1b]8;;${url}\x07${text}\x1b]8;;\x07`;
}

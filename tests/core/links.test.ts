import { expect, test } from "bun:test";
import { link, supportsHyperlinks } from "../../plugin/core/links";
import { visualWidth } from "../../plugin/core/wrap";

test("OSC8 exact bytes, disabled mode, raw URL preservation and colors", () => {
	const url = "https://example.com/pull/42";
	expect(link("#42", url, false)).toBe("#42");
	expect(link("#42", url, true)).toBe(`\x1b]8;;${url}\x07#42\x1b]8;;\x07`);
	for (const raw of [
		"http://example.com",
		"https://EXAMPLE.com:443/a/../b",
		"https://example.com/путь",
		"https://example.com/%1b",
		"https://",
		"https://example.com/a b",
	])
		expect(link("x", raw, true)).toBe(`\x1b]8;;${raw}\x07x\x1b]8;;\x07`);
	expect(link("\x1b[31m#42\x1b[0m", url, true)).toBe(
		`\x1b]8;;${url}\x07\x1b[31m#42\x1b[0m\x1b]8;;\x07`,
	);
	expect(visualWidth(link("🐙 o/r", url, true))).toBe(visualWidth("🐙 o/r"));
	expect(visualWidth(link("\x1b[31m#42\x1b[0m", url, true))).toBe(3);
});
test("reject unsafe URLs, every C0/DEL/C1 code point and malformed UTF-16", () => {
	for (const url of [
		"",
		"example.com",
		"file:///etc/passwd",
		"javascript:alert(1)",
		"HTTPS://example.com",
		" https://example.com",
		"https://example.com/\ud800",
		"https://example.com/\udc00",
		"https://example.com/\ud800x",
	])
		expect(link("x", url, true)).toBe("x");
	for (let code = 0; code <= 0x9f; code++)
		if (code < 0x20 || code >= 0x7f)
			expect(
				link("x", `https://example.com/${String.fromCharCode(code)}`, true),
			).toBe("x");
	expect(link("x", "https://example.com/😀", true)).toContain("😀");
});
test("terminal support full Go environment table", () => {
	for (const program of [
		"iTerm.app",
		"WezTerm",
		"mintty",
		"ghostty",
		"vscode",
		"Hyper",
		"rio",
		"Tabby",
		"WarpTerminal",
	])
		expect(supportsHyperlinks({ TERM_PROGRAM: program })).toBe(true);
	for (const term of [
		"xterm-kitty",
		"foot",
		"foot-direct",
		"foot-extra",
		"foot-extra-direct",
		"alacritty",
		"alacritty-direct",
	])
		expect(supportsHyperlinks({ TERM: term })).toBe(true);
	for (const key of ["KITTY_WINDOW_ID", "WT_SESSION", "DOMTERM"])
		expect(supportsHyperlinks({ [key]: "1" })).toBe(true);
	for (const key of ["TMUX", "STY", "ZELLIJ"])
		expect(supportsHyperlinks({ TERM_PROGRAM: "iTerm.app", [key]: "0" })).toBe(
			false,
		);
	for (const env of [
		{},
		{ TERM: "xterm-256color" },
		{ TERM_PROGRAM: "Apple_Terminal" },
		{ TERM_PROGRAM: "wezterm" },
		{ VTE_VERSION: "4600" },
		{ KONSOLE_VERSION: "180800" },
		{ VTE_VERSION: "5000x" },
		{ VTE_VERSION: "5e3" },
		{ VTE_VERSION: "5000.0" },
		{ VTE_VERSION: "0x1388" },
	])
		expect(supportsHyperlinks(env)).toBe(false);
	for (const env of [
		{ VTE_VERSION: "5000" },
		{ VTE_VERSION: "5002" },
		{ VTE_VERSION: " +5000 " },
		{ KONSOLE_VERSION: "200400" },
	])
		expect(supportsHyperlinks(env)).toBe(true);
});

test("terminal version parsing matches signed 64-bit Go integers", () => {
	for (const raw of [
		"9007199254740992",
		"9223372036854775807",
		"\u00855000\u0085",
	])
		expect(supportsHyperlinks({ VTE_VERSION: raw })).toBe(true);
	for (const raw of [
		"9223372036854775808",
		"-9223372036854775809",
		"\ufeff5000",
	])
		expect(supportsHyperlinks({ VTE_VERSION: raw })).toBe(false);
});

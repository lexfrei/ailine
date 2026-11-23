import { completionShells, visibleFlags } from "./args";

type Candidate = { name: string; description: string };
type Context = "root" | "validate" | "help" | "completion" | "shell";
const contexts: readonly Context[] = [
	"root",
	"validate",
	"help",
	"completion",
	"shell",
];
const commands: readonly Candidate[] = [
	{ name: "validate", description: "Validate config file" },
	{ name: "help", description: "Help about any command" },
	{ name: "completion", description: "Generate shell completion scripts" },
];

function candidates(context: Context): Candidate[] {
	const flags = visibleFlags(
		context === "root" ? "root" : context === "shell" ? "shell" : "command",
	);
	const result = flags.map((flag) => ({
		name: `--${flag.name}`,
		description: flag.description,
	}));
	if (context === "root" || context === "help") result.push(...commands);
	if (context === "completion") {
		for (const shell of completionShells)
			result.push({ name: shell, description: `Generate ${shell} completion` });
	}
	return result;
}

function bashScript(): string {
	const cases = contexts
		.map(
			(context) =>
				`    ${context}) candidates='${candidates(context)
					.map((candidate) => candidate.name)
					.join(" ")}' ;;`,
		)
		.join("\n");
	return `# bash completion for ailine
_ailine() {
  local context=root skip=false word i candidates current
  current="\${COMP_WORDS[COMP_CWORD]}"
  for ((i=1; i<COMP_CWORD; i++)); do
    word="\${COMP_WORDS[i]}"
    if [[ "$skip" == true ]]; then skip=false; continue; fi
    case "$word" in
      --) COMPREPLY=(); return ;;
      --config|--cost|--theme|--hyperlinks) skip=true; continue ;;
      -*) continue ;;
    esac
    case "$context:$word" in
      root:validate|help:validate) context=validate ;;
      root:help) context=help ;;
      root:completion|help:completion) context=completion ;;
      completion:bash|completion:zsh|completion:fish|completion:powershell) context=shell ;;
    esac
  done
  case "\${COMP_WORDS[COMP_CWORD-1]}" in
    --config) COMPREPLY=(); compopt -o default 2>/dev/null || COMPREPLY=($(compgen -f -- "$current")); return ;;
    --cost|--hyperlinks) COMPREPLY=($(compgen -W 'auto true false' -- "$current")); return ;;
    --theme) COMPREPLY=($(compgen -W 'emoji text' -- "$current")); return ;;
  esac
  case "$context" in
${cases}
  esac
  COMPREPLY=($(compgen -W "$candidates" -- "$current"))
}
complete -F _ailine ailine
`;
}

function zshScript(noDescriptions: boolean): string {
	const cases = contexts
		.map((context) => {
			const words = candidates(context)
				.map(
					(candidate) =>
						`'${candidate.name}${noDescriptions ? "" : `:${candidate.description.replaceAll(":", "\\:")}`}'`,
				)
				.join(" ");
			return `    ${context}) candidates=(${words}) ;;`;
		})
		.join("\n");
	return `#compdef ailine
_ailine() {
  local context=root skip=false word i
  local -a candidates
  for ((i=2; i<CURRENT; i++)); do
    word="$words[i]"
    if [[ "$skip" == true ]]; then skip=false; continue; fi
    case "$word" in
      --) return ;;
      --config|--cost|--theme|--hyperlinks) skip=true; continue ;;
      -*) continue ;;
    esac
    case "$context:$word" in
      root:validate|help:validate) context=validate ;;
      root:help) context=help ;;
      root:completion|help:completion) context=completion ;;
      completion:bash|completion:zsh|completion:fish|completion:powershell) context=shell ;;
    esac
  done
  case "$words[CURRENT-1]" in
    --config) _files; return ;;
    --cost|--hyperlinks) compadd auto true false; return ;;
    --theme) compadd emoji text; return ;;
  esac
  case "$context" in
${cases}
  esac
  ${noDescriptions ? 'compadd -- "$candidates[@]"' : '_describe "ailine" candidates'}
}
compdef _ailine ailine
`;
}

function fishScript(noDescriptions: boolean): string {
	const lines = [
		"# fish completion for ailine",
		"function __ailine_context",
		"  set -l context root",
		"  set -l skip false",
		"  set -l tokens (commandline -opc)",
		"  for word in $tokens[2..-1]",
		"    if test $skip = true; set skip false; continue; end",
		"    switch $word",
		"      case --; return",
		"      case --config --cost --theme --hyperlinks; set skip true; continue",
		"      case '-*'; continue",
		"    end",
		"    switch $context:$word",
		"      case root:validate help:validate; set context validate",
		"      case root:help; set context help",
		"      case root:completion help:completion; set context completion",
		"      case completion:bash completion:zsh completion:fish completion:powershell; set context shell",
		"    end",
		"  end",
		"  test $context = $argv[1]",
		"end",
	];
	for (const context of contexts) {
		for (const candidate of candidates(context)) {
			const flag = candidate.name.startsWith("--");
			let line = `complete -c ailine -n '__ailine_context ${context}' ${flag ? `-l ${candidate.name.slice(2)}` : `-a '${candidate.name}'`} -f`;
			if (!noDescriptions) line += ` -d '${candidate.description}'`;
			if (candidate.name === "--config") line += " -r -F";
			if (["--cost", "--hyperlinks"].includes(candidate.name))
				line += " -r -a 'auto true false'";
			if (candidate.name === "--theme") line += " -r -a 'emoji text'";
			lines.push(line);
		}
	}
	return `${lines.join("\n")}\n`;
}

function powershellScript(noDescriptions: boolean): string {
	const cases = contexts
		.map((context) => {
			const entries = candidates(context)
				.map(
					(candidate) =>
						`@('${candidate.name}', '${noDescriptions ? candidate.name : candidate.description}')`,
				)
				.join(", ");
			return `    '${context}' { $candidates = @(${entries}) }`;
		})
		.join("\n");
	return `# PowerShell completion for ailine
Register-ArgumentCompleter -Native -CommandName ailine -ScriptBlock {
  param($wordToComplete, $commandAst, $cursorPosition)
  $context = 'root'
  $skip = $false
  $previous = ''
  foreach ($element in $commandAst.CommandElements | Select-Object -Skip 1) {
    if ($element.Extent.EndOffset -ge $cursorPosition -and $wordToComplete) { break }
    $word = $element.Extent.Text
    $previous = $word
    if ($skip) { $skip = $false; continue }
    if ($word -eq '--') { return }
    if ($word -in @('--config', '--cost', '--theme', '--hyperlinks')) { $skip = $true; continue }
    if ($word.StartsWith('-')) { continue }
    switch ("$context/$word") {
      { $_ -in @('root/validate', 'help/validate') } { $context = 'validate' }
      'root/help' { $context = 'help' }
      { $_ -in @('root/completion', 'help/completion') } { $context = 'completion' }
      { $_ -in @('completion/bash', 'completion/zsh', 'completion/fish', 'completion/powershell') } { $context = 'shell' }
    }
  }
  $candidates = @()
  switch ($context) {
${cases}
  }
  if ($skip) {
    switch ($previous) {
      '--config' { return }
      '--theme' { $candidates = @(@('emoji','emoji'), @('text','text')) }
      default { $candidates = @(@('auto','auto'), @('true','true'), @('false','false')) }
    }
  }
  foreach ($candidate in $candidates) {
    if ($candidate[0].StartsWith($wordToComplete, [System.StringComparison]::OrdinalIgnoreCase)) {
      [System.Management.Automation.CompletionResult]::new($candidate[0], $candidate[0], 'ParameterValue', $candidate[1])
    }
  }
}
`;
}

export function completionScript(
	shell: string,
	noDescriptions = false,
): string {
	switch (shell) {
		case "bash":
			return bashScript();
		case "zsh":
			return zshScript(noDescriptions);
		case "fish":
			return fishScript(noDescriptions);
		case "powershell":
			return powershellScript(noDescriptions);
		default:
			throw new Error("unsupported completion shell");
	}
}

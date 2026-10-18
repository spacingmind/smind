package gate

import "regexp"

type GuardFamily string

const (
	FamilyExfiltration GuardFamily = "exfiltration"
	FamilyInjection    GuardFamily = "injection"
	FamilyDestructive  GuardFamily = "destructive"
	FamilyPersistence  GuardFamily = "persistence"
	FamilyNetwork      GuardFamily = "network"
	FamilyObfuscation  GuardFamily = "obfuscation"
)

type guardRule struct {
	family GuardFamily
	name   string
	re     *regexp.Regexp
}

var guardRules = []guardRule{
	{FamilyExfiltration, "pipe_to_interpreter", regexp.MustCompile(`(?i)\|\s*(?:sudo(?:\s+-\w+)*\s+|env\s+|/\S*/)?(?:sh|bash|zsh|dash|ksh|fish|python3?|perl|ruby|node)\b`)},
	{FamilyExfiltration, "sh_process_subst", regexp.MustCompile(`(?i)\b(?:ba)?sh\s+<\(\s*(?:curl|wget)`)},
	{FamilyExfiltration, "cmd_subst_fetch", regexp.MustCompile(`(?i)\$\(\s*(?:curl|wget)`)},
	{FamilyExfiltration, "download_then_sh", regexp.MustCompile(`(?i)(?:curl|wget)\b[^\n]*(?:-o|-O|--output)\b[^\n]*;\s*(?:ba)?sh\b`)},
	{FamilyExfiltration, "cat_secrets", regexp.MustCompile(`(?i)\b(?:cat|less|more|head|tail|bat|xxd|strings|cp|scp)\s+[^\n]*(?:\.env\b|id_rsa|id_ed25519|\.aws/|\.ssh/|\.npmrc|\.netrc|\.gnupg|\.kube/config)`)},
	{FamilyExfiltration, "send_secrets", regexp.MustCompile(`(?i)\b(send|post|upload|exfiltrate|leak|email)\b[^\n]{0,60}\b(secrets?|credentials?|tokens?|api[ _-]?keys?|passwords?|\.env)\b`)},

	{FamilyInjection, "ignore_previous_instructions", regexp.MustCompile(`(?i)(?:ignore|disregard|forget|override)\s+(?:all\s+|any\s+|the\s+)?(?:of\s+)?(?:the\s+)?(?:previous|prior|above|earlier|preceding|existing)\s+(?:instructions?|rules?|prompts?|guidelines?|directions?|messages?)`)},
	{FamilyInjection, "disregard_system", regexp.MustCompile(`(?i)disregard\s+(the\s+|all\s+)?(system|previous|prior|above)`)},
	{FamilyInjection, "you_are_now", regexp.MustCompile(`(?i)\byou\s+are\s+now\b`)},
	{FamilyInjection, "reveal_prompt", regexp.MustCompile(`(?i)\b(reveal|print|show|output|repeat)\s+(your\s+|the\s+)?(system\s+)?(prompt|instructions)\b`)},
	{FamilyInjection, "do_not_tell_user", regexp.MustCompile(`(?i)\b(do\s+not|don['’]?t|never)\s+(tell|inform|mention|reveal)\b[^\n]{0,30}\buser\b`)},
	{FamilyInjection, "new_instructions_colon", regexp.MustCompile(`(?i)\bnew\s+instructions?\s*:`)},
	{FamilyInjection, "no_ask_approval", regexp.MustCompile(`(?i)\b(?:never|don['’]?t|do\s+not|stop|without)\s+(?:ask(?:ing)?|request(?:ing)?|wait(?:ing)?)\b[^\n]{0,40}\b(?:approval|permission|confirmation|consent|the\s+user)`)},
	{FamilyInjection, "skip_approval", regexp.MustCompile(`(?i)\bskip(?:ping)?\s+(?:the\s+)?(?:confirmation|approval|permission|review|hooks?|checks?)`)},
	{FamilyInjection, "no_verify", regexp.MustCompile(`(?i)--no-verify`)},
	{FamilyInjection, "dangerously_skip_permissions", regexp.MustCompile(`(?i)--dangerously-skip-permissions`)},
	{FamilyInjection, "yolo", regexp.MustCompile(`(?i)--yolo\b`)},
	{FamilyInjection, "bypass_controls", regexp.MustCompile(`(?i)bypass(?:ing)?\s+(?:permissions?|approval|sandbox|hooks?)`)},
	{FamilyInjection, "disable_safety", regexp.MustCompile(`(?i)\bdisable\s+(?:the\s+)?(?:sandbox|hooks?|approval|permission|safety|guard)`)},
	{FamilyInjection, "trust_tool_output", regexp.MustCompile(`(?i)treat\s+(?:tool|file|web|command)\s+(?:output|content|results?)\s+as\s+(?:trusted|authoritative|instructions?)`)},
	{FamilyInjection, "always_force_flag", regexp.MustCompile(`(?i)always\s+(?:run|use|pass)\b[^\n]*--(?:force|yes|no-verify|dangerously)`)},

	{FamilyDestructive, "rm_recursive_root", regexp.MustCompile(`(?i)\brm\s+-[a-z]*[rf][a-z]*\s+(-[a-z]+\s+)*(/|~|\$HOME|\*|\.\s|\.\.)`)},
	{FamilyDestructive, "rm_long_flags", regexp.MustCompile(`(?i)\brm\s+(?:-[a-z]*\s+)*--(?:recursive|force)`)},
	{FamilyDestructive, "rm_home_or_cwd", regexp.MustCompile(`(?i)\brm\s+-[a-z]*[rf][a-z]*\s+(?:-[a-z]+\s+)*(?:\./?(?:\s|$)|"?\$\{?HOME\}?"?|\.\./)`)},
	{FamilyDestructive, "xargs_rm", regexp.MustCompile(`(?i)\bxargs\s+(?:-\S+\s+)*rm\b`)},
	{FamilyDestructive, "git_push_force", regexp.MustCompile(`(?i)\bgit\s+push\b[^\n]*(--force\b|\s-f\b)`)},
	{FamilyDestructive, "git_push_plus_refspec", regexp.MustCompile(`(?i)\bgit\s+push\b[^\n]*\s\+\S`)},
	{FamilyDestructive, "git_push_f_cluster", regexp.MustCompile(`(?i)\bgit\s+push\b[^\n]*\s-[a-z]*f[a-z]*\b`)},
	{FamilyDestructive, "git_reset_hard", regexp.MustCompile(`(?i)\bgit\s+(reset\s+--hard|clean\s+-[a-z]*f)`)},
	{FamilyDestructive, "find_delete", regexp.MustCompile(`(?i)\bfind\s+[^\n]*-delete\b`)},
	{FamilyDestructive, "shred", regexp.MustCompile(`(?i)\bshred\b`)},
	{FamilyDestructive, "drop_table", regexp.MustCompile(`(?i)\bdrop\s+(table|database|schema)\b`)},
	{FamilyDestructive, "mkfs", regexp.MustCompile(`(?i)\bmkfs\b`)},
	{FamilyDestructive, "dd_if", regexp.MustCompile(`(?i)\bdd\s+if=`)},
	{FamilyDestructive, "chmod_777", regexp.MustCompile(`(?i)\bchmod\s+-R\s+0?777\b`)},
	{FamilyDestructive, "fork_bomb", regexp.MustCompile(`:\(\)\s*\{`)},
	{FamilyDestructive, "truncate_zero", regexp.MustCompile(`(?i)\btruncate\s+-s\s*0\b`)},
	{FamilyDestructive, "overwrite_disk", regexp.MustCompile(`>\s*/dev/sd[a-z]`)},

	{FamilyPersistence, "crontab", regexp.MustCompile(`(?i)\bcrontab\b`)},
	{FamilyPersistence, "etc_cron", regexp.MustCompile(`/etc/cron`)},
	{FamilyPersistence, "shell_rc", regexp.MustCompile(`(?i)(~|\$HOME)?/?\.(bashrc|zshrc|profile|bash_profile|zprofile)\b`)},
	{FamilyPersistence, "shell_rc_more", regexp.MustCompile(`(?i)\.(?:zshenv|zlogin|zlogout|bash_login|bash_logout|inputrc)\b`)},
	{FamilyPersistence, "fish_config", regexp.MustCompile(`(?i)config\.fish`)},
	{FamilyPersistence, "etc_environment", regexp.MustCompile(`/etc/environment`)},
	{FamilyPersistence, "git_hooks_dir", regexp.MustCompile(`(?i)\.git/hooks`)},
	{FamilyPersistence, "git_hooks_path", regexp.MustCompile(`(?i)\bgit\s+config\s+[^\n]*core\.hooksPath`)},
	{FamilyPersistence, "at_now", regexp.MustCompile(`(?i)\bat\s+now\b`)},
	{FamilyPersistence, "authorized_keys", regexp.MustCompile(`(?i)authorized_keys`)},
	{FamilyPersistence, "launchctl", regexp.MustCompile(`(?i)\blaunchctl\b`)},
	{FamilyPersistence, "systemctl_enable", regexp.MustCompile(`(?i)\bsystemctl\s+(enable|--user)`)},
	{FamilyPersistence, "launch_agents", regexp.MustCompile(`(?i)\bLaunchAgents\b`)},
	{FamilyPersistence, "rc_local", regexp.MustCompile(`/etc/(rc\.local|profile\.d)`)},

	{FamilyNetwork, "netcat", regexp.MustCompile(`(?i)\b(?:nc|ncat|netcat)\s+(?:-\w+|\S+\s+\d{1,5}\b)`)},
	{FamilyNetwork, "socat", regexp.MustCompile(`(?i)\bsocat\s+(?:-|\S+:)`)},
	{FamilyNetwork, "dev_tcp", regexp.MustCompile(`/dev/(tcp|udp)/`)},
	{FamilyNetwork, "ssh_reverse", regexp.MustCompile(`(?i)\bssh\s+[^\n]*-R\b`)},
	{FamilyNetwork, "ngrok", regexp.MustCompile(`(?i)\bngrok\b`)},
	{FamilyNetwork, "bash_interactive", regexp.MustCompile(`(?i)\bbash\s+-i\b`)},
	{FamilyNetwork, "script_socket", regexp.MustCompile(`(?i)\b(python|perl|ruby|php)\s+-[a-z]*c\b[^\n]*socket`)},
	{FamilyNetwork, "pip_install", regexp.MustCompile(`(?i)\b(?:pip3?|pipx)\s+install\b`)},
	{FamilyNetwork, "pkg_install_global", regexp.MustCompile(`(?i)\b(?:npm|yarn|pnpm)\s+(?:install|i|add)\b[^\n]*\s(?:-g|--global)\b`)},
	{FamilyNetwork, "yarn_global_add", regexp.MustCompile(`(?i)\byarn\s+global\s+add\b`)},
	{FamilyNetwork, "npx_run", regexp.MustCompile(`(?i)\bnpx\s+\S`)},

	{FamilyObfuscation, "base64_decode", regexp.MustCompile(`(?i)\bbase64\s+(-d|--decode|-D)\b`)},
	{FamilyObfuscation, "eval", regexp.MustCompile(`(?i)\beval\s*[\($"']`)},
	{FamilyObfuscation, "hex_blob", regexp.MustCompile(`(?i)(\\x[0-9a-f]{2}){5,}`)},
	{FamilyObfuscation, "zero_width", regexp.MustCompile(`[\x{200B}-\x{200F}\x{202A}-\x{202E}\x{2060}\x{FEFF}]`)},
	{FamilyObfuscation, "echo_blob_pipe", regexp.MustCompile(`\becho\s+[A-Za-z0-9+/=]{40,}\s*\|`)},
	{FamilyObfuscation, "xxd_reverse", regexp.MustCompile(`(?i)\bxxd\s+-r\b`)},
	{FamilyObfuscation, "powershell_enc", regexp.MustCompile(`(?i)\bpowershell\b[^\n]*-enc`)},
}

// GuardScan scans the texts against the six guard families and reports each
// matching (family, pattern) once as a ReasonGuard finding. Each text is
// scanned raw and in its canonical form.
func GuardScan(texts ...string) []Finding {
	seen := make(map[GuardFamily]map[string]struct{})
	var out []Finding
	for _, t := range scanTexts(texts) {
		for _, r := range guardRules {
			if !r.re.MatchString(t) {
				continue
			}
			if seen[r.family] == nil {
				seen[r.family] = make(map[string]struct{})
			}
			if _, ok := seen[r.family][r.name]; ok {
				continue
			}
			seen[r.family][r.name] = struct{}{}
			out = append(out, finding(ReasonGuard, "%s: %s", r.family, r.name))
		}
	}
	return out
}

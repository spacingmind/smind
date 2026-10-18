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
	{FamilyExfiltration, "curl_pipe_shell", regexp.MustCompile(`(?i)\b(curl|wget)\b[^\n]*\|\s*(sudo\s+)?(sh|bash|zsh)\b`)},
	{FamilyExfiltration, "cat_secrets", regexp.MustCompile(`(?i)\bcat\s+[^\n]*(\.env\b|id_rsa|id_ed25519|\.aws/credentials|\.ssh/|\.npmrc|\.netrc)`)},
	{FamilyExfiltration, "send_secrets", regexp.MustCompile(`(?i)\b(send|post|upload|exfiltrate|leak|email)\b[^\n]{0,60}\b(secrets?|credentials?|tokens?|api[ _-]?keys?|passwords?|\.env)\b`)},

	{FamilyInjection, "ignore_previous_instructions", regexp.MustCompile(`(?i)ignore\s+(all\s+|any\s+|the\s+)?(previous|prior|above|earlier)\s+(instructions?|rules?|prompts?)`)},
	{FamilyInjection, "disregard_system", regexp.MustCompile(`(?i)disregard\s+(the\s+|all\s+)?(system|previous|prior|above)`)},
	{FamilyInjection, "you_are_now", regexp.MustCompile(`(?i)\byou\s+are\s+now\b`)},
	{FamilyInjection, "reveal_prompt", regexp.MustCompile(`(?i)\b(reveal|print|show|output|repeat)\s+(your\s+|the\s+)?(system\s+)?(prompt|instructions)\b`)},
	{FamilyInjection, "do_not_tell_user", regexp.MustCompile(`(?i)\b(do\s+not|don'?t|never)\s+(tell|inform|mention|reveal)\b[^\n]{0,30}\buser\b`)},
	{FamilyInjection, "new_instructions_colon", regexp.MustCompile(`(?i)\bnew\s+instructions?\s*:`)},

	{FamilyDestructive, "rm_recursive_root", regexp.MustCompile(`(?i)\brm\s+-[a-z]*[rf][a-z]*\s+(-[a-z]+\s+)*(/|~|\$HOME|\*|\.\s|\.\.)`)},
	{FamilyDestructive, "git_push_force", regexp.MustCompile(`(?i)\bgit\s+push\b[^\n]*(--force\b|\s-f\b)`)},
	{FamilyDestructive, "git_reset_hard", regexp.MustCompile(`(?i)\bgit\s+(reset\s+--hard|clean\s+-[a-z]*f)`)},
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
	{FamilyPersistence, "authorized_keys", regexp.MustCompile(`(?i)authorized_keys`)},
	{FamilyPersistence, "launchctl", regexp.MustCompile(`(?i)\blaunchctl\b`)},
	{FamilyPersistence, "systemctl_enable", regexp.MustCompile(`(?i)\bsystemctl\s+(enable|--user)`)},
	{FamilyPersistence, "launch_agents", regexp.MustCompile(`(?i)\bLaunchAgents\b`)},
	{FamilyPersistence, "rc_local", regexp.MustCompile(`/etc/(rc\.local|profile\.d)`)},

	{FamilyNetwork, "netcat", regexp.MustCompile(`(?i)\b(nc|ncat|netcat|socat)\b`)},
	{FamilyNetwork, "dev_tcp", regexp.MustCompile(`/dev/(tcp|udp)/`)},
	{FamilyNetwork, "ssh_reverse", regexp.MustCompile(`(?i)\bssh\s+[^\n]*-R\b`)},
	{FamilyNetwork, "ngrok", regexp.MustCompile(`(?i)\bngrok\b`)},
	{FamilyNetwork, "bash_interactive", regexp.MustCompile(`(?i)\bbash\s+-i\b`)},
	{FamilyNetwork, "script_socket", regexp.MustCompile(`(?i)\b(python|perl|ruby|php)\s+-[a-z]*c\b[^\n]*socket`)},

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

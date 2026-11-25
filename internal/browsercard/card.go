// Package browsercard builds the human-facing text and options of the permission card the daemon raises when an agent calls a browser tool.
package browsercard

import (
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"
)

type Class string

const (
	ClassRead Class = "read" // tabs, snapshot, screenshot, console, wait
	ClassOpen Class = "open" // browser_open (also covers browser_close)
	ClassAct  Class = "act"  // any other acting tool: asked every call, never grantable
)

type Input struct {
	Class      Class
	Tool       string   // e.g. "browser_open"
	TaskTitle  string   // free text
	Workspace  string   // free text
	Allowlist  []string // hosts the person added for this workspace
	URL        string   // raw target of an open (may carry a query or userinfo); empty for other tools
	HostStatus string   // "localhost" | "added" | "" (the policy's verdict on the URL's host)
	Uses       int      // browser calls this run has already made
}

type Option struct {
	ID   string // "allow_once" | "allow_run" | "deny"
	Name string
	Kind string // "allow_once" | "reject_once"
}

type Info struct {
	Tool, Class, URL, Host, HostASCII, HostStatus, TaskTitle, Workspace string
	Allowlist                                                           []string // the first 20 hosts, whole
	AllowlistTotal                                                      int      // how many hosts the workspace allows
	Uses                                                                int
	QueryLen                                                            int  // runes of the raw query, 0 if none
	HasFragment                                                         bool // the raw URL carried a fragment
	PathCut                                                             bool // the path was cut by the display cap
}

type Card struct {
	Summary string
	Options []Option
	Info    Info
}

func Build(in Input) Card {
	tool := cleanText(in.Tool, 64)
	taskTitle := quoted(in.TaskTitle, 60)
	workspace := quoted(in.Workspace, 60)
	// EVERY host is cleaned (a valid host is up to 253 characters, never cut) so the
	// card's text counts the true total; Info.Allowlist carries the first 20.
	allowlist := make([]string, 0, len(in.Allowlist))
	for _, h := range in.Allowlist {
		if c := cleanText(h, 253); c != "" {
			allowlist = append(allowlist, c)
		}
	}
	facts := urlFactsOf(in.URL)
	return Card{
		Summary: summary(in, tool, taskTitle, workspace, allowlist, facts),
		Options: optionsFor(in.Class, tool),
		Info:    info(in, tool, taskTitle, workspace, allowlist, facts),
	}
}

// closeGrantName is the run grant a CLOSE card offers. The grant is the run's open grant (it covers
// opening and closing), so the label says exactly that: a close card that said "opening any page" read
// as the wrong question, and one that said only "closing" would hide what is granted.
const closeGrantName = "Allow closing tabs, and opening any page the browser policy allows (localhost and your added hosts), for this run"

func optionsFor(class Class, tool string) []Option {
	allowOnce := Option{ID: "allow_once", Name: "Allow once", Kind: "allow_once"}
	deny := Option{ID: "deny", Name: "Deny", Kind: "reject_once"}
	switch class {
	case ClassRead:
		return []Option{
			allowOnce,
			{ID: "allow_run", Name: "Allow reads for this run", Kind: "allow_once"},
			deny,
		}
	case ClassOpen:
		if tool == "browser_close" {
			return []Option{allowOnce, {ID: "allow_run", Name: closeGrantName, Kind: "allow_once"}, deny}
		}
		return []Option{
			allowOnce,
			{ID: "allow_run", Name: "Allow opening any page the browser policy allows (localhost and your added hosts) and closing tabs, for this run", Kind: "allow_once"},
			deny,
		}
	default:
		return []Option{allowOnce, deny}
	}
}

// daemonCheck ends every summary: it is appended AFTER the variable text is
// capped, so no input can push it off the line.
const daemonCheck = " (daemon check)"

// summaryBodyMax is the cap of the variable part (the whole summary stays under
// 500 runes with the marker).
const summaryBodyMax = 480

func summary(in Input, tool, taskTitle, workspace string, allowlist []string, facts urlFacts) string {
	var body string
	switch in.Class {
	case ClassRead:
		body = fmt.Sprintf(
			`Let this run read browser pages? "%s" wants to use the browser in "%s" (read-only: tabs, snapshot, screenshot, console, wait). It can reach this computer's localhost and: %s. Page text is untrusted.`,
			taskTitle, workspace, hostsText(allowlist))
	case ClassOpen:
		if in.Tool == "browser_close" {
			body = fmt.Sprintf(`Close a browser tab? "%s" in "%s".`, taskTitle, workspace)
			break
		}
		body = fmt.Sprintf(
			`Open %s%s in the browser? (%s) "%s" in "%s" has used the browser %d times.`,
			redactURL(in.URL), markersText(facts), hostStatusText(in.HostStatus), taskTitle, workspace, in.Uses)
	default:
		target := "the current page"
		if in.URL != "" {
			target = redactURL(in.URL) + markersText(facts)
		}
		body = fmt.Sprintf(
			`Allow %s on %s? "%s" in "%s". Asked every time.`,
			tool, target, taskTitle, workspace)
	}
	return cleanText(body, summaryBodyMax) + daemonCheck
}

// urlFacts is what the card tells about the parts of the URL it never shows.
type urlFacts struct {
	queryLen    int
	hasFragment bool
	pathCut     bool
}

// urlFactsOf measures the raw URL: content stays hidden, only its presence
// (and the query's length) is signalled. A parse failure leaves all zero.
func urlFactsOf(raw string) urlFacts {
	u, err := url.Parse(raw)
	if err != nil {
		return urlFacts{}
	}
	return urlFacts{
		queryLen:    utf8.RuneCountInString(u.RawQuery),
		hasFragment: u.Fragment != "",
		pathCut:     utf8.RuneCountInString(cleanText(u.Path, len(u.Path))) > 120,
	}
}

// markersText sits right after the redacted URL so a query, a fragment or a
// cut path is never silently dropped from what the person approves.
func markersText(f urlFacts) string {
	var b strings.Builder
	if f.queryLen > 0 {
		fmt.Fprintf(&b, " (+query, %d characters)", f.queryLen)
	}
	if f.hasFragment {
		b.WriteString(" (+fragment)")
	}
	if f.pathCut {
		b.WriteString(" (path cut)")
	}
	return b.String()
}

func hostsText(hosts []string) string {
	if len(hosts) == 0 {
		return "no other hosts"
	}
	// Whole hosts only: a host cut mid-way could read as another host. The
	// first host always shows, even alone over the limit.
	text := hosts[0]
	shown := 1
	for _, h := range hosts[1:] {
		if utf8.RuneCountInString(text)+2+utf8.RuneCountInString(h) > 150 {
			break
		}
		text += ", " + h
		shown++
	}
	if hidden := len(hosts) - shown; hidden > 0 {
		return fmt.Sprintf("%s +%d more", text, hidden)
	}
	return text
}

func hostStatusText(status string) string {
	switch status {
	case "localhost":
		return "localhost"
	case "added":
		return "a host you added"
	default:
		return "host not on your list"
	}
}

func info(in Input, tool, taskTitle, workspace string, allowlist []string, facts urlFacts) Info {
	i := Info{
		Tool:           tool,
		Class:          string(in.Class),
		HostStatus:     in.HostStatus,
		TaskTitle:      taskTitle,
		Workspace:      workspace,
		Allowlist:      firstHosts(allowlist, 20),
		AllowlistTotal: len(allowlist),
		Uses:           in.Uses,
	}
	if in.Tool == "browser_close" {
		return i
	}
	i.QueryLen = facts.queryLen
	i.HasFragment = facts.hasFragment
	i.PathCut = facts.pathCut
	if in.URL != "" {
		i.URL = redactURL(in.URL)
		if u, err := url.Parse(in.URL); err == nil && u.Host != "" && (u.Scheme == "http" || u.Scheme == "https") {
			i.Host = cleanText(u.Host, 120)
			i.HostASCII = asciiHostOf(i.Host)
		}
	}
	return i
}

// asciiHostOf is the canonical form the policy matches on, empty when the
// host is already ASCII (or has no valid punycode form).
func asciiHostOf(host string) string {
	_, ascii := hostDisplay(host)
	return ascii
}

// firstHosts returns up to n whole hosts.
func firstHosts(hosts []string, n int) []string {
	if len(hosts) <= n {
		return hosts
	}
	return hosts[:n]
}

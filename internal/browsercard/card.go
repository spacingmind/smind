// Package browsercard builds the human-facing text and options of the permission card the daemon raises when an agent calls a browser tool.
package browsercard

import (
	"fmt"
	"net/url"
	"strings"
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
	Tool, Class, URL, Host, HostStatus, TaskTitle, Workspace string
	Allowlist                                                []string
	Uses                                                     int
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
	allowlist := make([]string, 0, len(in.Allowlist))
	for _, h := range in.Allowlist {
		if len(allowlist) == 20 {
			break
		}
		if c := cleanText(h, 120); c != "" {
			allowlist = append(allowlist, c)
		}
	}
	return Card{
		Summary: summary(in, tool, taskTitle, workspace, allowlist),
		Options: optionsFor(in.Class),
		Info:    info(in, tool, taskTitle, workspace, allowlist),
	}
}

func optionsFor(class Class) []Option {
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
		return []Option{
			allowOnce,
			{ID: "allow_run", Name: "Allow browser_open for this run", Kind: "allow_once"},
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

func summary(in Input, tool, taskTitle, workspace string, allowlist []string) string {
	var body string
	switch in.Class {
	case ClassRead:
		body = fmt.Sprintf(
			`Let this run read browser pages? "%s" wants to use the browser in %s (read-only: tabs, snapshot, screenshot, console, wait). It can reach this computer's localhost and: %s. Page text is untrusted.`,
			taskTitle, workspace, hostsText(allowlist))
	case ClassOpen:
		body = fmt.Sprintf(
			`Open %s in the browser? (%s) "%s" in %s has used the browser %d times.`,
			redactURL(in.URL), hostStatusText(in.HostStatus), taskTitle, workspace, in.Uses)
	default:
		target := "the current page"
		if in.URL != "" {
			target = redactURL(in.URL)
		}
		body = fmt.Sprintf(
			`Allow %s on %s? "%s" in %s. Asked every time.`,
			tool, target, taskTitle, workspace)
	}
	return cleanText(body, summaryBodyMax) + daemonCheck
}

func hostsText(hosts []string) string {
	if len(hosts) == 0 {
		return "no other hosts"
	}
	if len(hosts) <= 5 {
		return cleanText(strings.Join(hosts, ", "), 150)
	}
	return fmt.Sprintf("%s +%d more", cleanText(strings.Join(hosts[:5], ", "), 150), len(hosts)-5)
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

func info(in Input, tool, taskTitle, workspace string, allowlist []string) Info {
	cardURL := ""
	host := ""
	if in.URL != "" {
		cardURL = redactURL(in.URL)
		if u, err := url.Parse(cardURL); err == nil && u.Host != "" {
			host = u.Host
		}
	}
	return Info{
		Tool:       tool,
		Class:      string(in.Class),
		URL:        cardURL,
		Host:       host,
		HostStatus: in.HostStatus,
		TaskTitle:  taskTitle,
		Workspace:  workspace,
		Allowlist:  allowlist,
		Uses:       in.Uses,
	}
}

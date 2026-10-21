package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// `smind learn probe` is the CLI side of the learning.probe.* RPCs
// (ADR-0037): dry-run estimate, start, show.

// learnProbeEstimate mirrors the learning.probe.estimate result.
type learnProbeEstimate struct {
	Eligible        bool               `json:"eligible"`
	Reasons         []learnProbeReason `json:"reasons"`
	Quick           bool               `json:"quick"`
	Arms            int                `json:"arms"`
	EstUSD          float64            `json:"estUSD"`
	EstArmUSD       float64            `json:"estArmUSD"`
	ExpectedMinutes int                `json:"expectedMinutes"`
	PerProbeCapUSD  float64            `json:"perProbeCapUSD"`
	DailyCapUSD     float64            `json:"dailyCapUSD"`
	SpentTodayUSD   float64            `json:"spentTodayUSD"`
	QuotaPercent    *float64           `json:"quotaPercent"`
}

type learnProbeReason struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// learnProbeView mirrors the learning.probe.get result (ProbeView).
type learnProbeView struct {
	ID            int64               `json:"id"`
	ProposalID    int64               `json:"proposalId"`
	WorkspaceID   int64               `json:"workspaceId"`
	Quick         bool                `json:"quick"`
	Status        string              `json:"status"`
	Verdict       *string             `json:"verdict"`
	VerdictReason string              `json:"verdictReason"`
	EstUSD        float64             `json:"estUSD"`
	CostUSD       float64             `json:"costUSD"`
	StartedAt     string              `json:"startedAt"`
	FinishedAt    string              `json:"finishedAt,omitempty"`
	ElapsedMs     int64               `json:"elapsedMs"`
	Arms          []learnProbeArm     `json:"arms"`
	Contrast      *learnProbeContrast `json:"contrast,omitempty"`
}

type learnProbeArm struct {
	Role            string  `json:"role"`
	Status          string  `json:"status"`
	RunID           string  `json:"runId,omitempty"`
	ElapsedMs       int64   `json:"elapsedMs"`
	CostUSD         float64 `json:"costUSD"`
	LoadedSkill     *bool   `json:"loadedSkill,omitempty"`
	FailureRecurred *bool   `json:"failureRecurred"`
	ProducedDiff    bool    `json:"producedDiff"`
	DiffBytes       int64   `json:"diffBytes"`
}

type learnProbeContrast struct {
	Loaded     bool                  `json:"loaded"`
	NoiseFloor bool                  `json:"noiseFloor"`
	Metrics    []learnProbeMetricRow `json:"metrics"`
}

type learnProbeMetricRow struct {
	Key     string   `json:"key"`
	A       float64  `json:"a"`
	A2      *float64 `json:"a2"`
	B       float64  `json:"b"`
	DeltaAB float64  `json:"deltaAB"`
	Noise   *float64 `json:"noise"`
	Worse   bool     `json:"worse"`
}

func learnProbe(args []string) int {
	if len(args) > 0 && args[0] == "show" {
		return learnProbeShow(args[1:])
	}
	a, err := parseLearnArgs(args, nil, []string{"--quick", "--dry-run"})
	if err != nil {
		return learnUsageErr("probe", err)
	}
	id, err := a.id(0, "<proposal-id>")
	if err != nil {
		return learnUsageErr("probe", err)
	}
	params := map[string]any{"id": id, "quick": a.bools["--quick"]}
	if a.bools["--dry-run"] {
		var est learnProbeEstimate
		if code := learnCall("probe", "learning.probe.estimate", params, &est); code != 0 {
			return code
		}
		if est.Eligible {
			fmt.Println("eligible: yes")
		} else {
			fmt.Println("eligible: no")
		}
		for _, r := range est.Reasons {
			fmt.Printf("reason: %s: %s\n", r.Code, r.Message)
		}
		fmt.Printf("estimate: USD %.4f for %d arms (~%d min); cap USD %.2f; spent today USD %.4f\n",
			est.EstUSD, est.Arms, est.ExpectedMinutes, est.PerProbeCapUSD, est.SpentTodayUSD)
		return 0
	}
	var v learnProbeView
	if code := learnCall("probe", "learning.probe.start", params, &v); code != 0 {
		return code
	}
	est, err := learnProbeEstimateFor(id, a.bools["--quick"])
	if err == nil {
		fmt.Printf("probe %d started for proposal %d (est USD %.4f, ~%d min); follow with: smind learn probe show %d\n",
			v.ID, id, est.EstUSD, est.ExpectedMinutes, id)
	} else {
		fmt.Printf("probe %d started for proposal %d; follow with: smind learn probe show %d\n", v.ID, id, id)
	}
	return 0
}

// learnProbeEstimateFor fetches the estimate again for the start banner.
func learnProbeEstimateFor(id int64, quick bool) (learnProbeEstimate, error) {
	client, err := dialDaemon(context.Background())
	if err != nil {
		return learnProbeEstimate{}, err
	}
	defer client.Close()
	var est learnProbeEstimate
	if err := client.Call(context.Background(), "learning.probe.estimate", map[string]any{"id": id, "quick": quick}, &est); err != nil {
		return learnProbeEstimate{}, err
	}
	return est, nil
}

func learnProbeShow(args []string) int {
	a, err := parseLearnArgs(args, nil, []string{"--json"})
	if err != nil {
		return learnUsageErr("probe show", err)
	}
	id, err := a.id(0, "<proposal-id>")
	if err != nil {
		return learnUsageErr("probe show", err)
	}
	var raw json.RawMessage
	if code := learnCall("probe show", "learning.probe.get", map[string]any{"id": id}, &raw); code != 0 {
		return code
	}
	if a.bools["--json"] {
		printLearnJSON(raw)
		return 0
	}
	if string(raw) == "null" {
		fmt.Printf("no probe for proposal %d\n", id)
		return 0
	}
	var v learnProbeView
	if err := json.Unmarshal(raw, &v); err != nil {
		fmt.Fprintf(os.Stderr, "smind learn probe show: decode: %v\n", err)
		return 1
	}
	return printProbeView(v)
}

func printProbeView(v learnProbeView) int {
	verdict := "-"
	if v.Verdict != nil {
		verdict = *v.Verdict
	}
	fmt.Printf("probe %d for proposal %d: %s\n", v.ID, v.ProposalID, v.Status)
	fmt.Printf("verdict: %s (%s)\n", verdict, v.VerdictReason)
	tw := learnTable()
	for _, arm := range v.Arms {
		loaded, recurred := "-", "-"
		if arm.LoadedSkill != nil {
			loaded = yesNo(*arm.LoadedSkill)
		}
		if arm.FailureRecurred != nil {
			recurred = yesNo(*arm.FailureRecurred)
		}
		fmt.Fprintf(tw, "%s  %s  %s  $%.4f  loaded=%s recurred=%s\n",
			arm.Role, arm.Status, probeDuration(arm.ElapsedMs), arm.CostUSD, loaded, recurred)
	}
	tw.Flush()
	if v.Contrast != nil {
		fmt.Println("contrast:")
		tw := learnTable()
		fmt.Fprintln(tw, "METRIC\tCONTROL\tREPLICATE\tWITH SKILL\tΔ\tNOISE")
		for _, m := range v.Contrast.Metrics {
			replicate, noise := "-", "-"
			if m.A2 != nil {
				replicate = fmt.Sprintf("%g", *m.A2)
			}
			if m.Noise != nil {
				noise = fmt.Sprintf("%g", *m.Noise)
			}
			fmt.Fprintf(tw, "%s\t%g\t%s\t%g\t%g\t%s\n", m.Key, m.A, replicate, m.B, m.DeltaAB, noise)
		}
		tw.Flush()
	}
	fmt.Println("Behavioural contrast on one replay of the source episode, not a measurement.")
	return 0
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func probeDuration(ms int64) string {
	d := time.Duration(ms) * time.Millisecond
	if d < time.Minute {
		return d.Truncate(time.Millisecond).String()
	}
	m := int(d.Minutes())
	s := int(d.Seconds()) % 60
	return fmt.Sprintf("%dm%02ds", m, s)
}

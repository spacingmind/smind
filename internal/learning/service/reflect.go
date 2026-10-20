package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/learning"
	"github.com/spacingmind/smind/internal/learning/approval"
	"github.com/spacingmind/smind/internal/learning/executor"
	"github.com/spacingmind/smind/internal/learning/gate"
	"github.com/spacingmind/smind/internal/learning/redact"
	"github.com/spacingmind/smind/internal/learning/reflector"
	"github.com/spacingmind/smind/internal/learning/window"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

const (
	skipProviderNoIsolation = "provider_no_tool_isolation"
	skipBudget              = "budget"

	reflectTimeout = 5 * time.Minute
	priorTurns     = 10
	rejectedLimit  = 10
	budgetWindow   = 24 * time.Hour

	// verdictFieldBytes clips every model-derived string kept in a job's
	// verdicts column.
	verdictFieldBytes = 200
)

// verdict is one intent's outcome as stored in learning_jobs.verdicts.
type verdict struct {
	Name       string          `json:"name"`
	Action     string          `json:"action"`
	Accepted   bool            `json:"accepted"`
	Retry      bool            `json:"retry,omitempty"`
	ProposalID int64           `json:"proposalId,omitempty"`
	Findings   []verdictFinder `json:"findings,omitempty"`
}

type verdictFinder struct {
	Reason string `json:"reason"`
	Detail string `json:"detail,omitempty"`
}

// reflect is the reflect pipeline for one queued job. Watermark rules: a job
// that ends landed, empty, failed (executor error other than busy/cancel, or
// parse/scope failure) or skipped for the provider gate advances the chat's
// watermark and resets pending; a budget/disabled/mode skip, ErrBusy or a
// cancelled run leave both alone so the same window is retried.
func (s *Service) reflect(job *store.LearningJob, cw store.ChatWorkspace, cfg config.LearningConfig) {
	chatID := cw.ChatID
	s.setStatus(job, statusRunning, nil)

	chat, err := s.d.Store.GetChat(chatID)
	if err != nil {
		s.fail(job, "load chat: "+err.Error())
		return
	}
	wruns, err := s.d.Store.ListChatRunsWithEventsAfter(chatID, chat.LearnWatermark)
	if err != nil {
		s.fail(job, "load window: "+err.Error())
		return
	}
	var newest int64
	for _, wr := range wruns {
		for _, e := range wr.Events {
			newest = max(newest, e.ID)
		}
	}
	// terminal records the status and then advances the chat's watermark.
	terminal := func(status string, mutate func(*store.LearningJob)) {
		s.setStatus(job, status, mutate)
		if err := s.d.Store.SetChatLearnState(chatID, newest, 0); err != nil {
			slog.Error("learning: advance watermark", "chat", chatID, "err", err)
		}
	}

	// 2. Provider gate (ADR-0026 D13).
	provider := cw.Provider
	if provider == "" && len(wruns) > 0 {
		provider = wruns[len(wruns)-1].Run.Provider
	}
	if !learning.CanReflect(provider) {
		terminal(statusSkipped, func(j *store.LearningJob) {
			j.SkipReason = skipProviderNoIsolation
			j.WindowToEventID = newest
		})
		return
	}

	// 4. Window.
	rd := s.redactor(cw)
	learned, err := s.d.Store.ListLearnedSkills(cw.WorkspaceID, store.SkillActive, store.SkillDisabled)
	if err != nil {
		s.fail(job, "load learned skills: "+err.Error())
		return
	}
	prior, err := s.d.Store.ListChatPriorHumanPrompts(chatID, chat.LearnWatermark, priorTurns)
	if err != nil {
		s.fail(job, "load prior prompts: "+err.Error())
		return
	}
	var human []gate.LiveSkill
	if s.d.Human != nil {
		human = s.d.Human(cw.WorkspaceID)
	}
	out := window.Build(window.Input{
		Runs:           s.windowRuns(wruns),
		Redact:         rd.String,
		Caps:           window.CapsFromConfig(cfg),
		Learned:        skillRefs(learned),
		Human:          humanRefs(human),
		PriorUserTurns: prior,
	})
	newest = max(newest, out.LastEventID)
	if strings.TrimSpace(out.Text) == "" || (len(out.Window) == 0 && out.ToolCalls == 0) {
		terminal(statusEmpty, func(j *store.LearningJob) { j.WindowToEventID = newest })
		return
	}

	// 3. Budget (AC11): decided before any run starts; nothing advances.
	if over, err := s.overBudget(cw.WorkspaceID, cfg, len(out.Text)); err != nil {
		s.fail(job, "budget check: "+err.Error())
		return
	} else if over {
		s.skip(job, skipBudget)
		return
	}
	job.WindowToEventID = newest

	// 5. Execute.
	userPrompt := reflector.ComposeUserPrompt(out.Text, s.rejectedExamples(cw.WorkspaceID))
	res, err := s.d.Exec.Run(s.ctx, executor.Request{
		WorkspaceID: cw.WorkspaceID, Purpose: learning.PurposeReflect, Model: cfg.ReflectorModel,
		SystemPrompt: reflector.ReflectorPrompt(),
		UserPrompt:   userPrompt,
		MaxTurns:     1, Timeout: reflectTimeout,
	})
	var retryRes *executor.Result
	usage := func(j *store.LearningJob) {
		j.RunID = res.RunID
		if res.CostUSD != nil || (retryRes != nil && retryRes.CostUSD != nil) {
			cost := 0.0
			if res.CostUSD != nil {
				cost = *res.CostUSD
			}
			if retryRes != nil && retryRes.CostUSD != nil {
				cost += *retryRes.CostUSD
			}
			j.CostUSD = &cost
		}
		if res.InputTokens != nil || res.OutputTokens != nil ||
			(retryRes != nil && (retryRes.InputTokens != nil || retryRes.OutputTokens != nil)) {
			var n int64
			if res.InputTokens != nil {
				n += *res.InputTokens
			}
			if res.OutputTokens != nil {
				n += *res.OutputTokens
			}
			if retryRes != nil {
				if retryRes.InputTokens != nil {
					n += *retryRes.InputTokens
				}
				if retryRes.OutputTokens != nil {
					n += *retryRes.OutputTokens
				}
			}
			j.Tokens = &n
		}
	}
	if err != nil {
		retry := errors.Is(err, executor.ErrBusy) || s.ctx.Err() != nil || errors.Is(err, context.Canceled)
		failed := func(j *store.LearningJob) { usage(j); j.Error = clipError(err.Error()) }
		if retry {
			s.setStatus(job, statusFailed, failed)
			return
		}
		terminal(statusFailed, failed)
		return
	}

	// 6. Parse.
	intents, err := reflector.ParseOutput(res.FinalText, gate.ModeReflect)
	if err == nil {
		err = reflector.ScopeCheck(intents, gate.ModeReflect, out.ShownLearned)
	}
	if err != nil {
		terminal(statusFailed, func(j *store.LearningJob) { usage(j); j.Error = clipError(err.Error()) })
		return
	}

	// 7. Gate each intent and store the survivors.
	verdicts, created, err := s.gateAndStore(job, cw, cfg, rd, out, intents, learned, human)
	if err != nil {
		vj, _ := json.Marshal(verdicts)
		s.setStatus(job, statusFailed, func(j *store.LearningJob) {
			usage(j)
			j.Verdicts = string(vj)
			j.Error = clipError(err.Error())
		})
		return
	}

	// 7b. One-shot retry (ADR-0037 D11): verdicts rejected only for
	// repairable format reasons get one more executor call with the gate's
	// findings appended; the result is re-gated by the same gate. A retry
	// failure of any kind never fails the job.
	if items := retryItems(verdicts); len(items) > 0 {
		if v2, c2, res2 := s.retryReflect(job, cw, cfg, rd, out, learned, human, userPrompt, items, acceptedNames(verdicts)); res2 != nil {
			retryRes = res2
			for i := range v2 {
				v2[i].Retry = true
			}
			verdicts = append(verdicts, v2...)
			created += c2
		}
	}
	vj, _ := json.Marshal(verdicts)

	// 8. Terminal.
	status := statusEmpty
	if created > 0 {
		status = statusLanded
	}
	terminal(status, func(j *store.LearningJob) { usage(j); j.Verdicts = string(vj) })
}

// retryReflect runs the single reflector retry: the same executor request
// with a GATE_FINDINGS block appended to the original user prompt, then the
// same gate and store path. Intents that re-emit an already accepted name
// are dropped and at most len(items) are kept. It
// never fails the job: on any error the first-pass verdicts stand, and only
// the run result is returned for cost accounting.
func (s *Service) retryReflect(job *store.LearningJob, cw store.ChatWorkspace, cfg config.LearningConfig, rd *redact.Redactor,
	out window.Output, learned []store.LearnedSkill, human []gate.LiveSkill, userPrompt string, items []reflector.RetryItem, accepted map[string]bool) ([]verdict, int, *executor.Result) {
	prompt2 := reflector.ComposeRetryPrompt(userPrompt, items)
	if over, err := s.overBudget(cw.WorkspaceID, cfg, len(prompt2)); err != nil || over {
		return nil, 0, nil
	}
	res2, err := s.d.Exec.Run(s.ctx, executor.Request{
		WorkspaceID: cw.WorkspaceID, Purpose: learning.PurposeReflect, Model: cfg.ReflectorModel,
		SystemPrompt: reflector.ReflectorPrompt(),
		UserPrompt:   prompt2,
		MaxTurns:     1, Timeout: reflectTimeout,
	})
	if err != nil {
		return nil, 0, &res2
	}
	intents2, err := reflector.ParseOutput(res2.FinalText, gate.ModeReflect)
	if err == nil {
		err = reflector.ScopeCheck(intents2, gate.ModeReflect, out.ShownLearned)
	}
	if err != nil {
		return nil, 0, &res2
	}
	// At most one corrected intent per retried item, and never one that
	// re-emits an intent the first pass already stored. Matching by the
	// retried names would drop a corrected bad_name (the fix renames it).
	var kept []gate.Intent
	for _, in := range intents2 {
		if accepted[in.Name] || len(kept) >= len(items) {
			continue
		}
		kept = append(kept, in)
	}
	if len(kept) == 0 {
		return nil, 0, &res2
	}
	v2, c2, err := s.gateAndStore(job, cw, cfg, rd, out, kept, learned, human)
	if err != nil {
		slog.Warn("learning: reflector retry gate", "job", job.ID, "err", err)
		return nil, 0, &res2
	}
	return v2, c2, &res2
}

// overBudget applies the per-workspace daily budget. Runs with no reported
// usage are priced at the window cap; usage AT the dollar limit already
// blocks.
func (s *Service) overBudget(workspaceID int64, cfg config.LearningConfig, windowBytes int) (bool, error) {
	cost, tokens, unreported, err := s.d.Store.SumLearningUsageForWorkspace(workspaceID, s.now().Add(-budgetWindow))
	if err != nil {
		return false, err
	}
	tokens += int64(unreported) * int64(cfg.WindowKB*1024/4)
	if cost >= cfg.DailyBudgetUSD {
		return true, nil
	}
	return tokens+int64(windowBytes/4) > int64(cfg.DailyBudgetTokens), nil
}

func (s *Service) redactor(cw store.ChatWorkspace) *redact.Redactor {
	return NewRedactor(s.d.Store, s.d.Home, cw.WorkspacePath)
}

// NewRedactor builds the workspace redactor: MCP secrets plus the home,
// workspace and smind-home directories.
func NewRedactor(st *store.Store, home, workspacePath string) *redact.Redactor {
	var secrets []string
	if rows, err := st.ListMcpServers(); err == nil {
		secrets = taskrunner.McpSecrets(rows)
	} else {
		slog.Warn("learning: load MCP servers for redaction", "err", err)
	}
	var dirs []string
	if h, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, h)
	}
	dirs = append(dirs, workspacePath, home)
	return redact.New(secrets, dirs)
}

func (s *Service) windowRuns(wruns []store.ChatWindowRun) []window.Run {
	out := make([]window.Run, 0, len(wruns))
	for _, wr := range wruns {
		r := wr.Run
		wrun := window.Run{
			ID: r.ID, Provider: r.Provider, Status: r.Status, StopReason: r.StopReason, ErrMsg: r.ErrMsg,
			Origin: windowOrigin(r.Origin), Interrupt: r.Interrupt,
		}
		// A prompt reflected in an earlier window is background, not evidence.
		if wr.PromptInWindow {
			wrun.Prompt = r.Prompt
		}
		for _, e := range wr.Events {
			ev, err := s.d.DecodeEvent(e.EventData)
			if err != nil {
				slog.Warn("learning: skip undecodable event", "run", r.ID, "event", e.ID, "err", err)
				continue
			}
			wrun.Events = append(wrun.Events, window.Event{ID: e.ID, Seq: e.Seq, E: ev})
		}
		out = append(out, wrun)
	}
	return out
}

func windowOrigin(o string) window.Origin {
	switch window.Origin(o) {
	case window.OriginHuman, window.OriginOrchestrator, window.OriginAgent:
		return window.Origin(o)
	}
	return window.OriginUnknown
}

func skillRefs(skills []store.LearnedSkill) []window.SkillRef {
	refs := make([]window.SkillRef, 0, len(skills))
	for _, k := range skills {
		refs = append(refs, window.SkillRef{Name: k.Name, Description: k.Description, Body: k.Body})
	}
	return refs
}

func humanRefs(skills []gate.LiveSkill) []window.SkillRef {
	refs := make([]window.SkillRef, 0, len(skills))
	for _, k := range skills {
		refs = append(refs, window.SkillRef{Name: k.Name, Description: k.Description})
	}
	return refs
}

func (s *Service) rejectedExamples(workspaceID int64) []reflector.RejectedExample {
	rejected, err := s.d.Store.ListRejectedProposals(workspaceID, rejectedLimit)
	if err != nil {
		slog.Warn("learning: load rejected proposals", "err", err)
		return nil
	}
	out := make([]reflector.RejectedExample, 0, len(rejected))
	for _, p := range rejected {
		ex := reflector.RejectedExample{Name: p.Name, Note: p.Note}
		if items, err := approval.DecodeItems(p.Intents); err == nil && len(items) > 0 {
			ex.Description = items[0].Result.Description
		}
		out = append(out, ex)
	}
	return out
}

// repairable is the set of gate reasons that describe a structurally fixable
// formatting error: a retry that shows the model these findings can plausibly
// fix them. Provenance, guard, secret, path/host, hidden-text, similarity and
// patch-target rejections are never retried.
var repairable = map[gate.Reason]bool{
	gate.ReasonBadName:        true,
	gate.ReasonBodyTooLong:    true,
	gate.ReasonBadDescription: true,
	gate.ReasonBadCategory:    true,
	gate.ReasonBadFrontmatter: true,
	gate.ReasonURLLink:        true,
	gate.ReasonBadReason:      true,
}

// acceptedNames is the set of intent names the verdicts stored as proposals.
func acceptedNames(verdicts []verdict) map[string]bool {
	out := map[string]bool{}
	for _, v := range verdicts {
		if v.Accepted {
			out[v.Name] = true
		}
	}
	return out
}

// retryItems returns the verdicts that were rejected only for repairable
// reasons, as retry prompt items. An accepted verdict or one carrying any
// non-repairable finding does not qualify.
func retryItems(verdicts []verdict) []reflector.RetryItem {
	var items []reflector.RetryItem
	for _, v := range verdicts {
		if v.Accepted || len(v.Findings) == 0 {
			continue
		}
		ok := true
		for _, f := range v.Findings {
			if !repairable[gate.Reason(f.Reason)] {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		item := reflector.RetryItem{Name: v.Name}
		for _, f := range v.Findings {
			item.Findings = append(item.Findings, reflector.RetryFinding{Reason: f.Reason, Detail: f.Detail})
		}
		items = append(items, item)
	}
	return items
}

// gateAndStore gates every intent independently and stores the accepted ones
// as pending proposals. It returns every intent's verdict (rejections carry
// their findings, clipped) and how many proposals were created.
func (s *Service) gateAndStore(job *store.LearningJob, cw store.ChatWorkspace, cfg config.LearningConfig, rd *redact.Redactor,
	out window.Output, intents []gate.Intent, learned []store.LearnedSkill, human []gate.LiveSkill) ([]verdict, int, error) {
	byName := make(map[string]store.LearnedSkill, len(learned))
	live := make([]gate.LiveSkill, 0, len(learned)+len(human))
	for _, k := range learned {
		byName[k.Name] = k
		live = append(live, gate.LiveSkill{Name: k.Name, Category: k.Category, Description: k.Description, Body: k.Body, Status: k.Status})
	}
	if pending, err := s.d.Store.ListProposals(cw.WorkspaceID, "pending"); err == nil {
		for _, p := range pending {
			if p.Kind != "create" {
				continue
			}
			if items, err := approval.DecodeItems(p.Intents); err == nil && len(items) > 0 {
				r := items[0].Result
				live = append(live, gate.LiveSkill{Name: r.Name, Category: r.Category, Description: r.Description, Body: r.Body, Status: "proposed"})
			}
		}
	} else {
		return nil, 0, fmt.Errorf("load pending proposals: %w", err)
	}
	live = append(live, human...)

	gcfg := gate.DefaultConfig()
	gcfg.Mode = gate.ModeReflect
	gcfg.Redact = rd.String
	if cfg.Similarity.Duplicate > 0 {
		gcfg.Duplicate = cfg.Similarity.Duplicate
	}
	if cfg.Similarity.Possible > 0 {
		gcfg.Possible = cfg.Similarity.Possible
	}

	var verdicts []verdict
	created := 0
	for _, in := range intents {
		v := gate.Check(in, out.Window, live, gcfg)
		entry := verdict{Name: clipField(in.Name), Action: clipField(string(in.Action))}
		if v.OK && secretIn(rd, in) {
			v = gate.Verdict{Findings: []gate.Finding{{Reason: gate.ReasonSecret, Detail: "evidence or reason changed under redaction"}}}
		}
		if !v.OK {
			for _, f := range v.Findings {
				entry.Findings = append(entry.Findings, verdictFinder{Reason: clipField(string(f.Reason)), Detail: clipField(f.Detail)})
			}
			verdicts = append(verdicts, entry)
			continue
		}
		evidence, err := json.Marshal(in.Evidence)
		if err != nil {
			return verdicts, created, fmt.Errorf("encode evidence: %w", err)
		}
		item := approval.Item{Intent: in, Result: v.Result}
		kind := string(in.Action)
		if in.Action == gate.ActionPatch {
			target := byName[in.Name]
			item.TargetSkillID, item.BaseVersion = target.ID, target.Version
		}
		name := v.Result.Name
		if name == "" {
			name = in.Name
		}
		p, err := s.d.Approval.CreateProposal(approval.NewProposal{
			WorkspaceID: cw.WorkspaceID, JobID: job.ID, Source: "reflector", Kind: kind, Name: name,
			Items: []approval.Item{item}, Evidence: string(evidence),
			PossibleDuplicateOf: v.PossibleDuplicateOf, Similarity: v.Similarity,
		})
		if err != nil {
			return verdicts, created, fmt.Errorf("create proposal: %w", err)
		}
		created++
		entry.Accepted, entry.ProposalID = true, p.ID
		verdicts = append(verdicts, entry)
		if in.Action == gate.ActionCreate {
			r := v.Result
			live = append(live, gate.LiveSkill{Name: r.Name, Category: r.Category, Description: r.Description, Body: r.Body, Status: "proposed"})
		}
		s.d.Notify.NotifyProposed(cw.WorkspaceID, p.ID, name, kind)
	}
	return verdicts, created, nil
}

// secretIn reports whether any text about to be stored changes under
// redaction.
func secretIn(rd *redact.Redactor, in gate.Intent) bool {
	if rd.Changed(in.Reason) {
		return true
	}
	for _, e := range in.Evidence {
		if rd.Changed(e.Quote) {
			return true
		}
	}
	return false
}

func clipField(s string) string {
	if len(s) <= verdictFieldBytes {
		return s
	}
	n := verdictFieldBytes
	for n > 0 && s[n]&0xC0 == 0x80 {
		n--
	}
	return s[:n]
}

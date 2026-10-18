package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/learning/executor"
	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/store"
)

func TestCollector_OffByDefault(t *testing.T) {
	f := newFx(t, "") // learning_mode defaults to off
	f.addRun(f.chat, "hello", textEv("x"))
	f.finished(f.chat, "claude-native", 500)
	if got := f.pending(f.chat); got != 0 {
		t.Fatalf("pending = %d, want 0", got)
	}
	if js := f.jobs(); len(js) != 0 {
		t.Fatalf("jobs = %+v, want none", js)
	}
	if err := f.svc.Reflect(context.Background(), f.chat.ID); !errors.Is(err, ErrModeOff) {
		t.Fatalf("Reflect = %v, want ErrModeOff", err)
	}
	f.svc.tailFlush()
	f.svc.wg.Wait()
	if f.exec.count() != 0 || len(f.jobs()) != 0 {
		t.Fatalf("executor calls %d, jobs %d", f.exec.count(), len(f.jobs()))
	}
}

func TestCollector_CadenceAt50ToolCalls(t *testing.T) {
	f := newFx(t, "propose")
	if f.config().CadenceToolCalls != 50 {
		t.Fatalf("default cadence = %d", f.config().CadenceToolCalls)
	}
	f.finished(f.chat, "claude-native", 49)
	if js := f.jobs(); len(js) != 0 || f.pending(f.chat) != 49 {
		t.Fatalf("49 calls: jobs %d pending %d", len(js), f.pending(f.chat))
	}
	f.finished(f.chat, "claude-native", 1)
	js := f.jobs()
	if len(js) != 1 || js[0].TriggerKind != "cadence" || js[0].Kind != "reflect" || js[0].ChatID == nil || *js[0].ChatID != f.chat.ID {
		t.Fatalf("jobs = %+v, want exactly one cadence reflect job", js)
	}
}

func TestCollector_ExplicitTriggerBypassesCooldown(t *testing.T) {
	f := newFx(t, "propose")
	f.addRun(f.chat, "hello", textEv("x"))
	// The cadence job ends retryable (busy executor): pending is kept.
	f.exec.fn = func(executor.Request) (executor.Result, error) { return executor.Result{}, executor.ErrBusy }
	f.finished(f.chat, "claude-native", 50) // cadence job -> starts the cool-down
	if len(f.jobs()) != 1 {
		t.Fatalf("jobs = %d, want 1", len(f.jobs()))
	}
	f.clk.advance(5 * time.Minute)
	f.finished(f.chat, "claude-native", 5) // pending still >= 50 but cooling down
	if len(f.jobs()) != 1 {
		t.Fatalf("cadence inside the cool-down created a job: %d", len(f.jobs()))
	}
	if got := f.pending(f.chat); got != 55 {
		t.Fatalf("pending = %d, want 55 kept for a later trigger", got)
	}
	if err := f.svc.Reflect(context.Background(), f.chat.ID); err != nil {
		t.Fatalf("explicit Reflect = %v", err)
	}
	f.svc.wg.Wait()
	js := f.jobs()
	if len(js) != 2 || js[0].TriggerKind != "explicit" {
		t.Fatalf("jobs = %+v, want an explicit job after the cadence one", js)
	}
	// An explicit job does not start a cool-down of its own.
	f.clk.advance(26 * time.Minute) // 31 min after the cadence job
	f.finished(f.chat, "claude-native", 1)
	if len(f.jobs()) != 3 {
		t.Fatalf("cadence after the cool-down: jobs = %d, want 3", len(f.jobs()))
	}
}

func TestCollector_ExplicitErrors(t *testing.T) {
	f := newFx(t, "propose")
	if err := f.svc.Reflect(context.Background(), f.chat.ID); !errors.Is(err, ErrNothingToReflect) {
		t.Fatalf("no events: %v, want ErrNothingToReflect", err)
	}
	_, ids := f.addRun(f.chat, "hello", textEv("x"))
	if err := f.st.SetChatLearnState(f.chat.ID, ids[len(ids)-1], 0); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Reflect(context.Background(), f.chat.ID); !errors.Is(err, ErrNothingToReflect) {
		t.Fatalf("all reflected: %v, want ErrNothingToReflect", err)
	}
	f.addRun(f.chat, "again", textEv("y"))
	f.setConfig(func(c *config.LearningConfig) { c.Enabled = false })
	if err := f.svc.Reflect(context.Background(), f.chat.ID); !errors.Is(err, ErrDisabled) {
		t.Fatalf("disabled: %v", err)
	}
	f.setConfig(func(c *config.LearningConfig) { c.Enabled = true })
	if err := f.svc.Curate(context.Background(), f.ws.ID); !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("Curate = %v", err)
	}
	sys, sysChat, _ := f.st.CreateSystemTask(f.ws.ID, "learning", "/cwd")
	_ = sys
	if err := f.svc.Reflect(context.Background(), sysChat.ID); !errors.Is(err, ErrNotEligible) {
		t.Fatalf("system chat: %v", err)
	}
}

func TestCollector_IdleTailFlush(t *testing.T) {
	setup := func(t *testing.T, pending int64) *fx {
		f := newFx(t, "propose")
		f.addRun(f.chat, "hello", textEv("x")) // finished at t0+1m
		if _, err := f.st.AddChatLearnPending(f.chat.ID, pending); err != nil {
			t.Fatal(err)
		}
		return f
	}
	t.Run("idle with enough pending", func(t *testing.T) {
		f := setup(t, 10)
		f.clk.advance(20 * time.Minute)
		f.svc.tailFlush()
		f.svc.wg.Wait()
		if len(f.jobs()) != 0 {
			t.Fatal("flushed before the idle time")
		}
		f.clk.advance(11 * time.Minute) // 31 min after the run finished
		f.svc.tailFlush()
		f.svc.wg.Wait()
		js := f.jobs()
		if len(js) != 1 || js[0].TriggerKind != "tail" {
			t.Fatalf("jobs = %+v, want one tail job", js)
		}
		// A tail job respects the cool-down.
		f.svc.tailFlush()
		f.svc.wg.Wait()
		if len(f.jobs()) != 1 {
			t.Fatal("second tail job inside the cool-down")
		}
	})
	t.Run("too few pending", func(t *testing.T) {
		f := setup(t, 9)
		f.clk.advance(time.Hour)
		f.svc.tailFlush()
		f.svc.wg.Wait()
		if len(f.jobs()) != 0 {
			t.Fatal("flushed with pending < 10")
		}
	})
	t.Run("archived chat", func(t *testing.T) {
		f := setup(t, 12)
		if _, err := f.st.ArchiveChat(f.chat.ID); err != nil {
			t.Fatal(err)
		}
		f.svc.tailFlush()
		f.svc.wg.Wait()
		if js := f.jobs(); len(js) != 1 || js[0].TriggerKind != "tail" {
			t.Fatalf("jobs = %+v, want one tail job for the archived chat", js)
		}
	})
}

func TestCollector_OneJobPerWorkspace(t *testing.T) {
	f := newFx(t, "propose")
	chat2 := f.newChat(f.task, "claude-native")
	f.addRun(f.chat, "a", textEv("x"))
	f.addRun(chat2, "b", textEv("y"))

	release := make(chan struct{})
	started := make(chan struct{}, 64)
	f.svc.onJobStart = func(int64) {
		started <- struct{}{}
		<-release
	}

	// Two chats both over cadence: the second waits (its count is kept).
	for _, c := range []store.Chat{f.chat, chat2} {
		f.svc.handleFinished(runs.RunFinished{RunID: "r", TaskID: c.TaskID, ChatID: c.ID, Provider: "claude-native", Purpose: "user", ToolCalls: 60})
	}
	<-started
	if js := f.jobs(); len(js) != 1 {
		t.Fatalf("jobs = %d, want 1 while the first is in flight", len(js))
	}
	if got := f.pending(chat2); got != 60 {
		t.Fatalf("waiting chat pending = %d, want 60 kept", got)
	}

	// Concurrent explicit triggers while a job is in flight: all refused.
	var wg sync.WaitGroup
	var busy atomic.Int32
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c := f.chat
			if i%2 == 1 {
				c = chat2
			}
			if err := f.svc.Reflect(context.Background(), c.ID); errors.Is(err, ErrBusy) {
				busy.Add(1)
			}
		}(i)
	}
	wg.Wait()
	if busy.Load() != 16 || len(f.jobs()) != 1 {
		t.Fatalf("busy = %d, jobs = %d, want 16 / 1", busy.Load(), len(f.jobs()))
	}
	close(release)
	f.svc.wg.Wait()
}

func TestCollector_ConcurrentTriggersProduceOneJob(t *testing.T) {
	f := newFx(t, "propose")
	chats := []store.Chat{f.chat, f.newChat(f.task, "claude-native"), f.newChat(f.task, "claude-native")}
	for _, c := range chats {
		f.addRun(c, "p", textEv("x"))
	}
	release := make(chan struct{})
	f.svc.onJobStart = func(int64) { <-release }
	var wg sync.WaitGroup
	var ok atomic.Int32
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := f.svc.Reflect(context.Background(), chats[i%3].ID); err == nil {
				ok.Add(1)
			}
		}(i)
	}
	wg.Wait()
	close(release)
	f.svc.wg.Wait()
	if ok.Load() != 1 || len(f.jobs()) != 1 {
		t.Fatalf("accepted = %d, jobs = %d, want exactly 1 / 1", ok.Load(), len(f.jobs()))
	}
}

func TestCollector_CodexRunsDoNotCount(t *testing.T) {
	f := newFx(t, "propose")
	codex := f.newChat(f.task, "codex-native")
	f.finished(codex, "codex-native", 500)
	if got := f.pending(codex); got != 0 || len(f.jobs()) != 0 {
		t.Fatalf("codex: pending %d jobs %d", got, len(f.jobs()))
	}
	// GLM does emit tool events and counts.
	glm := f.newChat(f.task, "glm")
	f.finished(glm, "glm", 7)
	if got := f.pending(glm); got != 7 {
		t.Fatalf("glm pending = %d, want 7", got)
	}
}

func TestReflect_DoesNotRetriggerLearning(t *testing.T) {
	f := newFx(t, "propose")
	for _, purpose := range []string{"reflect", "curate"} {
		f.svc.handleFinished(runs.RunFinished{RunID: "r", TaskID: f.chat.TaskID, ChatID: f.chat.ID, Provider: "claude-native", Purpose: purpose, ToolCalls: 500})
	}
	// A system task's run, even labelled "user", is ignored too.
	_, sysChat, err := f.st.CreateSystemTask(f.ws.ID, "learning", "/cwd")
	if err != nil {
		t.Fatal(err)
	}
	f.svc.handleFinished(runs.RunFinished{RunID: "r", TaskID: sysChat.TaskID, ChatID: sysChat.ID, Provider: "claude-native", Purpose: "user", ToolCalls: 500})
	f.svc.wg.Wait()
	sc, _ := f.st.GetChat(sysChat.ID)
	if f.pending(f.chat) != 0 || sc.LearnPending != 0 || len(f.jobs()) != 0 || f.exec.count() != 0 {
		t.Fatalf("learning retriggered itself: pending %d/%d jobs %d", f.pending(f.chat), sc.LearnPending, len(f.jobs()))
	}
}

func TestKillSwitch_StopsEverything(t *testing.T) {
	f := newFx(t, "propose")
	f.addRun(f.chat, "hello", textEv("x"))
	f.setConfig(func(c *config.LearningConfig) { c.Enabled = false })

	f.finished(f.chat, "claude-native", 500)
	if f.pending(f.chat) != 0 || len(f.jobs()) != 0 {
		t.Fatal("observer acted with the kill switch off")
	}
	if err := f.svc.Reflect(context.Background(), f.chat.ID); !errors.Is(err, ErrDisabled) {
		t.Fatalf("Reflect = %v, want ErrDisabled", err)
	}
	if _, err := f.st.AddChatLearnPending(f.chat.ID, 20); err != nil {
		t.Fatal(err)
	}
	f.clk.advance(time.Hour)
	f.svc.tailFlush()
	f.svc.wg.Wait()
	if len(f.jobs()) != 0 {
		t.Fatal("tail flush ran with the kill switch off")
	}

	// A job queued while enabled is skipped when the switch flips before it runs.
	f.setConfig(func(c *config.LearningConfig) { c.Enabled = true })
	release := make(chan struct{})
	started := make(chan struct{})
	f.svc.onJobStart = func(int64) { close(started); <-release }
	if err := f.svc.Reflect(context.Background(), f.chat.ID); err != nil {
		t.Fatal(err)
	}
	<-started
	f.setConfig(func(c *config.LearningConfig) { c.Enabled = false })
	close(release)
	f.svc.wg.Wait()
	js := f.jobs()
	if len(js) != 1 || js[0].Status != "skipped" || js[0].SkipReason != "disabled" || f.exec.count() != 0 {
		t.Fatalf("jobs = %+v, exec calls %d", js, f.exec.count())
	}
}

func TestService_StartFailsInterruptedJobsAndConsumesEvents(t *testing.T) {
	f := newFx(t, "propose")
	if err := f.st.SetChatLearnState(f.chat.ID, 77, 30); err != nil {
		t.Fatal(err)
	}
	stale, err := f.st.CreateLearningJob(store.LearningJob{WorkspaceID: f.ws.ID, ChatID: &f.chat.ID, Kind: "reflect", TriggerKind: "cadence", Status: "running"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.svc.Start(ctx)
	f.svc.Start(ctx) // idempotent
	got, _ := f.st.GetLearningJob(stale.ID)
	if got.Status != "failed" || got.Error != "interrupted by restart" {
		t.Fatalf("stale job = %+v", got)
	}
	if c, _ := f.st.GetChat(f.chat.ID); c.LearnWatermark != 77 || c.LearnPending != 30 {
		t.Fatalf("watermark/pending touched: %d/%d", c.LearnWatermark, c.LearnPending)
	}

	// Through the channel: the observer returns at once and the loop counts.
	f.svc.OnRunFinished(runs.RunFinished{RunID: "r", TaskID: f.chat.TaskID, ChatID: f.chat.ID, Provider: "claude-native", Purpose: "user", ToolCalls: 3})
	deadline := time.Now().Add(5 * time.Second)
	for f.pending(f.chat) != 33 {
		if time.Now().After(deadline) {
			t.Fatalf("pending = %d, want 33 via the event loop", f.pending(f.chat))
		}
		time.Sleep(5 * time.Millisecond)
	}
	f.svc.Close()
	f.svc.OnRunFinished(runs.RunFinished{}) // after Close: still never blocks
}

func TestObserver_NeverBlocksWhenQueueFull(t *testing.T) {
	f := newFx(t, "propose") // not started: nothing drains the queue
	done := make(chan struct{})
	go func() {
		for i := 0; i < eventQueue*3; i++ {
			f.svc.OnRunFinished(runs.RunFinished{RunID: "r"})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("OnRunFinished blocked on a full queue")
	}
}

package taskrunner

import (
	"context"
	"errors"

	claudecode "github.com/spacingmind/claude-agent-sdk-go"
)

// claudeTurnStreamer is the part of *claudecode.Client that sends a turn and
// reads its messages separately, which claudePrompt needs to look past a result
// that does not belong to the turn it sent (see forwardTurnSkippingStale).
// A claudeBackend that does not implement it falls back to its own Prompt.
type claudeTurnStreamer interface {
	Query(ctx context.Context, text string) error
	QueryBlocks(ctx context.Context, blocks []claudecode.PromptBlock) error
	ReceiveResponse(ctx context.Context) <-chan claudecode.Message
}

// isNotificationResult reports whether res is the empty result the CLI emits
// for a turn it started by itself to handle a background-task notification:
// no turns, no stop reason, no output, not an error. A real turn always has at
// least one turn and a stop reason, so this cannot match a normal result. Its
// modelUsage is NOT part of the test: under the SDK's session it repeats the
// previous turn's usage (observed), so it is empty in a bare CLI and non-empty
// here.
func isNotificationResult(res claudecode.ResultMessage) bool {
	return res.NumTurns == 0 && res.StopReason == "" && !res.IsError && res.Result == ""
}

// forwardTurnSkippingStale is claudecode.Client.Prompt's read loop (forward
// every message onto updates until the turn's result, which is returned
// instead) with one difference: it does not stop at a result produced for a
// background-task notification.
//
// Why: resuming a session whose previous run left a background shell command
// behind (a run that started `sleep 90` in the background, then ended) makes
// the CLI report that command as lost with a task_notification and answer it
// with its own turn, an empty zero-turn result, BEFORE it handles the prompt
// that was just sent. Prompt returns at that first result, so the run ended
// "done" in seconds with no output and no stop reason, and the real reply was
// discarded when the client closed. Observed against claude 2.1.303; the real
// answer arrives as the next result on the same stream.
//
// Each task_notification seen allows one empty result to be skipped, no more,
// so a prompt whose genuine result is empty (a local slash command with no
// output) still ends at its own result instead of waiting forever.
func forwardTurnSkippingStale(ctx context.Context, recv func(context.Context) <-chan claudecode.Message, updates chan<- claudecode.Message) (claudecode.ResultMessage, error) {
	notifications := 0
	for {
		sawResult := false
		for msg := range recv(ctx) {
			switch m := msg.(type) {
			case claudecode.TaskNotificationMessage:
				// A background task (a shell command started with
				// run_in_background) ended or was found lost.
				notifications++
			case claudecode.ResultMessage:
				sawResult = true
				if notifications > 0 && isNotificationResult(m) {
					notifications--
					continue
				}
				return m, nil
			}
			select {
			case updates <- msg:
			case <-ctx.Done():
				return claudecode.ResultMessage{}, ctx.Err()
			}
		}
		if err := ctx.Err(); err != nil {
			return claudecode.ResultMessage{}, err
		}
		if !sawResult {
			return claudecode.ResultMessage{}, errors.New("claudecode: cli exited before sending a result message")
		}
		// A skipped result closed this read; the turn's own messages are next.
	}
}

// claudeSendText sends prompt as one turn and returns its result, closing
// updates like claudecode.Client.Prompt does.
func claudeSendText(ctx context.Context, client claudeBackend, prompt string, updates chan<- claudecode.Message) (claudecode.ResultMessage, error) {
	s, ok := client.(claudeTurnStreamer)
	if !ok {
		return client.Prompt(ctx, prompt, updates)
	}
	defer close(updates)
	if err := s.Query(ctx, prompt); err != nil {
		return claudecode.ResultMessage{}, err
	}
	return forwardTurnSkippingStale(ctx, s.ReceiveResponse, updates)
}

// claudeSendBlocks is claudeSendText for a multi-block turn (text and images).
func claudeSendBlocks(ctx context.Context, client claudeBackend, blocks []claudecode.PromptBlock, updates chan<- claudecode.Message) (claudecode.ResultMessage, error) {
	s, ok := client.(claudeTurnStreamer)
	if !ok {
		return client.PromptBlocks(ctx, blocks, updates)
	}
	defer close(updates)
	if err := s.QueryBlocks(ctx, blocks); err != nil {
		return claudecode.ResultMessage{}, err
	}
	return forwardTurnSkippingStale(ctx, s.ReceiveResponse, updates)
}

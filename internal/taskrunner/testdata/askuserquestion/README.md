Recorded from a real Claude Code CLI 2.1.310 run (Haiku, stream-json, control protocol) in an isolated directory.

- `single.input.json`, `multi.input.json`: the `input` of the `can_use_tool` control request for `AskUserQuestion`.
- The answer the CLI accepted (and relayed to the model as "Your questions have been answered: ...") is the same input plus
  `"answers": {"<question text>": "<label>"}`; a multi-select answer is the labels joined with ", "; free text not among the options
  is accepted as is. A deny with a message reaches the model as an error tool result carrying that message. The CLI did not advertise the
  `ask_user_question_unanswered_v1` capability, and still sent the request under bypassPermissions.

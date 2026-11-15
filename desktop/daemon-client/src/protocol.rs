//! Parsing of the wsapi wire protocol (internal/wsapi/wsapi.go and
//! events.go): one JSON object per WS text message; we only need the
//! server-pushed event notification shape plus enough of the envelope
//! to ignore responses to our own requests.

use serde::Deserialize;

/// ServerMessage is the inbound half of the wsapi envelope. Fields the
/// desktop client never sends/receives meaningfully (cancels are
/// client->server) are simply absent here.
#[derive(Debug, Deserialize)]
pub struct ServerMessage {
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub id: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub event: Option<EventNotification>,
    /// The result of a request this client sent (currently only
    /// task.get, for resolving a notification's workspaceId). Present
    /// only alongside a matching `id`, never alongside `event`.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub result: Option<serde_json::Value>,
}

/// EventNotification is the pushed-event wire shape per ADR 0005:
/// {"topic": "...", "seq": N, "payload": {...}} with no id.
#[derive(Debug, Deserialize)]
pub struct EventNotification {
    pub topic: String,
    pub seq: i64,
    pub payload: serde_json::Value,
}

/// PermissionPending is the payload of permission.pending events:
/// {runId, taskId, chatId, requestId, summary, options}.
#[derive(Debug, Deserialize)]
pub struct PermissionPending {
    #[serde(rename = "runId")]
    pub run_id: String,
    #[serde(rename = "taskId")]
    pub task_id: i64,
    #[serde(default, rename = "chatId")]
    pub chat_id: i64,
    #[serde(rename = "requestId")]
    pub request_id: String,
    pub summary: String,
    #[serde(default)]
    pub options: Vec<serde_json::Value>,
}

/// PermissionResolved is the payload of permission.resolved events:
/// {runId, taskId, requestId, ...} -- only the fields this client needs
/// (see internal/wsapi/events.go's permissionResolvedPayload).
#[derive(Debug, Deserialize)]
pub struct PermissionResolved {
    #[serde(rename = "runId")]
    pub run_id: String,
    #[serde(rename = "taskId")]
    pub task_id: i64,
    #[serde(rename = "requestId")]
    pub request_id: String,
}

/// permission_resolved decodes a permission.resolved event's payload.
pub fn permission_resolved(payload: &serde_json::Value) -> Option<PermissionResolved> {
    serde_json::from_value(payload.clone()).ok()
}

/// QuestionPending is the payload of question.pending events (ADR-0055):
/// {runId, taskId, chatId, requestId, request} -- only the fields this
/// client needs; the request text itself is deliberately not carried
/// into any notification (DT5 B: fixed bodies).
#[derive(Debug, Deserialize)]
pub struct QuestionPending {
    #[serde(rename = "runId")]
    pub run_id: String,
    #[serde(rename = "taskId")]
    pub task_id: i64,
    #[serde(default, rename = "chatId")]
    pub chat_id: i64,
    #[serde(rename = "requestId")]
    pub request_id: String,
}

/// question_pending decodes a question.pending event's payload.
pub fn question_pending(payload: &serde_json::Value) -> Option<QuestionPending> {
    serde_json::from_value(payload.clone()).ok()
}

/// QuestionResolved is the payload of question.resolved events:
/// {runId, taskId, chatId, requestId, reason} -- only the request id
/// matters here (the clear is by request id, like permission.resolved).
#[derive(Debug, Deserialize)]
pub struct QuestionResolved {
    #[serde(rename = "requestId")]
    pub request_id: String,
}

/// question_resolved decodes a question.resolved event's payload.
pub fn question_resolved(payload: &serde_json::Value) -> Option<QuestionResolved> {
    serde_json::from_value(payload.clone()).ok()
}

/// RunStatus is the payload of run.status events:
/// {runId, taskId, chatId, status, stopReason, err} -- only the fields
/// this client needs (see internal/wsapi/events.go's runStatusPayload).
#[derive(Debug, Deserialize)]
pub struct RunStatus {
    #[serde(default, rename = "runId")]
    pub run_id: String,
    #[serde(rename = "taskId")]
    pub task_id: i64,
    #[serde(default, rename = "chatId")]
    pub chat_id: i64,
    pub status: String,
}

/// run_status decodes a run.status event's payload.
pub fn run_status(payload: &serde_json::Value) -> Option<RunStatus> {
    serde_json::from_value(payload.clone()).ok()
}

/// NotifyKind is which attention event a notification is for. The
/// notification text is fixed per kind (DT5 B/AC5): an OS banner can be
/// read from a lock screen, so it never carries prompt, question,
/// command, error or task-title text from the event payload.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum NotifyKind {
    Permission,
    Question,
    Failure,
}

impl NotifyKind {
    pub fn fixed_title(self) -> &'static str {
        match self {
            NotifyKind::Permission => "sMind: permission needed",
            NotifyKind::Question => "sMind: a question for you",
            NotifyKind::Failure => "sMind: run failed",
        }
    }

    pub fn fixed_body(self) -> &'static str {
        match self {
            NotifyKind::Permission => "A run needs your approval",
            NotifyKind::Question => "An agent has a question",
            NotifyKind::Failure => "Run failed",
        }
    }
}

/// Notification is the OS-notification-ready form of an event: a kind
/// with its FIXED title/body (never derived from the payload), plus
/// task_id (the source task, so the click handler can look up its
/// workspaceId -- see `crate::cache::WorkspaceCache` -- and build a
/// route), request_id and chat_id. For Permission/Question request_id
/// is the request id and feeds the tray's attention count
/// (`crate::attention`), which dedupes by it; for Failure it carries
/// the RUN id, used only as the dedupe/click key. chat_id names the
/// chat tab the request's card lives in (0 = unknown, from a daemon
/// payload without a chatId).
#[derive(Debug, PartialEq, Eq)]
pub struct Notification {
    pub kind: NotifyKind,
    pub title: String,
    pub body: String,
    pub task_id: i64,
    pub chat_id: i64,
    pub request_id: String,
}

impl Notification {
    fn fixed(kind: NotifyKind, task_id: i64, chat_id: i64, request_id: String) -> Self {
        Self {
            kind,
            title: kind.fixed_title().to_string(),
            body: kind.fixed_body().to_string(),
            task_id,
            chat_id,
            request_id,
        }
    }
}

/// TaskGetResult is the subset of task.get's result (a store.Task,
/// marshalled with no `json` tags -- see internal/store/types.go -- so
/// its wire keys are the exact Go field names) this client needs: the
/// workspaceId a permission.pending notification's taskId belongs to.
#[derive(Debug, Deserialize)]
pub struct TaskGetResult {
    #[serde(rename = "WorkspaceID")]
    pub workspace_id: i64,
}

/// task_get_message builds a task.get request. `id` is expected to be
/// `task_get_request_id(task_id)` so the response can be matched back to
/// the task without extra bookkeeping.
pub fn task_get_message(id: &str, task_id: i64) -> serde_json::Value {
    serde_json::json!({
        "id": id,
        "method": "task.get",
        "params": { "id": task_id }
    })
}

const TASK_GET_ID_PREFIX: &str = "task-get-";

/// task_get_request_id is the deterministic id a task.get request for
/// `task_id` is sent with.
pub fn task_get_request_id(task_id: i64) -> String {
    format!("{TASK_GET_ID_PREFIX}{task_id}")
}

/// task_get_response_task_id extracts the task id back out of a
/// response's `id`, if it looks like one of ours.
pub fn task_get_response_task_id(id: &str) -> Option<i64> {
    id.strip_prefix(TASK_GET_ID_PREFIX)?.parse().ok()
}

/// task_get_workspace_id decodes a task.get response's `result` into the
/// workspaceId it carries.
pub fn task_get_workspace_id(result: &serde_json::Value) -> Option<i64> {
    serde_json::from_value::<TaskGetResult>(result.clone())
        .ok()
        .map(|r| r.workspace_id)
}

/// parse_server_message decodes one inbound WS text message.
pub fn parse_server_message(text: &str) -> Option<ServerMessage> {
    serde_json::from_str(text).ok()
}

/// permission_pending decodes a permission.pending event's payload.
pub fn permission_pending(payload: &serde_json::Value) -> Option<PermissionPending> {
    serde_json::from_value(payload.clone()).ok()
}

/// notification_for turns an attention event into an OS notification
/// with FIXED per-kind text (see `NotifyKind`): permission.pending,
/// question.pending, and run.status with status exactly "error". For a
/// failed run the notification's request_id carries the RUN id (the
/// dedupe/click key). Returns None for everything else, including
/// run.status done/running/stopped -- those are not attention.
pub fn notification_for(event: &EventNotification) -> Option<Notification> {
    match event.topic.as_str() {
        "permission.pending" => {
            let p = permission_pending(&event.payload)?;
            Some(Notification::fixed(NotifyKind::Permission, p.task_id, p.chat_id, p.request_id))
        }
        "question.pending" => {
            let q = question_pending(&event.payload)?;
            Some(Notification::fixed(NotifyKind::Question, q.task_id, q.chat_id, q.request_id))
        }
        "run.status" => {
            let rs = run_status(&event.payload)?;
            if rs.status != "error" {
                return None;
            }
            Some(Notification::fixed(NotifyKind::Failure, rs.task_id, rs.chat_id, rs.run_id))
        }
        _ => None,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    const SAMPLE: &str = r#"{"event":{"topic":"permission.pending","seq":3,"payload":{"runId":"run_1","taskId":42,"chatId":7,"requestId":"req_9","summary":"Run `cargo test` in task 42?","options":[{"id":"allow","name":"Allow","kind":"allow"}]}}}"#;

    #[test]
    fn permission_resolved_payload_decodes() {
        let payload = serde_json::json!({"runId":"run_1","taskId":42,"chatId":7,"requestId":"req_9","optionId":"allow","reason":"human"});
        let p = permission_resolved(&payload).unwrap();
        assert_eq!((p.run_id.as_str(), p.task_id, p.request_id.as_str()), ("run_1", 42, "req_9"));
        assert!(permission_resolved(&serde_json::json!({"runId":"r"})).is_none());
    }

    #[test]
    fn parses_event_notification() {
        let msg = parse_server_message(SAMPLE).unwrap();
        let ev = msg.event.expect("event present");
        assert_eq!(ev.topic, "permission.pending");
        assert_eq!(ev.seq, 3);
        let p = permission_pending(&ev.payload).unwrap();
        assert_eq!(p.summary, "Run `cargo test` in task 42?");
        assert_eq!(p.request_id, "req_9");
        assert_eq!(p.task_id, 42);
    }

    #[test]
    fn notification_from_pending() {
        let msg = parse_server_message(SAMPLE).unwrap();
        let n = notification_for(msg.event.as_ref().unwrap()).unwrap();
        assert_eq!(n.kind, NotifyKind::Permission);
        assert_eq!(n.title, "sMind: permission needed");
        assert_eq!(n.body, "A run needs your approval");
        assert_eq!(n.task_id, 42);
        assert_eq!(n.chat_id, 7);
        assert_eq!(n.request_id, "req_9");
    }

    #[test]
    fn permission_notification_has_a_fixed_body() {
        let msg = parse_server_message(
            r#"{"event":{"topic":"permission.pending","seq":3,"payload":{"runId":"run_1","taskId":42,"chatId":7,"requestId":"req_9","summary":"Run `rm -rf x`?"}}}"#,
        )
        .unwrap();
        let n = notification_for(msg.event.as_ref().unwrap()).unwrap();
        assert_eq!(n.title, "sMind: permission needed");
        assert_eq!(n.body, "A run needs your approval");
        // "rm" appears inside the fixed word "permission"; the body is what
        // must never carry the command.
        assert!(!n.body.contains("rm"), "body must not carry the command");
    }

    #[test]
    fn question_pending_becomes_a_fixed_body_notification() {
        let msg = parse_server_message(
            r#"{"event":{"topic":"question.pending","seq":5,"payload":{"runId":"run_2","taskId":43,"chatId":8,"requestId":"req_10","request":{"question":"secret question","schema":null}}}}"#,
        )
        .unwrap();
        let n = notification_for(msg.event.as_ref().unwrap()).unwrap();
        assert_eq!(n.kind, NotifyKind::Question);
        assert_eq!(n.title, "sMind: a question for you");
        assert_eq!(n.body, "An agent has a question");
        assert!(!n.body.contains("secret"), "body must not carry the question text");
        assert!(!n.title.contains("secret"));
        assert_eq!(n.task_id, 43);
        assert_eq!(n.chat_id, 8);
        assert_eq!(n.request_id, "req_10");
    }

    #[test]
    fn failed_run_notification_has_a_fixed_body() {
        let msg = parse_server_message(
            r#"{"event":{"topic":"run.status","seq":7,"payload":{"runId":"run_err","taskId":9,"chatId":4,"status":"error","err":"token sk-123 leaked"}}}"#,
        )
        .unwrap();
        let n = notification_for(msg.event.as_ref().unwrap()).unwrap();
        assert_eq!(n.kind, NotifyKind::Failure);
        assert_eq!(n.title, "sMind: run failed");
        assert_eq!(n.body, "Run failed");
        assert!(!n.body.contains("sk-"), "body must not carry the error text");
        assert!(!n.title.contains("sk-"));
        assert_eq!(n.task_id, 9);
        assert_eq!(n.chat_id, 4);
        assert_eq!(n.request_id, "run_err", "Failure's request_id carries the run id");
    }

    #[test]
    fn no_notification_body_contains_event_text() {
        let marker = "MARKER-qz7w3";
        let cases = [
            (
                "permission.pending",
                serde_json::json!({"runId":marker,"taskId":1,"chatId":1,"requestId":"r","summary":marker,"options":[{"id":marker}]}),
                NotifyKind::Permission,
            ),
            (
                "question.pending",
                serde_json::json!({"runId":marker,"taskId":1,"chatId":1,"requestId":"r","request":{"question":marker}}),
                NotifyKind::Question,
            ),
            (
                "run.status",
                serde_json::json!({"runId":"r","taskId":1,"chatId":1,"status":"error","err":marker,"stopReason":marker,"failureKind":marker}),
                NotifyKind::Failure,
            ),
        ];
        for (topic, payload, kind) in cases {
            let ev = EventNotification { topic: topic.to_string(), seq: 1, payload };
            let n = notification_for(&ev).unwrap_or_else(|| panic!("{topic} became a notification"));
            assert_eq!(n.kind, kind, "{topic}");
            assert!(!n.title.contains(marker), "{topic}: title leaked event text");
            assert!(!n.body.contains(marker), "{topic}: body leaked event text");
        }
    }

    #[test]
    fn done_and_needs_continue_are_silent() {
        for status in ["done", "running", "stopped", "needs-continue"] {
            let msg = parse_server_message(&format!(
                r#"{{"event":{{"topic":"run.status","seq":1,"payload":{{"runId":"r","taskId":5,"status":"{status}"}}}}}}"#
            ))
            .unwrap();
            assert!(
                notification_for(msg.event.as_ref().unwrap()).is_none(),
                "status {status} must stay silent"
            );
        }
    }

    #[test]
    fn notification_without_chat_id_defaults_to_zero() {
        let msg = parse_server_message(
            r#"{"event":{"topic":"permission.pending","seq":4,"payload":{"runId":"run_1","taskId":42,"requestId":"req_9","summary":"s"}}}"#,
        )
        .unwrap();
        let n = notification_for(msg.event.as_ref().unwrap()).unwrap();
        assert_eq!(n.chat_id, 0);
    }

    #[test]
    fn task_get_message_shape() {
        let id = task_get_request_id(42);
        assert_eq!(id, "task-get-42");
        let msg = task_get_message(&id, 42);
        assert_eq!(msg["method"], "task.get");
        assert_eq!(msg["params"]["id"], 42);
        assert_eq!(msg["id"], "task-get-42");
    }

    #[test]
    fn task_get_response_round_trip() {
        assert_eq!(task_get_response_task_id("task-get-42"), Some(42));
        assert_eq!(task_get_response_task_id("events-sub-1"), None);
        assert_eq!(task_get_response_task_id("task-get-not-a-number"), None);

        let result = serde_json::json!({"ID": 42, "WorkspaceID": 7, "Title": "t", "Status": "running"});
        assert_eq!(task_get_workspace_id(&result), Some(7));
        assert_eq!(task_get_workspace_id(&serde_json::json!({"ID": 42})), None);
    }

    #[test]
    fn skips_malformed() {
        assert!(parse_server_message("not json").is_none());
        assert!(parse_server_message("42").is_none()); // not a JSON object
    }

    #[test]
    fn response_envelope_has_no_event() {
        let msg = parse_server_message(r#"{"id":"1","result":{"topics":["permission.pending"]}}"#).unwrap();
        assert_eq!(msg.id.as_deref(), Some("1"));
        assert!(msg.event.is_none());
        assert!(notification_for_through(&msg).is_none());
    }

    #[test]
    fn run_status_decodes_task_id_and_status() {
        let msg = parse_server_message(
            r#"{"event":{"topic":"run.status","seq":1,"payload":{"runId":"r1","taskId":42,"status":"running"}}}"#,
        )
        .unwrap();
        let rs = run_status(&msg.event.unwrap().payload).unwrap();
        assert_eq!(rs.task_id, 42);
        assert_eq!(rs.status, "running");
    }

    #[test]
    fn other_topics_yield_no_notification() {
        let msg = parse_server_message(
            r#"{"event":{"topic":"task.status","seq":1,"payload":{"taskId":42,"status":"running"}}}"#,
        )
        .unwrap();
        assert!(notification_for(msg.event.as_ref().unwrap()).is_none());
    }

    #[test]
    fn missing_fields_in_payload_is_none() {
        let msg = parse_server_message(
            r#"{"event":{"topic":"permission.pending","seq":2,"payload":{"runId":"r"}}}"#,
        )
        .unwrap();
        assert!(notification_for(msg.event.as_ref().unwrap()).is_none());
    }

    fn notification_for_through(msg: &ServerMessage) -> Option<Notification> {
        msg.event.as_ref().and_then(notification_for)
    }
}

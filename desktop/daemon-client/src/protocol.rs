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
/// {runId, taskId, requestId, summary, options}.
#[derive(Debug, Deserialize)]
pub struct PermissionPending {
    #[serde(rename = "runId")]
    pub run_id: String,
    #[serde(rename = "taskId")]
    pub task_id: i64,
    #[serde(rename = "requestId")]
    pub request_id: String,
    pub summary: String,
    #[serde(default)]
    pub options: Vec<serde_json::Value>,
}

/// RunStatus is the payload of run.status events:
/// {runId, taskId, status, stopReason, err} -- only the fields this
/// client needs (see internal/wsapi/events.go's runStatusPayload).
#[derive(Debug, Deserialize)]
pub struct RunStatus {
    #[serde(rename = "taskId")]
    pub task_id: i64,
    pub status: String,
}

/// run_status decodes a run.status event's payload.
pub fn run_status(payload: &serde_json::Value) -> Option<RunStatus> {
    serde_json::from_value(payload.clone()).ok()
}

/// Notification is the OS-notification-ready form of an event. task_id
/// carries the source task so the click handler can look up its
/// workspaceId (see `crate::cache::WorkspaceCache`) and build a route;
/// request_id feeds the tray's attention count (`crate::attention`),
/// which dedupes by it.
#[derive(Debug, PartialEq, Eq)]
pub struct Notification {
    pub title: String,
    pub body: String,
    pub task_id: i64,
    pub request_id: String,
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

/// notification_for turns a permission.pending payload into an OS
/// notification's title/body. Returns None for other topics.
pub fn notification_for(event: &EventNotification) -> Option<Notification> {
    match event.topic.as_str() {
        "permission.pending" => {
            let p = permission_pending(&event.payload)?;
            Some(Notification {
                title: "smind: permission needed".to_string(),
                body: p.summary,
                task_id: p.task_id,
                request_id: p.request_id,
            })
        }
        _ => None,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    const SAMPLE: &str = r#"{"event":{"topic":"permission.pending","seq":3,"payload":{"runId":"run_1","taskId":42,"requestId":"req_9","summary":"Run `cargo test` in task 42?","options":[{"id":"allow","name":"Allow","kind":"allow"}]}}}"#;

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
        assert_eq!(n.title, "smind: permission needed");
        assert_eq!(n.body, "Run `cargo test` in task 42?");
        assert_eq!(n.task_id, 42);
        assert_eq!(n.request_id, "req_9");
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

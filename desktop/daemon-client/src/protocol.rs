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

/// Notification is the OS-notification-ready form of an event.
#[derive(Debug, PartialEq, Eq)]
pub struct Notification {
    pub title: String,
    pub body: String,
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

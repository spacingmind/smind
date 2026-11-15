//! Task-route URL building, matching
//! `web/packages/ui/src/lib/route.ts`'s
//! `#/workspace/<id>/task/<id>/<kind>` hash shape. Pure string logic, so
//! it's unit-tested without a webview.

use url::Url;

/// The tab kind a notification/tray/deep-link click navigates to when no
/// chat is known -- the base "files" tab, one of `route.ts`'s own base
/// kinds (a tab kind like the removed "task" would make the web
/// `parseRoute` reject the route, leaving the click focus-only).
pub const DEFAULT_KIND: &str = "files";

/// task_route_hash builds the `#/workspace/<ws>/task/<id>/<kind>` fragment.
pub fn task_route_hash(workspace_id: i64, task_id: i64, kind: &str) -> String {
    format!("#/workspace/{workspace_id}/task/{task_id}/{kind}")
}

/// task_route_url builds the full `<daemonUrl>/#/workspace/.../task/.../<kind>`
/// URL the webview should navigate to.
pub fn task_route_url(daemon_url: &Url, workspace_id: i64, task_id: i64, kind: &str) -> String {
    let mut base = daemon_url.as_str().trim_end_matches('/').to_string();
    base.push('/');
    base.push_str(&task_route_hash(workspace_id, task_id, kind));
    base
}

/// card_route_hash builds the `#/workspace/<ws>/task/<id>/chat/<chatId>`
/// fragment, with `?card=<requestId>` when a request id is given -- the
/// exact card a pending permission/question lives at.
pub fn card_route_hash(workspace_id: i64, task_id: i64, chat_id: i64, request_id: Option<&str>) -> String {
    let mut hash = format!("#/workspace/{workspace_id}/task/{task_id}/chat/{chat_id}");
    if let Some(id) = request_id.filter(|id| !id.is_empty()) {
        hash.push_str("?card=");
        hash.extend(url::form_urlencoded::byte_serialize(id.as_bytes()));
    }
    hash
}

/// card_route_url builds the full `<daemonUrl>/#/workspace/.../chat/...`
/// URL the webview should navigate to, joining like task_route_url.
pub fn card_route_url(daemon_url: &Url, workspace_id: i64, task_id: i64, chat_id: i64, request_id: Option<&str>) -> String {
    let mut base = daemon_url.as_str().trim_end_matches('/').to_string();
    base.push('/');
    base.push_str(&card_route_hash(workspace_id, task_id, chat_id, request_id));
    base
}

/// A parsed `smind://` deep link (quick-wins AC6).
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum DeepLink {
    Task { task_id: i64 },
    WorkspaceTask { workspace_id: i64, task_id: i64 },
}

/// parse_deep_link accepts exactly `smind://task/<taskId>` and
/// `smind://workspace/<wsId>/task/<taskId>`; anything else (wrong
/// scheme, non-numeric ids, extra path segments, a query string) is
/// rejected -- a deep link can only ever choose a route, per AC6, so a
/// malformed one is simply refused rather than leniently reinterpreted.
pub fn parse_deep_link(raw: &str) -> Option<DeepLink> {
    let url = Url::parse(raw).ok()?;
    if url.scheme() != "smind" {
        return None;
    }
    if url.query().is_some() {
        return None;
    }
    let host = url.host_str()?;
    let segments: Vec<&str> = url.path_segments()?.filter(|s| !s.is_empty()).collect();
    match (host, segments.as_slice()) {
        ("task", [task_id_raw]) => Some(DeepLink::Task { task_id: task_id_raw.parse().ok()? }),
        ("workspace", [workspace_id_raw, "task", task_id_raw]) => Some(DeepLink::WorkspaceTask {
            workspace_id: workspace_id_raw.parse().ok()?,
            task_id: task_id_raw.parse().ok()?,
        }),
        _ => None,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn hash_shape_matches_route_ts() {
        assert_eq!(task_route_hash(3, 42, "files"), "#/workspace/3/task/42/files");
        assert_eq!(task_route_hash(3, 42, "diff"), "#/workspace/3/task/42/diff");
    }

    #[test]
    fn url_joins_daemon_base() {
        let base = Url::parse("http://127.0.0.1:4648").unwrap();
        assert_eq!(
            task_route_url(&base, 3, 42, "files"),
            "http://127.0.0.1:4648/#/workspace/3/task/42/files"
        );
    }

    #[test]
    fn url_joins_with_trailing_slash_base() {
        let base = Url::parse("http://127.0.0.1:4648/").unwrap();
        assert_eq!(
            task_route_url(&base, 3, 42, "files"),
            "http://127.0.0.1:4648/#/workspace/3/task/42/files"
        );
    }

    #[test]
    fn chat_route_url_with_card() {
        let base = Url::parse("http://127.0.0.1:4648").unwrap();
        assert_eq!(
            card_route_url(&base, 3, 42, 7, Some("req_9")),
            "http://127.0.0.1:4648/#/workspace/3/task/42/chat/7?card=req_9"
        );
    }

    #[test]
    fn chat_route_without_request_has_no_query() {
        let base = Url::parse("http://127.0.0.1:4648").unwrap();
        assert_eq!(
            card_route_url(&base, 3, 42, 7, None),
            "http://127.0.0.1:4648/#/workspace/3/task/42/chat/7"
        );
        assert_eq!(
            card_route_url(&base, 3, 42, 7, Some("")),
            "http://127.0.0.1:4648/#/workspace/3/task/42/chat/7"
        );
    }

    #[test]
    fn request_id_is_percent_encoded() {
        assert_eq!(
            card_route_hash(3, 42, 7, Some("a b&c")),
            "#/workspace/3/task/42/chat/7?card=a+b%26c"
        );
    }

    #[test]
    fn fallback_route_has_a_kind_the_web_accepts() {
        let base = Url::parse("http://127.0.0.1:4648").unwrap();
        let url = task_route_url(&base, 3, 42, DEFAULT_KIND);
        assert!(url.ends_with("/files"), "{url}");
        assert!(["files", "diff", "terminal", "pr"].contains(&DEFAULT_KIND));
    }

    #[test]
    fn deep_link_task_form() {
        assert_eq!(parse_deep_link("smind://task/42"), Some(DeepLink::Task { task_id: 42 }));
    }

    #[test]
    fn deep_link_workspace_task_form() {
        assert_eq!(
            parse_deep_link("smind://workspace/3/task/42"),
            Some(DeepLink::WorkspaceTask { workspace_id: 3, task_id: 42 })
        );
    }

    #[test]
    fn deep_link_rejects_wrong_scheme() {
        assert_eq!(parse_deep_link("http://task/42"), None);
        assert_eq!(parse_deep_link("smind2://task/42"), None);
    }

    #[test]
    fn deep_link_rejects_non_numeric_ids() {
        assert_eq!(parse_deep_link("smind://task/abc"), None);
        assert_eq!(parse_deep_link("smind://workspace/abc/task/42"), None);
        assert_eq!(parse_deep_link("smind://workspace/3/task/abc"), None);
    }

    #[test]
    fn deep_link_rejects_extra_path() {
        assert_eq!(parse_deep_link("smind://task/42/extra"), None);
        assert_eq!(parse_deep_link("smind://workspace/3/task/42/extra"), None);
        assert_eq!(parse_deep_link("smind://workspace/3"), None);
    }

    #[test]
    fn deep_link_rejects_query_junk() {
        assert_eq!(parse_deep_link("smind://task/42?foo=bar"), None);
        assert_eq!(parse_deep_link("smind://workspace/3/task/42?x=y"), None);
    }

    #[test]
    fn deep_link_rejects_malformed() {
        assert_eq!(parse_deep_link("not a url"), None);
        assert_eq!(parse_deep_link("smind:task/42"), None); // no authority
        assert_eq!(parse_deep_link("smind://unknown/1"), None);
    }
}

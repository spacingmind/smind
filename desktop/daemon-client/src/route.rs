//! Task-route URL building, matching
//! `web/packages/ui/src/lib/route.ts`'s
//! `#/workspace/<id>/task/<id>/<kind>` hash shape. Pure string logic, so
//! it's unit-tested without a webview.

use url::Url;

/// The tab kind a notification/tray/deep-link click navigates to -- the
/// base "task" tab, matching `route.ts`'s own base kinds.
pub const DEFAULT_KIND: &str = "task";

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
        assert_eq!(task_route_hash(3, 42, "task"), "#/workspace/3/task/42/task");
        assert_eq!(task_route_hash(3, 42, "files"), "#/workspace/3/task/42/files");
    }

    #[test]
    fn url_joins_daemon_base() {
        let base = Url::parse("http://127.0.0.1:4648").unwrap();
        assert_eq!(
            task_route_url(&base, 3, 42, "task"),
            "http://127.0.0.1:4648/#/workspace/3/task/42/task"
        );
    }

    #[test]
    fn url_joins_with_trailing_slash_base() {
        let base = Url::parse("http://127.0.0.1:4648/").unwrap();
        assert_eq!(
            task_route_url(&base, 3, 42, "task"),
            "http://127.0.0.1:4648/#/workspace/3/task/42/task"
        );
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

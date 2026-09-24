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
}

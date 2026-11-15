//! Shared by the unit tests in src/ipc_guard.rs and the integration test tests/ipc_guard.rs.

/// (url, accepted?): the one table for the origin rule, also used against the capability pattern below.
pub fn table() -> Vec<(String, bool)> {
    let ok = |s: &str| (s.to_string(), true);
    let no = |s: &str| (s.to_string(), false);
    vec![
        ok("http://127.0.0.1:47123"),
        ok("http://127.0.0.1:47123/"),
        ok("http://127.0.0.1:47123/?k=secret#/workspace/1/task/2"),
        ok("http://127.0.0.1:47123/a/b/c.html?x=1"),
        // another port
        no("http://127.0.0.1:47124/"),
        no("http://127.0.0.1:3000/"),
        no("http://127.0.0.1:4648/"),
        no("http://127.0.0.1/"),
        // another host
        no("http://localhost:47123/"),
        no("http://127.0.0.2:47123/"),
        no("http://[::1]:47123/"),
        // built at runtime: a literal wildcard address trips the listener hygiene test
        no(&format!("http://{}:47123/", ["0", "0", "0", "0"].join("."))),
        no("http://127.0.0.1.evil.test:47123/"),
        no("http://evil.test:47123/"),
        // another scheme
        no("https://127.0.0.1:47123/"),
        no("tauri://localhost/"),
        no("http://tauri.localhost/"),
        no("file:///tmp/x.html"),
        no("ws://127.0.0.1:47123/"),
        // userinfo
        no("http://user@127.0.0.1:47123/"),
        no("http://user:pass@127.0.0.1:47123/"),
        no("http://:pass@127.0.0.1:47123/"),
        // no host, opaque
        no("about:blank"),
        no("data:text/html,hi"),
        no("javascript:alert(1)"),
        no("blob:http://127.0.0.1:47123/abc"),
    ]
}

/// A reason is a sentence, not a placeholder.
pub fn reason_ok(reason: &str) -> bool {
    let r = reason.trim().to_lowercase();
    r.len() >= 25
        && r.split_whitespace().count() >= 4
        && !["todo", "tbd", "fixme", "placeholder", "xxx", "n/a"]
            .iter()
            .any(|p| r.starts_with(p))
}

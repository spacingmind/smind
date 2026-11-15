//! DT5 B/AC8: the consent-flow test banner. The Settings UI calls this
//! exactly when the person turns desktop notifications on: sending the
//! first notification is what makes macOS ask for its own permission,
//! so it deliberately does not check the consent setting itself.

use tauri::{AppHandle, State};

use crate::notify::{self, ClickContext, Target};
use crate::state::DesktopState;

#[tauri::command]
pub fn notify_test(app: AppHandle, state: State<'_, DesktopState>) -> Result<(), String> {
    let ctx = ClickContext {
        app: app.clone(),
        cache: state.cache.clone(),
        proxy_url: state.proxy_url.clone(),
    };
    notify::show(
        ctx,
        "sMind: notifications are on".into(),
        "You will see a banner like this when an agent needs you.".into(),
        Target::task(0),
    );
    Ok(())
}

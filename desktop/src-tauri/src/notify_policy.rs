//! DT5 B/AC8: whether a notification kind may show its banner. Consent
//! (`notifications_on`) gates everything; a kind's own switch then gates
//! that kind. A kind that is off -- or no consent yet -- shows nothing
//! but still counts toward the badge: counting is the watcher's job
//! (`client_watch`), this is only the show decision.

use smind_daemon_client::NotifyKind;

use crate::app_settings::{AppSettings, AppSettingsPatch};

pub fn should_show(settings: &AppSettings, kind: NotifyKind) -> bool {
    settings.notifications_on == Some(true)
        && match kind {
            NotifyKind::Permission => settings.notify_permission,
            NotifyKind::Question => settings.notify_question,
            NotifyKind::Failure => settings.notify_failure,
        }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn consented() -> AppSettings {
        AppSettings::FIRST_RUN
            .apply(AppSettingsPatch { notifications_on: Some(true), ..Default::default() })
    }

    #[test]
    fn nothing_shows_before_consent() {
        let not_asked = AppSettings::FIRST_RUN;
        let refused = AppSettings::FIRST_RUN
            .apply(AppSettingsPatch { notifications_on: Some(false), ..Default::default() });
        for kind in [NotifyKind::Permission, NotifyKind::Question, NotifyKind::Failure] {
            assert!(!should_show(&not_asked, kind), "{kind:?}: not asked shows nothing");
            assert!(!should_show(&refused, kind), "{kind:?}: a refusal shows nothing");
        }
    }

    #[test]
    fn an_off_kind_is_silent_but_counts() {
        let off = consented()
            .apply(AppSettingsPatch { notify_question: Some(false), ..Default::default() });
        assert!(!should_show(&off, NotifyKind::Question));
        assert!(should_show(&off, NotifyKind::Permission));
        assert!(should_show(&off, NotifyKind::Failure));
    }

    #[test]
    fn a_kind_on_with_consent_shows() {
        let s = consented();
        assert!(should_show(&s, NotifyKind::Permission));
        assert!(should_show(&s, NotifyKind::Question));
        assert!(should_show(&s, NotifyKind::Failure));
    }
}

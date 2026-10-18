//! The daemon password a relay device presents in its `device_hello`
//! (ADR-0031 P13c). It is typed by the user, handed to Rust once, and lives
//! here, in this process's memory, until the app exits: never written to disk
//! or a log, never returned to the webview, never part of a Debug/Display
//! rendering. A rejected password is forgotten at once.

use std::fmt;
use std::sync::{Arc, Mutex};

use tokio::sync::Notify;
use zeroize::Zeroizing;

/// The longest password the daemon will even look at (it answers
/// `incorrect_password` for more); refusing here keeps a pasted novel out of
/// the hello.
pub const MAX_PASSWORD_BYTES: usize = 128;

#[derive(Default)]
struct Inner {
    value: Mutex<Option<Zeroizing<String>>>,
    changed: Notify,
}

/// A shared, in-memory password slot. Cloning shares the slot: the relay loop
/// reads it per hello, the Tauri command sets it.
#[derive(Clone, Default)]
pub struct PasswordCell {
    inner: Arc<Inner>,
}

impl PasswordCell {
    /// Stores `password` (replacing any earlier one) and wakes a loop that is
    /// waiting for one. An empty password clears the slot.
    pub fn set(&self, password: String) {
        let password = Zeroizing::new(password);
        let mut slot = self.inner.value.lock().unwrap();
        *slot = if password.is_empty() { None } else { Some(password) };
        drop(slot);
        self.inner.changed.notify_waiters();
        // A permit too, so a set that lands just before the loop starts waiting
        // is not lost.
        self.inner.changed.notify_one();
    }

    /// A copy for one hello. The copy zeroizes itself when dropped.
    pub fn get(&self) -> Option<Zeroizing<String>> {
        self.inner.value.lock().unwrap().clone()
    }

    /// Forgets the password (it was rejected, or the connection was removed).
    pub fn clear(&self) {
        *self.inner.value.lock().unwrap() = None;
    }

    /// Resolves after the next `set`.
    pub async fn changed(&self) {
        self.inner.changed.notified().await;
    }

    /// Whether a password is held. (Never its value.)
    pub fn is_set(&self) -> bool {
        self.inner.value.lock().unwrap().is_some()
    }
}

impl fmt::Debug for PasswordCell {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "PasswordCell({})", if self.is_set() { "set, redacted" } else { "empty" })
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn debug_never_shows_the_password() {
        let cell = PasswordCell::default();
        cell.set("correct horse battery".into());
        let shown = format!("{cell:?}");
        assert!(!shown.contains("correct"), "{shown}");
        assert_eq!(shown, "PasswordCell(set, redacted)");
    }

    #[test]
    fn set_get_clear_and_empty_clears() {
        let cell = PasswordCell::default();
        assert!(cell.get().is_none());
        cell.set("hunter2hunter2".into());
        assert_eq!(cell.get().unwrap().as_str(), "hunter2hunter2");
        cell.set(String::new());
        assert!(cell.get().is_none(), "an empty password clears the slot");
        cell.set("again-again-again".into());
        cell.clear();
        assert!(!cell.is_set());
    }

    #[tokio::test]
    async fn changed_wakes_on_set_even_if_set_came_first() {
        let cell = PasswordCell::default();
        cell.set("a-password-123".into());
        tokio::time::timeout(std::time::Duration::from_secs(1), cell.changed())
            .await
            .expect("a set before the wait must not be lost");
        let waiter = {
            let cell = cell.clone();
            tokio::spawn(async move { cell.changed().await })
        };
        tokio::time::sleep(std::time::Duration::from_millis(20)).await;
        cell.set("another-pass-123".into());
        tokio::time::timeout(std::time::Duration::from_secs(1), waiter)
            .await
            .expect("woken")
            .unwrap();
    }
}

//! The daemon password a relay device presents in its `device_hello`
//! (ADR-0031 P13c). It is typed by the user, handed to Rust once, and lives
//! here, in this process's memory, until the app exits: never written to disk
//! or a log, never returned to the webview, never part of a Debug/Display
//! rendering. A rejected password is forgotten at once. (The copy held here
//! is zeroized when it is replaced or dropped; short-lived copies in the IPC
//! argument and the request buffers are ordinary memory, not wiped.)

use std::fmt;
use std::sync::atomic::{AtomicU64, Ordering};
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
    /// Bumped by every `set`: lets a caller forget exactly the password it sent
    /// without erasing a newer one the user entered while the reply was in flight.
    generation: AtomicU64,
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
        self.inner.generation.fetch_add(1, Ordering::SeqCst);
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

    /// A copy and the generation it belongs to (see `clear_if`).
    pub fn snapshot(&self) -> (Option<Zeroizing<String>>, u64) {
        let slot = self.inner.value.lock().unwrap();
        (slot.clone(), self.inner.generation.load(Ordering::SeqCst))
    }

    /// Forgets the password only if it is still the one `snapshot` returned for
    /// `generation`: a daemon's "incorrect" answer applies to what was sent, not
    /// to whatever the user typed since.
    pub fn clear_if(&self, generation: u64) {
        let mut slot = self.inner.value.lock().unwrap();
        if self.inner.generation.load(Ordering::SeqCst) == generation {
            *slot = None;
        }
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
    fn a_rejected_password_is_forgotten_but_a_newer_one_is_not() {
        let cell = PasswordCell::default();
        cell.set("first-password-123".into());
        let (sent, gen) = cell.snapshot();
        assert_eq!(sent.unwrap().as_str(), "first-password-123");
        // The user types another while the daemon is still answering the first.
        cell.set("second-password-456".into());
        cell.clear_if(gen);
        assert_eq!(cell.get().unwrap().as_str(), "second-password-456", "the newer password must survive");
        let (_, gen2) = cell.snapshot();
        cell.clear_if(gen2);
        assert!(cell.get().is_none(), "the rejected password is forgotten");
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

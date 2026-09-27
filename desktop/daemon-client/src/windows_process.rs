//! Every child process this app spawns is a console-subsystem tool
//! (`wsl.exe`, `lsof`, `kill`, `ps`, `tar`, the managed daemon binary
//! itself...). On Windows, a GUI app spawning one of these without
//! `CREATE_NO_WINDOW` gets a console window flashed on screen for the
//! split second it runs -- `daemon_status` alone fires several of these
//! per call, which is what made Settings -> Daemon flicker. Route every
//! `std::process::Command` through `no_window` before `.spawn()`/
//! `.status()`/`.output()` so none of them ever gets a window, on Windows
//! or off it (a no-op there).

use std::process::Command;

#[cfg(windows)]
const CREATE_NO_WINDOW: u32 = 0x0800_0000;

/// no_window sets `CREATE_NO_WINDOW` on the given command under
/// `cfg(windows)`; a no-op everywhere else, so call sites don't need
/// their own `#[cfg(windows)]`.
pub fn no_window(cmd: &mut Command) -> &mut Command {
    #[cfg(windows)]
    {
        use std::os::windows::process::CommandExt;
        cmd.creation_flags(CREATE_NO_WINDOW);
    }
    cmd
}

#[cfg(test)]
mod tests {
    use super::*;

    #[cfg(windows)]
    #[test]
    fn no_window_sets_create_no_window_flag() {
        // There's no public getter for creation flags on `Command`, so this
        // asserts the one thing we can observe from outside: the flagged
        // command still runs and completes normally (a wrong flag value,
        // e.g. one that collided with a reserved bit, would fail to spawn).
        let mut cmd = Command::new("cmd");
        cmd.args(["/C", "exit 0"]);
        no_window(&mut cmd);
        let status = cmd.status().expect("smind desktop: flagged command must still spawn");
        assert!(status.success());
    }

    #[cfg(not(windows))]
    #[test]
    fn no_window_is_a_no_op_off_windows() {
        let mut cmd = Command::new("true");
        no_window(&mut cmd);
        let status = cmd.status().expect("smind desktop: `true` must exist");
        assert!(status.success());
    }
}

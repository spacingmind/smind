import { notifyManager } from "@tanstack/react-query";

// React Query batches observer updates with setTimeout(0), which a fake-timer test never fires and a
// microtask-only `await flush()` never reaches; a microtask keeps that flush enough to see a settled
// request. Test-only: import it (render-with-daemon does) in a test that mounts query hooks.
notifyManager.setScheduler((callback) => queueMicrotask(callback));

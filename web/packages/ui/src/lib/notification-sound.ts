/**
 * Plays a short, synthesized notification chime via the Web Audio API --
 * a bundled/generated sound rather than a network asset, per the plan's
 * Decisions ("Sound: ... Use a short bundled sound or the Web Audio API,
 * not a network asset").
 *
 * Best-effort and silent on failure: a browser without AudioContext, one
 * that refuses to run audio outside a user gesture, or any other runtime
 * quirk should never turn a missed chime into a thrown error -- the same
 * "never throws" contract use-attention-notifications.ts already holds
 * itself to for the Notification it accompanies.
 */
export function playNotificationSound(): void {
  if (typeof window === "undefined") return;
  const AudioContextCtor =
    window.AudioContext ?? (window as unknown as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext;
  if (!AudioContextCtor) return;

  try {
    const ctx = new AudioContextCtor();
    const oscillator = ctx.createOscillator();
    const gain = ctx.createGain();
    oscillator.type = "sine";
    oscillator.frequency.value = 880;
    gain.gain.setValueAtTime(0.15, ctx.currentTime);
    gain.gain.exponentialRampToValueAtTime(0.0001, ctx.currentTime + 0.2);
    oscillator.connect(gain);
    gain.connect(ctx.destination);
    oscillator.start();
    oscillator.stop(ctx.currentTime + 0.2);
    oscillator.onended = () => {
      void ctx.close();
    };
  } catch {
    // Best-effort only -- see this file's doc comment.
  }
}

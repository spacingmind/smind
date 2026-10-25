import { formatDelta, formatMetric, METRIC_LABELS } from "@/lib/probe-format";
import type { ProbeMetricRow } from "@/lib/probe-types";
import { cn } from "@/lib/utils";

/**
 * Per-metric contrast of the arms. The replicate and noise columns exist only when the probe had a
 * noise floor (a quick probe has two arms); a `worse` row (the skill arm worse than control by more
 * than the noise) is marked with a word, not just a tint.
 */
export function ProbeContrastTable({ metrics, noiseFloor }: { metrics: ProbeMetricRow[]; noiseFloor: boolean }) {
  const head = "whitespace-nowrap px-2 py-1.5 text-right text-ui-sm font-normal text-foreground-subtle";
  const cell = "whitespace-nowrap px-2 py-1.5 text-right text-ui-sm tabular-nums text-foreground-muted";
  return (
    <div className="overflow-x-auto rounded-lg border border-border">
      <table data-testid="probe-contrast" className="w-full border-collapse">
        <caption className="sr-only">Contrast between the control arm and the arm with this skill</caption>
        <thead>
          <tr className="border-b border-border">
            <th scope="col" className={cn(head, "text-left")}>
              Metric
            </th>
            <th scope="col" className={head}>
              Control
            </th>
            {noiseFloor && (
              <th scope="col" className={head} data-testid="probe-col-replicate">
                Replicate
              </th>
            )}
            <th scope="col" className={head}>
              With this skill
            </th>
            <th scope="col" className={head}>
              Δ
            </th>
            {noiseFloor && (
              <th scope="col" className={head} data-testid="probe-col-noise">
                Noise
              </th>
            )}
          </tr>
        </thead>
        <tbody>
          {metrics.map((m) => (
            <tr key={m.key} data-testid="probe-metric" data-metric={m.key} data-worse={m.worse} className="border-b border-border last:border-b-0">
              <th scope="row" className={cn(cell, "text-left font-normal text-foreground")}>
                {METRIC_LABELS[m.key]}
              </th>
              <td className={cell}>{formatMetric(m.key, m.a)}</td>
              {noiseFloor && <td className={cell}>{m.a2 === null ? "–" : formatMetric(m.key, m.a2)}</td>}
              <td className={cell}>{formatMetric(m.key, m.b)}</td>
              <td className={cn(cell, m.worse && "text-warning")}>
                {formatDelta(m.key, m.deltaAB)}
                {m.worse && (
                  <span data-testid="probe-worse" className="ml-1.5 text-ui-xs font-medium">
                    worse
                  </span>
                )}
              </td>
              {noiseFloor && <td className={cell}>{m.noise === null ? "–" : `±${formatMetric(m.key, m.noise)}`}</td>}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

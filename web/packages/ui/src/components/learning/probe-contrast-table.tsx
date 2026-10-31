import { formatDelta, formatMetric, METRIC_LABELS } from "@/lib/probe-format";
import type { ProbeMetricRow } from "@/lib/probe-types";
import { cn } from "@/lib/utils";

function WorseMark() {
  return (
    <span data-testid="probe-worse" className="ml-1.5 text-ui-xs font-medium">
      worse
    </span>
  );
}

/** Below `sm` a five-column table would scroll sideways and hide Δ and Noise: one block per metric instead. */
function StackedContrast({ metrics, noiseFloor }: { metrics: ProbeMetricRow[]; noiseFloor: boolean }) {
  return (
    <ul data-testid="probe-contrast-stacked" aria-label="Contrast between the control arm and the arm with this skill" className="flex flex-col rounded-lg border border-border sm:hidden">
      {metrics.map((m) => (
        <li key={m.key} data-testid="probe-metric-stacked" data-metric={m.key} data-worse={m.worse} className="flex flex-col gap-1 border-b border-border px-3 py-2 last:border-b-0">
          <span className="text-ui-sm text-foreground">{METRIC_LABELS[m.key]}</span>
          <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-0.5 text-ui-sm tabular-nums">
            <dt className="text-foreground-subtle">Control</dt>
            <dd className="text-right text-foreground-muted">{formatMetric(m.key, m.a)}</dd>
            {noiseFloor && (
              <>
                <dt className="text-foreground-subtle">Replicate</dt>
                <dd className="text-right text-foreground-muted">{m.a2 === null ? "–" : formatMetric(m.key, m.a2)}</dd>
              </>
            )}
            <dt className="text-foreground-subtle">With this skill</dt>
            <dd className="text-right text-foreground-muted">{formatMetric(m.key, m.b)}</dd>
            <dt className="text-foreground-subtle">Δ</dt>
            <dd className={cn("text-right text-foreground-muted", m.worse && "text-warning-text")}>
              {formatDelta(m.key, m.deltaAB)}
              {m.worse && <WorseMark />}
            </dd>
            {noiseFloor && (
              <>
                <dt className="text-foreground-subtle">Noise</dt>
                <dd className="text-right text-foreground-muted">{m.noise === null ? "–" : `±${formatMetric(m.key, m.noise)}`}</dd>
              </>
            )}
          </dl>
        </li>
      ))}
    </ul>
  );
}

/**
 * Per-metric contrast of the arms. The replicate and noise columns exist only when the probe had a
 * noise floor (a quick probe has two arms); a `worse` row (the skill arm worse than control by more
 * than the noise) is marked with a word, not just a tint. From `sm` up it is a table; below, a
 * stacked list (see `StackedContrast`).
 */
export function ProbeContrastTable({ metrics, noiseFloor }: { metrics: ProbeMetricRow[]; noiseFloor: boolean }) {
  return (
    <>
      <StackedContrast metrics={metrics} noiseFloor={noiseFloor} />
      <WideContrast metrics={metrics} noiseFloor={noiseFloor} />
    </>
  );
}

function WideContrast({ metrics, noiseFloor }: { metrics: ProbeMetricRow[]; noiseFloor: boolean }) {
  const head = "whitespace-nowrap px-2 py-1.5 text-right text-ui-sm font-normal text-foreground-subtle";
  const cell = "whitespace-nowrap px-2 py-1.5 text-right text-ui-sm tabular-nums text-foreground-muted";
  return (
    <div className="hidden overflow-x-auto rounded-lg border border-border sm:block">
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
              <td className={cn(cell, m.worse && "text-warning-text")}>
                {formatDelta(m.key, m.deltaAB)}
                {m.worse && <WorseMark />}
              </td>
              {noiseFloor && <td className={cell}>{m.noise === null ? "–" : `±${formatMetric(m.key, m.noise)}`}</td>}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

package quota

import "time"

// DisplayReport picks what a usage card shows (ADR-0028 §7.2). It is pure and
// never mutates its inputs; the Report it returns is a copy.
//
//   - A fresh fetched report (available, not stale, younger than ttl when ttl
//     is set) is returned unchanged: an observation never overrides its
//     numbers.
//   - Otherwise, where the observation is newer than the fetched report (or
//     there is no fetched report), the observed windows are shown: they replace
//     fetched windows with the same ID, field-wise: an observed value wins, but
//     a window the observation carries no numbers for (status only) keeps the
//     fetched used/remaining percent and reset. Windows with a new ID are added.
//     With no fetched report the result is the observation's own report.
//   - A fetched report with no newer observation is returned as it is.
//   - ok is false when there is neither.
func DisplayReport(fetched Result, obs *Observation, ttl time.Duration, now time.Time) (Report, bool) {
	rep := fetched.Report
	if rep != nil && rep.Status == StatusAvailable && !fetched.Stale &&
		(ttl <= 0 || now.IsZero() || now.Sub(rep.FetchedAt) < ttl) {
		return cloneReport(*rep), true
	}
	if obs == nil {
		if rep == nil {
			return Report{}, false
		}
		return cloneReport(*rep), true
	}
	if rep == nil {
		return cloneReport(obs.Report()), true
	}
	out := cloneReport(*rep)
	if !obs.At.After(rep.FetchedAt) {
		return out, true
	}
	for _, w := range cloneReport(Report{Windows: obs.Windows}).Windows {
		replaced := false
		for i := range out.Windows {
			if out.Windows[i].ID == w.ID {
				out.Windows[i], replaced = mergeObservedWindow(out.Windows[i], w), true
				break
			}
		}
		if !replaced {
			out.Windows = append(out.Windows, w)
		}
	}
	return out, true
}

// mergeObservedWindow overlays the observed window o on the fetched one f: o
// wins field by field, and f supplies the numbers o does not have.
func mergeObservedWindow(f, o Window) Window {
	if o.UsedPct == nil && o.RemainingPct == nil {
		o.UsedPct, o.RemainingPct = f.UsedPct, f.RemainingPct
		if o.Status != statusRejected && o.Status != statusAllowedWarning {
			o.Tone = f.Tone
		}
	}
	if o.ResetsAt == nil {
		o.ResetsAt = f.ResetsAt
	}
	if o.DurationSeconds == nil {
		o.DurationSeconds = f.DurationSeconds
	}
	return o
}

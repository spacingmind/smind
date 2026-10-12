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
//     fetched windows with the same ID and are added otherwise. With no fetched
//     report the result is the observation's own report.
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
				out.Windows[i], replaced = w, true
				break
			}
		}
		if !replaced {
			out.Windows = append(out.Windows, w)
		}
	}
	return out, true
}

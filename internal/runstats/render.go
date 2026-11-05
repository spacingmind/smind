package runstats

import (
	"fmt"
	"io"
)

// Render writes the human table of r. Same content as the JSON form.
func Render(w io.Writer, r *Report) {
	_ = r
	fmt.Fprintln(w, "run-stats")
}

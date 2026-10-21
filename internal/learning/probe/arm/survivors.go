package arm

import "fmt"

// SurvivorsError is KillTree's error when processes of the arm outlived the
// KILL pass. PIDs names them (never their environment or command line).
type SurvivorsError struct{ PIDs []int }

func (e *SurvivorsError) Error() string {
	return fmt.Sprintf("arm: processes survived kill: %v", e.PIDs)
}

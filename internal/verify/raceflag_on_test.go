//go:build race

package verify_test

// raceEnabled is true when the test binary is built with -race. Wall-clock perf guards are
// meaningless under the race detector (it adds large, variable overhead), so they skip on it.
const raceEnabled = true

//go:build !race

package verify_test

// raceEnabled is false in a normal (non -race) test build, so the wall-clock perf guards run.
const raceEnabled = false

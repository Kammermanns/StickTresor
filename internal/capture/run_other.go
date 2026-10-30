//go:build !windows && !darwin && !linux

package capture

// Collect liefert auf anderen Systemen nur Beispieldaten.
func Collect() (Result, error) { return fakeCollect() }

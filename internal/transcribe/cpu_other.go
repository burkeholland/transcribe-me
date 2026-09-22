//go:build !windows

package transcribe

// Non-Windows builds are only used by developer test machines that never launch
// the shipped native binaries, so the preflight is a no-op.
func detectCPUFeatures() CPUFeatures {
	return CPUFeatures{HasAVX: true, HasAVX2: true, HasFMA3: true, HasF16C: true, HasBMI2: true}
}

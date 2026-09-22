package transcribe

import (
	"errors"
	"strings"
)

// CPUFeatures captures the x86 capabilities the shipped native binary is
// compiled against. All listed capabilities are required at runtime. AVX is
// the OS-enabled AVX-register prerequisite (OSXSAVE plus XCR0 YMM state); the
// remaining flags are CPUID bits for the instruction sets ggml emits.
type CPUFeatures struct {
	HasAVX  bool
	HasAVX2 bool
	HasFMA3 bool
	HasF16C bool
	HasBMI2 bool
}

// validateCPU reports a user-facing error when required capabilities are absent.
// Kept as a pure function so tests exercise the truth table without global stubs.
func validateCPU(f CPUFeatures) error {
	var missing []string
	if !f.HasAVX {
		missing = append(missing, "AVX")
	}
	if !f.HasAVX2 {
		missing = append(missing, "AVX2")
	}
	if !f.HasFMA3 {
		missing = append(missing, "FMA3")
	}
	if !f.HasF16C {
		missing = append(missing, "F16C")
	}
	if !f.HasBMI2 {
		missing = append(missing, "BMI2")
	}
	if len(missing) == 0 {
		return nil
	}
	return errors.New("this CPU is missing " + strings.Join(missing, ", ") + ". Transcribe Me needs a modern x64 processor. See the system requirements.")
}

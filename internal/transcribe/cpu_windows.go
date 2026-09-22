//go:build windows

package transcribe

import "github.com/klauspost/cpuid/v2"

// detectCPUFeatures uses klauspost/cpuid/v2, which populates the CPU singleton
// at init from CPUID plus XGETBV so AVX reflects OS-enabled XSAVE support.
func detectCPUFeatures() CPUFeatures {
	return CPUFeatures{
		HasAVX:  cpuid.CPU.Has(cpuid.AVX),
		HasAVX2: cpuid.CPU.Has(cpuid.AVX2),
		HasFMA3: cpuid.CPU.Has(cpuid.FMA3),
		HasF16C: cpuid.CPU.Has(cpuid.F16C),
		HasBMI2: cpuid.CPU.Has(cpuid.BMI2),
	}
}

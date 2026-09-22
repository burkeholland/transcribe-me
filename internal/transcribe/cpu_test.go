package transcribe

import (
	"strings"
	"testing"
)

func TestValidateCPU(t *testing.T) {
	all := CPUFeatures{HasAVX: true, HasAVX2: true, HasFMA3: true, HasF16C: true, HasBMI2: true}
	if err := validateCPU(all); err != nil {
		t.Fatalf("modern CPU rejected: %v", err)
	}
	if err := validateCPU(CPUFeatures{}); err == nil ||
		!strings.Contains(err.Error(), "AVX") ||
		!strings.Contains(err.Error(), "AVX2") ||
		!strings.Contains(err.Error(), "FMA3") ||
		!strings.Contains(err.Error(), "F16C") ||
		!strings.Contains(err.Error(), "BMI2") {
		t.Fatalf("expected every required feature named when all are missing: %v", err)
	}
	// One dedicated truth-table subtest per required feature: toggle that field
	// off in an otherwise-modern CPU and assert only that name appears in the
	// error. This proves each check is wired to its own field.
	cases := []struct {
		name string
		off  func(*CPUFeatures)
		want string
	}{
		{"missing avx2", func(f *CPUFeatures) { f.HasAVX2 = false }, "AVX2"},
		{"missing fma3", func(f *CPUFeatures) { f.HasFMA3 = false }, "FMA3"},
		{"missing f16c", func(f *CPUFeatures) { f.HasF16C = false }, "F16C"},
		{"missing bmi2", func(f *CPUFeatures) { f.HasBMI2 = false }, "BMI2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := all
			tc.off(&f)
			err := validateCPU(f)
			if err == nil {
				t.Fatalf("expected error when %s is missing", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q in %q", tc.want, err.Error())
			}
			for _, other := range []string{"AVX2", "FMA3", "F16C", "BMI2"} {
				if other == tc.want {
					continue
				}
				// "AVX2" contains "AVX" but the exact-name check is a whole word.
				if strings.Contains(err.Error(), " "+other+",") || strings.HasSuffix(err.Error(), " "+other) || strings.Contains(err.Error(), " "+other+".") {
					t.Fatalf("unexpected %q in %q when only %s is missing", other, err.Error(), tc.want)
				}
			}
		})
	}
}

package recon

import "time"

func SetCashFlowClockForTest(now func() time.Time) func() {
	prev := cashFlowNow
	cashFlowNow = now
	return func() { cashFlowNow = prev }
}

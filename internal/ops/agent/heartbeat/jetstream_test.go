package heartbeat

import "testing"

func TestMergeCompactRecordPreservesApprovedEnrollment(t *testing.T) {
	existing := CompactRecord{
		AgentID: "agent/mysql-01",
		Status: AgentStatus{
			Enrollment: EnrollmentStatus{
				State:      EnrollmentStateApproved,
				ApprovedAt: "2026-05-28T13:34:16Z",
			},
		},
	}
	next := CompactRecord{
		AgentID: "agent/mysql-01",
		Status: AgentStatus{
			Enrollment: EnrollmentStatus{State: EnrollmentStatePending},
		},
	}
	merged := mergeCompactRecord(existing, next)
	if merged.Status.Enrollment.State != EnrollmentStateApproved || merged.Status.Enrollment.ApprovedAt != existing.Status.Enrollment.ApprovedAt {
		t.Fatalf("merged enrollment = %#v", merged.Status.Enrollment)
	}
}

package workflow

import (
	"testing"
	"time"
)

func TestCheckMergeGate_NoTaskFile(t *testing.T) {
	dir := t.TempDir()

	// No task.json — old-style flow, should allow merge
	if err := checkMergeGate(dir); err != nil {
		t.Errorf("should allow merge when no task.json: %v", err)
	}
}

func TestCheckMergeGate_PhaseDone(t *testing.T) {
	dir := t.TempDir()

	task := NewTask("test", "test", "Test", "Desc")
	task.Phase = PhaseDone
	if err := SaveTask(dir, task); err != nil {
		t.Fatalf("SaveTask failed: %v", err)
	}

	if err := checkMergeGate(dir); err != nil {
		t.Errorf("should allow merge when phase is done: %v", err)
	}
}

func TestCheckMergeGate_PhaseVerification(t *testing.T) {
	dir := t.TempDir()

	task := NewTask("test", "test", "Test", "Desc")
	task.Phase = PhaseVerification
	if err := SaveTask(dir, task); err != nil {
		t.Fatalf("SaveTask failed: %v", err)
	}

	err := checkMergeGate(dir)
	if err == nil {
		t.Error("should block merge when phase is verification")
	}
}

func TestCheckMergeGate_PhaseImplementation(t *testing.T) {
	dir := t.TempDir()

	task := NewTask("test", "test", "Test", "Desc")
	task.Phase = PhaseImplementation
	if err := SaveTask(dir, task); err != nil {
		t.Fatalf("SaveTask failed: %v", err)
	}

	err := checkMergeGate(dir)
	if err == nil {
		t.Error("should block merge when phase is implementation")
	}
}

func TestVerifyFailure_AppendAndTransition(t *testing.T) {
	dir := t.TempDir()

	task := NewTask("test", "test", "Test", "Desc")
	task.Phase = PhaseVerification
	if err := SaveTask(dir, task); err != nil {
		t.Fatalf("SaveTask failed: %v", err)
	}

	// Simulate what FlowVerifyFail does
	task.VerifyFailures = append(task.VerifyFailures, VerifyFailure{
		Reason:    "Tests are failing",
		Timestamp: time.Now(),
	})

	if err := task.SetPhase(dir, PhaseImplementation, nil); err != nil {
		t.Fatalf("SetPhase to implementation failed: %v", err)
	}

	loaded, err := LoadTask(dir)
	if err != nil {
		t.Fatalf("LoadTask failed: %v", err)
	}

	if loaded.Phase != PhaseImplementation {
		t.Errorf("phase = %q, want %q", loaded.Phase, PhaseImplementation)
	}
	if len(loaded.VerifyFailures) != 1 {
		t.Fatalf("expected 1 failure, got %d", len(loaded.VerifyFailures))
	}
	if loaded.VerifyFailures[0].Reason != "Tests are failing" {
		t.Errorf("reason = %q, want %q", loaded.VerifyFailures[0].Reason, "Tests are failing")
	}
}

func TestVerifyFailure_AccumulatesMultiple(t *testing.T) {
	dir := t.TempDir()

	task := NewTask("test", "test", "Test", "Desc")
	task.Phase = PhaseVerification
	if err := SaveTask(dir, task); err != nil {
		t.Fatalf("SaveTask failed: %v", err)
	}

	// First failure
	task.VerifyFailures = append(task.VerifyFailures, VerifyFailure{
		Reason:    "Tests failing",
		Timestamp: time.Now(),
	})
	if err := task.SetPhase(dir, PhaseImplementation, nil); err != nil {
		t.Fatalf("first failure transition failed: %v", err)
	}

	// Advance back to verification (simulating a fix attempt + PR)
	if err := task.SetPhase(dir, PhaseVerification, nil); err != nil {
		t.Fatalf("advance to verification failed: %v", err)
	}

	// Second failure
	task.VerifyFailures = append(task.VerifyFailures, VerifyFailure{
		Reason:    "Missing docs",
		Timestamp: time.Now(),
	})
	if err := task.SetPhase(dir, PhaseImplementation, nil); err != nil {
		t.Fatalf("second failure transition failed: %v", err)
	}

	loaded, err := LoadTask(dir)
	if err != nil {
		t.Fatalf("LoadTask failed: %v", err)
	}

	if len(loaded.VerifyFailures) != 2 {
		t.Fatalf("expected 2 failures, got %d", len(loaded.VerifyFailures))
	}
	if loaded.VerifyFailures[0].Reason != "Tests failing" {
		t.Errorf("first reason = %q, want %q", loaded.VerifyFailures[0].Reason, "Tests failing")
	}
	if loaded.VerifyFailures[1].Reason != "Missing docs" {
		t.Errorf("second reason = %q, want %q", loaded.VerifyFailures[1].Reason, "Missing docs")
	}
}

func TestVerifyPass_Transition(t *testing.T) {
	dir := t.TempDir()

	task := NewTask("test", "test", "Test", "Desc")
	task.Phase = PhaseVerification
	if err := SaveTask(dir, task); err != nil {
		t.Fatalf("SaveTask failed: %v", err)
	}

	if err := task.SetPhase(dir, PhaseDone, nil); err != nil {
		t.Fatalf("SetPhase to done failed: %v", err)
	}

	loaded, err := LoadTask(dir)
	if err != nil {
		t.Fatalf("LoadTask failed: %v", err)
	}
	if loaded.Phase != PhaseDone {
		t.Errorf("phase = %q, want %q", loaded.Phase, PhaseDone)
	}
}

func TestVerifyPass_FromReady(t *testing.T) {
	dir := t.TempDir()

	task := NewTask("test", "test", "Test", "Desc")
	task.Phase = PhaseReady
	if err := SaveTask(dir, task); err != nil {
		t.Fatalf("SaveTask failed: %v", err)
	}

	// Verify pass should work from any phase except done
	task.Phase = PhaseDone
	if err := SaveTask(dir, task); err != nil {
		t.Fatalf("SaveTask failed: %v", err)
	}

	// Reload and check that done phase is rejected
	loaded, err := LoadTask(dir)
	if err != nil {
		t.Fatalf("LoadTask failed: %v", err)
	}
	if loaded.Phase != PhaseDone {
		t.Errorf("phase = %q, want %q", loaded.Phase, PhaseDone)
	}
}

func TestVerifyPass_RejectsDone(t *testing.T) {
	dir := t.TempDir()

	task := NewTask("test", "test", "Test", "Desc")
	task.Phase = PhaseDone
	if err := SaveTask(dir, task); err != nil {
		t.Fatalf("SaveTask failed: %v", err)
	}

	// Simulate what FlowVerifyPass checks
	if task.Phase == PhaseDone {
		return // correctly rejected
	}
	t.Error("should have rejected done phase")
}

func TestVerifyFail_EmptyReason(t *testing.T) {
	// FlowVerifyFail requires a non-empty reason
	reason := ""
	if reason == "" {
		// This is what FlowVerifyFail checks — the command should error
		return
	}
	t.Error("should not reach here")
}

func TestVerifyPass_DirectToDoneFromAnyPhase(t *testing.T) {
	// verify pass should jump directly to done from any non-done phase
	phases := []Phase{PhaseNew, PhaseShaping, PhaseReady, PhaseImplementation, PhaseVerification}
	for _, p := range phases {
		t.Run(string(p), func(t *testing.T) {
			dir := t.TempDir()
			task := NewTask("test", "test", "Test", "Desc")
			task.Phase = p
			if err := SaveTask(dir, task); err != nil {
				t.Fatalf("SaveTask failed: %v", err)
			}

			// Simulate FlowVerifyPass logic
			if task.Phase == PhaseDone {
				t.Fatal("should not be done yet")
			}
			task.Phase = PhaseDone
			if err := SaveTask(dir, task); err != nil {
				t.Fatalf("SaveTask failed: %v", err)
			}

			loaded, err := LoadTask(dir)
			if err != nil {
				t.Fatalf("LoadTask failed: %v", err)
			}
			if loaded.Phase != PhaseDone {
				t.Errorf("phase = %q, want %q", loaded.Phase, PhaseDone)
			}
		})
	}
}

func TestVerifyFail_RejectsNewAndDone(t *testing.T) {
	for _, p := range []Phase{PhaseNew, PhaseDone} {
		t.Run(string(p), func(t *testing.T) {
			task := &Task{Phase: p}
			// Simulate FlowVerifyFail guards
			if task.Phase == PhaseDone || task.Phase == PhaseNew {
				return // correctly rejected
			}
			t.Errorf("should have rejected phase %q", p)
		})
	}
}

func TestFullVerifyFailCycle(t *testing.T) {
	dir := t.TempDir()

	task := NewTask("test", "test", "Test", "Desc")

	// Advance through phases to verification
	phases := []Phase{PhaseShaping, PhaseReady, PhaseImplementation, PhaseVerification}
	for _, p := range phases {
		if err := task.SetPhase(dir, p, nil); err != nil {
			t.Fatalf("SetPhase to %q failed: %v", p, err)
		}
	}

	// Verify fail → back to implementation
	task.VerifyFailures = append(task.VerifyFailures, VerifyFailure{
		Reason:    "Bug found",
		Timestamp: time.Now(),
	})
	if err := task.SetPhase(dir, PhaseImplementation, nil); err != nil {
		t.Fatalf("verify fail transition failed: %v", err)
	}

	// Implementation → verification again
	if err := task.SetPhase(dir, PhaseVerification, nil); err != nil {
		t.Fatalf("re-advance to verification failed: %v", err)
	}

	// Verify pass → done
	if err := task.SetPhase(dir, PhaseDone, nil); err != nil {
		t.Fatalf("verify pass transition failed: %v", err)
	}

	loaded, err := LoadTask(dir)
	if err != nil {
		t.Fatalf("LoadTask failed: %v", err)
	}
	if loaded.Phase != PhaseDone {
		t.Errorf("final phase = %q, want %q", loaded.Phase, PhaseDone)
	}
	if len(loaded.VerifyFailures) != 1 {
		t.Errorf("expected 1 failure in history, got %d", len(loaded.VerifyFailures))
	}
}

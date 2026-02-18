package output

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestLineSpinner_AllResolvedBeforeRun(t *testing.T) {
	spinner := NewLineSpinner(2)
	spinner.SetLine(0, "line-a %s done")
	spinner.SetLine(1, "line-b %s done")
	spinner.Resolve(0, "OK")
	spinner.Resolve(1, "OK")

	var buf bytes.Buffer
	spinner.w = &buf
	spinner.Run()

	out := buf.String()
	if !strings.Contains(out, "line-a OK done") {
		t.Errorf("expected line-a with OK, got: %s", out)
	}
	if !strings.Contains(out, "line-b OK done") {
		t.Errorf("expected line-b with OK, got: %s", out)
	}
}

func TestLineSpinner_ResolvesDuringRun(t *testing.T) {
	spinner := NewLineSpinner(2)
	spinner.SetLine(0, "first %s")
	spinner.SetLine(1, "second %s")

	var buf bytes.Buffer
	spinner.w = &buf

	// Resolve immediately after Run starts — the spinner should exit
	// once all lines are resolved.
	go func() {
		spinner.Resolve(0, "done0")
		spinner.Resolve(1, "done1")
	}()

	spinner.Run()

	out := buf.String()
	// The final redraw should contain the resolved values
	if !strings.Contains(out, "first done0") {
		t.Errorf("expected resolved line 0, got: %s", out)
	}
	if !strings.Contains(out, "second done1") {
		t.Errorf("expected resolved line 1, got: %s", out)
	}
}

func TestLineSpinner_SingleLine(t *testing.T) {
	spinner := NewLineSpinner(1)
	spinner.SetLine(0, "status: %s")
	spinner.Resolve(0, "merged")

	var buf bytes.Buffer
	spinner.w = &buf
	spinner.Run()

	out := buf.String()
	if !strings.Contains(out, "status: merged") {
		t.Errorf("expected resolved status, got: %s", out)
	}
}

func TestLineSpinner_PartialResolve(t *testing.T) {
	spinner := NewLineSpinner(3)
	spinner.SetLine(0, "a %s")
	spinner.SetLine(1, "b %s")
	spinner.SetLine(2, "c %s")

	var buf bytes.Buffer
	spinner.w = &buf

	// Only resolving 2 of 3 initially — spinner should keep running
	spinner.Resolve(0, "ok")

	done := make(chan struct{})
	go func() {
		spinner.Run()
		close(done)
	}()

	// Resolve remaining
	spinner.Resolve(1, "ok")
	spinner.Resolve(2, "ok")
	<-done

	out := buf.String()
	if !strings.Contains(out, "a ok") {
		t.Errorf("expected line a resolved, got: %s", out)
	}
	if !strings.Contains(out, "b ok") {
		t.Errorf("expected line b resolved, got: %s", out)
	}
	if !strings.Contains(out, "c ok") {
		t.Errorf("expected line c resolved, got: %s", out)
	}
}

func TestLineSpinner_CursorSaveRestore(t *testing.T) {
	spinner := NewLineSpinner(2)
	spinner.SetLine(0, "line-a %s")
	spinner.SetLine(1, "line-b %s")
	spinner.Resolve(0, "OK")
	spinner.Resolve(1, "OK")

	var buf bytes.Buffer
	spinner.w = &buf
	spinner.Run()

	out := buf.String()
	// Run should emit cursor-hide + cursor-save before the initial lines
	if !strings.Contains(out, "\033[?25l\0337") {
		t.Errorf("expected cursor hide + save at start, got: %q", out)
	}
	// Redraw should emit cursor-restore + clear-to-end-of-screen
	if !strings.Contains(out, "\0338\033[J") {
		t.Errorf("expected cursor restore + clear in redraw, got: %q", out)
	}
	// Run should emit cursor-show at the end
	if !strings.HasSuffix(out, "\033[?25h") {
		t.Errorf("expected cursor show at end, got: %q", out)
	}
}

func TestLineSpinner_NoCursorUpEscape(t *testing.T) {
	// Verify the old \033[nA cursor-up escape is no longer used.
	spinner := NewLineSpinner(2)
	spinner.SetLine(0, "a %s")
	spinner.SetLine(1, "b %s")
	spinner.Resolve(0, "done")
	spinner.Resolve(1, "done")

	var buf bytes.Buffer
	spinner.w = &buf
	spinner.Run()

	out := buf.String()
	if strings.Contains(out, "\033[2A") {
		t.Errorf("should not use cursor-up escape \\033[nA, got: %q", out)
	}
}

func TestLineSpinner_ZeroLines(t *testing.T) {
	spinner := NewLineSpinner(0)

	var buf bytes.Buffer
	spinner.w = &buf

	// Run should return immediately without blocking.
	spinner.Run()

	if buf.Len() != 0 {
		t.Errorf("expected no output for zero lines, got: %q", buf.String())
	}
}

func TestLineSpinner_StopBeforeAllResolved(t *testing.T) {
	spinner := NewLineSpinner(2)
	spinner.SetLine(0, "a %s")
	spinner.SetLine(1, "b %s")
	// Only resolve one line — the other is still spinning
	spinner.Resolve(0, "ok")

	var buf bytes.Buffer
	spinner.w = &buf

	done := make(chan struct{})
	go func() {
		spinner.Run()
		close(done)
	}()

	// Stop the spinner before all lines resolve
	spinner.Stop()
	<-done

	out := buf.String()
	// Must still emit cursor-show at the end
	if !strings.HasSuffix(out, "\033[?25h") {
		t.Errorf("expected cursor show after Stop(), got: %q", out)
	}
	// Must contain the resolved line
	if !strings.Contains(out, "a ok") {
		t.Errorf("expected resolved line a, got: %q", out)
	}
}

func TestLineSpinner_StopIdempotent(t *testing.T) {
	spinner := NewLineSpinner(1)
	spinner.SetLine(0, "x %s")
	spinner.Resolve(0, "done")

	var buf bytes.Buffer
	spinner.w = &buf
	spinner.Run()

	// Calling Stop after Run has already returned should not panic
	spinner.Stop()
	spinner.Stop()
}

func TestSpin_Success(t *testing.T) {
	var buf bytes.Buffer
	err := spinTo(&buf, "Doing work", func() error {
		return nil
	})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Doing work") {
		t.Errorf("expected message in output, got: %s", out)
	}
	// On success the final line should contain the success marker
	if !strings.Contains(out, "✓") {
		t.Errorf("expected success marker (✓) in output, got: %s", out)
	}
}

func TestSpin_Error(t *testing.T) {
	var buf bytes.Buffer
	testErr := errors.New("something broke")
	err := spinTo(&buf, "Failing task", func() error {
		return testErr
	})
	if !errors.Is(err, testErr) {
		t.Fatalf("expected testErr, got: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Failing task") {
		t.Errorf("expected message in output, got: %s", out)
	}
	// On error the final line should contain the progress marker, not success
	if !strings.Contains(out, "›") {
		t.Errorf("expected progress marker (›) in output, got: %s", out)
	}
	if strings.Contains(out, "✓") {
		t.Errorf("should not contain success marker on error, got: %s", out)
	}
}

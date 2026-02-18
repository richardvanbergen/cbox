# Fix spinner display corruption on Ctrl+C exit

## Context

The spinner implementation lives in `/workspace/internal/output/spinner.go`. There are two spinner functions:

1. **`LineSpinner.Run()`** (line 74) — multi-line spinner used by `flow status`, `flow clean`, and single-branch `flow status`. It hides the cursor (`\033[?25l`), saves cursor position (`\0337`), then redraws all lines every 80ms by restoring cursor position (`\0338`) and clearing below (`\033[J`). It only shows the cursor (`\033[?25h`) when all lines are resolved via the `s.done` channel.

2. **`Spin()`** (line 120) — single-line spinner used throughout the workflow for tasks like "Creating issue", "Starting sandbox", "Pushing branch", etc. It uses `\r\033[2K` to overwrite the current line each tick.

**Root cause:** Neither spinner handles SIGINT/SIGTERM. When Ctrl+C kills the process:
- The cursor remains hidden (`\033[?25l` was sent but `\033[?25h` never follows)
- For `LineSpinner`, the cursor-save/restore state is left dangling
- The last partial redraw may leave duplicated or garbled output

The corruption in the example shows the `LineSpinner` output repeated many times — each repetition is one 80ms animation frame that was printed but never overwritten because the cursor-restore/clear cycle was interrupted.

### Callers affected

- `FlowStatus` at `workflow.go:625` — multi-flow status listing
- `flowClean` at `workflow.go:751` — checking PR merge status
- `printFlowState` at `workflow.go:816` — single-flow detail view
- All `output.Spin()` callers throughout `workflow.go` (8+ call sites)

## Approach

### 1. Add a `Stop()` method to `LineSpinner`

Add a `Stop()` method that can be called to forcefully terminate the spinner, emitting cursor-show and performing cleanup. This closes the `done` channel if not already closed.

```go
func (s *LineSpinner) Stop() {
    s.mu.Lock()
    defer s.mu.Unlock()
    select {
    case <-s.done:
    default:
        close(s.done)
    }
}
```

### 2. Add signal-aware cleanup to `LineSpinner.Run()`

Register a SIGINT/SIGTERM handler within `Run()` that ensures cursor visibility is restored. Use a `defer` to guarantee cleanup runs regardless of exit path.

```go
func (s *LineSpinner) Run() {
    // ... existing zero-lines check ...

    // Hide cursor and save position
    fmt.Fprintf(s.w, "\033[?25l\0337")

    // Ensure cursor is always restored, even on signal
    defer fmt.Fprintf(s.w, "\033[?25h")

    // Catch signals to stop the spinner gracefully
    sig := make(chan os.Signal, 1)
    signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
    defer signal.Stop(sig)

    // ... print initial lines, start ticker ...

    for {
        select {
        case <-s.done:
            s.redraw()
            return
        case <-sig:
            // Signal received — do a final redraw with current state,
            // then return (defer handles cursor-show)
            s.redraw()
            return
        case <-ticker.C:
            s.frame++
            s.redraw()
        }
    }
}
```

Key design points:
- The `defer fmt.Fprintf(s.w, "\033[?25h")` guarantees cursor-show runs on any exit path (normal completion, signal, or panic).
- `signal.Notify` + `signal.Stop` scopes the handler to the spinner's lifetime.
- On signal, we do a final redraw to leave the output in a clean state (showing whatever has been resolved so far, with spinner frames frozen for unresolved lines).
- After the signal handler returns, the default Go behavior (process exit on second Ctrl+C) is restored via `signal.Stop`.

### 3. Add signal-aware cleanup to `Spin()` / `spinTo()`

Apply the same pattern to the single-line `spinTo()` function:

```go
func spinTo(w io.Writer, msg string, fn func() error) error {
    ch := make(chan error, 1)
    go func() { ch <- fn() }()

    sig := make(chan os.Signal, 1)
    signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
    defer signal.Stop(sig)

    // ... existing animation loop ...

    for {
        select {
        case err := <-ch:
            // ... existing completion logic ...
        case <-sig:
            // Clean up the line and re-raise signal
            fmt.Fprintf(w, "\r\033[2K")
            fmt.Fprintf(w, "%s %s\n", progressPrefix.Render("›"), msg)
            return nil
        case <-ticker.C:
            // ... existing animation logic ...
        }
    }
}
```

### 4. Re-raise the signal after cleanup

After the spinner cleans up, the process should still terminate. The signal handler in `Run()` and `spinTo()` consumes the signal, so the process won't die on its own. Two options:

**Option A (preferred):** After spinner cleanup returns, the calling code returns normally, and the process exits through its normal flow. Since Ctrl+C during `flow status` should just exit, returning `nil` from `FlowStatus` is fine — the process will exit normally.

**Option B:** Re-raise the signal with `syscall.Kill(syscall.Getpid(), sig)` after `signal.Stop()`. This preserves the correct exit code (130 for SIGINT) for scripts that check exit status.

We'll go with **Option A** for simplicity. The spinner returns, and the command's `RunE` returns `nil`, causing a clean exit. If the caller needs to know a signal was received, the `Run()` method can return a boolean or the `Stop()` method can set a flag — but for the current use cases this is unnecessary.

### 5. Update tests

Add tests to verify:
- `LineSpinner` restores cursor visibility when `Stop()` is called
- The `defer` cleanup in `Run()` emits `\033[?25h` even when the done channel is closed externally
- `spinTo` cleans up its output line when interrupted

The existing `TestLineSpinner_CursorSaveRestore` test already verifies the happy path. We need analogous tests for the interrupt path.

## Acceptance Criteria

- [ ] `LineSpinner.Run()` restores cursor visibility (`\033[?25h`) on SIGINT/SIGTERM
- [ ] `LineSpinner.Run()` uses `defer` to guarantee cursor restoration on any exit path
- [ ] `LineSpinner` has a `Stop()` method that unblocks `Run()` and triggers cleanup
- [ ] `Spin()` / `spinTo()` cleans up the terminal line on SIGINT/SIGTERM (clears the spinner line, shows a clean final state)
- [ ] Signal handlers are scoped with `signal.Stop()` so they don't leak beyond the spinner's lifetime
- [ ] Existing spinner tests continue to pass
- [ ] New tests verify cursor restoration when `Stop()` is called before all lines resolve
- [ ] New test verifies `spinTo` cleanup on simulated interruption
- [ ] No regression: `flow status` still displays correctly when all PR fetches complete normally
- [ ] No regression: `flow status <branch>` single-branch view still works correctly

## Notes

- The corruption shown in the example is specifically the `LineSpinner` — each repeated block of 5 lines is one animation frame that was printed but never overwritten. The `\0338\033[J` (restore-cursor + clear-below) that should have erased the previous frame was never executed because the process was killed.
- The `Spin()` function is less visually catastrophic on interrupt (it only corrupts one line), but should still be fixed for consistency.
- We do NOT need to handle cursor restoration at a global level (e.g., in `main()`). Scoping the signal handler to each spinner invocation is cleaner and avoids interfering with other signal handlers in `hostcmd` and `serve`.
- The `spinTo` function takes an `io.Writer` parameter for testability. Signal handling can't be injected as easily, but we can test cleanup by calling `Stop()` or by simulating channel-based interruption in tests (using a separate `ctx` or interrupt channel rather than actual OS signals).

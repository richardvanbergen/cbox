# Task: Remove hello command stub

## Context

The `cbox hello` command currently exists as a minimal stub at
`cmd/cbox/main.go:176-184`:

```go
func helloCmd() *cobra.Command {
    return &cobra.Command{
        Use:   "hello",
        Short: "Say hello",
        Run: func(cmd *cobra.Command, args []string) {
            output.Success("Hello from cbox!")
        },
    }
}
```

It is registered in `buildRootCmd()` at line 56:

```go
root.AddCommand(helloCmd())
```

There is a test in `cmd/cbox/hello_test.go` that validates the command name and
successful execution.

The user wants to remove this stub so the flow branch can be repurposed for
something useful.

## Approach

1. **Remove the `helloCmd()` function** from `cmd/cbox/main.go` (lines 176-184).
2. **Remove the `root.AddCommand(helloCmd())` call** from `buildRootCmd()` (line 56).
3. **Delete the test file** `cmd/cbox/hello_test.go`.
4. **Verify the build passes** with `go build -o bin/cbox ./cmd/cbox`.
5. **Verify tests pass** with `go test ./...`.

No other files reference `helloCmd` — the change is self-contained.

## Acceptance Criteria

- [ ] `helloCmd()` function is removed from `cmd/cbox/main.go`
- [ ] `root.AddCommand(helloCmd())` is removed from `buildRootCmd()`
- [ ] `cmd/cbox/hello_test.go` is deleted
- [ ] `cbox_build` succeeds (exit 0)
- [ ] `cbox_test` succeeds (exit 0)
- [ ] Running `cbox hello` produces an error (unknown command)

## Notes

- This is intentionally a small change — the purpose is to exercise the cbox
  flow workflow end-to-end (shaping → implementation → PR).

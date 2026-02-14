# CBox

## Build

With [just](https://github.com/casey/just):

```
just build
```

Or manually with ldflags to embed the version:

```
go build -ldflags '-s -w -X main.version=v0.0.1-alpha' -o bin/cbox ./cmd/cbox
```

Plain build (version falls back to git commit hash or "dev"):

```
go build -o bin/cbox ./cmd/cbox
```

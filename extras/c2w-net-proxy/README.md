# Document is at [`../../examples/networking/fetch/`](../../examples/networking/fetch/).

## how to build

- Requirement: Go >= 1.21

The following builds the binary at `/tmp/out/c2w-net-proxy.wasm`.

Makefile is avaliable at the root dir of this repo:

```
$ PREFIX=/tmp/out/ make c2w-net-proxy.wasm
```

Or you can manually build using go command:

```
$ GOOS=wasip1 GOARCH=wasm go build -o /tmp/out/c2w-net-proxy.wasm .
```

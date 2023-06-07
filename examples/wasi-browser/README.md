# Running container on browser with xterm-pty, leveraging WASI

This is an example of running a container on browser.
The difference between this and [emscripten example](../emscripten) is that this example runs WASI-compiled container on browser rather than emscripten-compiled one.

- pros: You can leverage WASI-specific optimization methods like [Wizer](https://github.com/bytecodealliance/wizer/).  container2wasm pre-initialies the kernel using Wizer by default so the time to take startup an WASI-compiled container hopefully be faster than the emscripten-compiled one.
- cons: Bigger WASM image size.

This example leverages polyfill library [browser_wasi_shim](https://github.com/bjorn3/browser_wasi_shim) provides WASI APIs to the WASM binary on browser.
We integrated that WASI polyfill's IO to xterm-pty for allowing the user connecting to that container via the terminal.

## Example

![Ubuntu container on browser](../../docs/images/ubuntu-wasi-on-browser.png)

First, prepare a WASI image named `out.wasm`.

```
$ c2w ubuntu:22.04 /tmp/out-js2/htdocs/out.wasm
```

Then, serve it via browser.

> Run this at the project repo root directory.

```
$ cp -R ./examples/wasi-browser/* /tmp/out-js2/ && chmod 755 /tmp/out-js2/htdocs
$ docker run --rm -p 8080:80 \
         -v "/tmp/out-js2/htdocs:/usr/local/apache2/htdocs/:ro" \
         -v "/tmp/out-js2/xterm-pty.conf:/usr/local/apache2/conf/extra/xterm-pty.conf:ro" \
         --entrypoint=/bin/sh httpd -c 'echo "Include conf/extra/xterm-pty.conf" >> /usr/local/apache2/conf/httpd.conf && httpd-foreground'
```

You can run the container on browser via `localhost:8080`.

## Tip: How to generate the WASI Polyfill (`browser_wasi_shim`)

The steps to generate the WASI Polyfill is written in the [Dockerfile](./Dockerfile)

```console
$ docker buildx build --output type=local,dest=./htdocs/ .
```

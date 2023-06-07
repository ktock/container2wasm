# python example (x86_64)

![Python x86_64 on browser](../../docs/images/python-hello-x86-64-wasi-on-browser.png)

## Convert to WASI

Convert the image to WASM

```
$ c2w python:3.11-slim /tmp/out/out.wasm
```

Run it on the runtime:

```
$ wasmtime -- /tmp/out/out.wasm python3 -c "print ('hello world')"
hello world
```

This passes `-c "print ('hello world')"` argument to `python3` (`CMD` of this image) and `hello world` is printed.

## Run WASI image on browser

The following runs the WASI-converted container image on browser.

> Run this at the project repo root directory.

```
$ c2w python:3.11-slim /tmp/out-js2/htdocs/out.wasm
$ cp -R ./examples/wasi-browser/* /tmp/out-js2/ && chmod 755 /tmp/out-js2/htdocs
$ docker run --rm -p 8080:80 \
         -v "/tmp/out-js2/htdocs:/usr/local/apache2/htdocs/:ro" \
         -v "/tmp/out-js2/xterm-pty.conf:/usr/local/apache2/conf/extra/xterm-pty.conf:ro" \
         --entrypoint=/bin/sh httpd -c 'echo "Include conf/extra/xterm-pty.conf" >> /usr/local/apache2/conf/httpd.conf && httpd-foreground'
```

You can run the container on browser via `localhost:8080`.

> Please see [wasi-browser example](../wasi-browser) for details about WASI-on-browser.

## Run on browser using emscripten

`--to-js` provides emscripten-compiled image runnable on browser.

```
$ c2w --to-js python:3.11-slim /tmp/pythonjs/htdocs/
```

Run it on browser:

> Run this at the project repo root directory.

```
$ cp -R ./examples/emscripten/* /tmp/pythonjs/ && chmod 755 /tmp/pythonjs/htdocs
$ docker run --rm -p 8080:80 \
         -v "/tmp/pythonjs/htdocs:/usr/local/apache2/htdocs/:ro" \
         -v "/tmp/pythonjs/xterm-pty.conf:/usr/local/apache2/conf/extra/xterm-pty.conf:ro" \
         --entrypoint=/bin/sh httpd -c 'echo "Include conf/extra/xterm-pty.conf" >> /usr/local/apache2/conf/httpd.conf && httpd-foreground'
```

You can run the container on browser via `localhost:8080`.

> you might need to wait a while untils the prompt is printed

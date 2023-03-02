# python example

Build a riscv64 python image:

```console
$ cat <<EOF | docker buildx build -t python-ubuntu-riscv64 --platform=linux/riscv64 --load -
FROM riscv64/ubuntu:22.04
RUN apt-get update && apt-get install -y python3
ENTRYPOINT ["python3"]
EOF
```

## Convert to WASI

Convert the image to WASM

```
$ c2w python-ubuntu-riscv64 /tmp/out/out.wasm
```

Run it on the runtime:

```
$ wasmtime -- /tmp/out/out.wasm -- -c "print ('hello world')"
hello world
```

This passes `-c "print ('hello world')"` argument to `python3` (`ENTRYPOINT` of this image) and `hello world` is printed.

## Run on browser

```
$ c2w --to-js python-ubuntu-riscv64 /tmp/pythonjs/htdocs/
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

This runs the image on browser.
We specified `python3` as the `ENTRYPOINT` of this image so python REPL starts (you might need to wait a while untils the prompt is printed).

![python with emscripten](../../docs/images/python-hello.png)

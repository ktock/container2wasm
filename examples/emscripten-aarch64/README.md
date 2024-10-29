# Running AArch64 container on browser with emscripten and experimental QEMU TCG mode

c2w experimentally integrates QEMU TCG on Wasm(emscripten).
AArch64 containers can run on the browser leveraging this.
Multicore CPUs can also be enabled leveraging MTTCG(Multi-Threded TCG).

> NOTE: Example of x86_64 container with QEMU TCG is available at [./../emscripten-qemu-tcg](./../emscripten-qemu-tcg)

Example: aarch64 alpine on emscripten

```
$ make
$ mkdir -p /tmp/out-js5/htdocs
$ ./out/c2w --to-js --target-arch=aarch64 --dockerfile=Dockerfile --assets=. arm64v8/alpine:3.20 /tmp/out-js5/htdocs/
$ cp -R ./examples/emscripten-aarch64/* /tmp/out-js5/
$ docker run --rm -p 8083:80 \
         -v "/tmp/out-js5/htdocs:/usr/local/apache2/htdocs/:ro" \
         -v "/tmp/out-js5/xterm-pty.conf:/usr/local/apache2/conf/extra/xterm-pty.conf:ro" \
         --entrypoint=/bin/sh httpd -c 'echo "Include conf/extra/xterm-pty.conf" >> /usr/local/apache2/conf/httpd.conf && httpd-foreground'
```

Then `localhost:8083` serves the image.

> NOTE: As of now, QEMU TCG mode isn't enabled for Wasi images (i.e. w/o `--to-js` flag)

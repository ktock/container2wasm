# Experimental support for QEMU TCG (x86_64)

c2w experimentally integrates QEMU TCG on Wasm(emscripten), using [QEMU Wasm](https://github.com/ktock/qemu-wasm).
Multicore CPUs can also be enabled leveraging MTTCG(Multi-Threded TCG).

> NOTE: Example of AArch64 container with QEMU TCG is available at [./../emscripten-aarch64](./../emscripten-aarch64)

Example: x86_64 alpine on emscripten

```
$ make
$ mkdir -p /tmp/out-js4/htdocs
$ ./out/c2w --dockerfile=Dockerfile --assets=. --target-stage=js-qemu-amd64 alpine /tmp/out-js4/htdocs/
$ cp -R ./examples/emscripten-qemu-tcg/* /tmp/out-js4/
$ docker run --rm -p 8082:80 \
         -v "/tmp/out-js4/htdocs:/usr/local/apache2/htdocs/:ro" \
         -v "/tmp/out-js4/xterm-pty.conf:/usr/local/apache2/conf/extra/xterm-pty.conf:ro" \
         --entrypoint=/bin/sh httpd -c 'echo "Include conf/extra/xterm-pty.conf" >> /usr/local/apache2/conf/httpd.conf && httpd-foreground'
```

Then `localhost:8082` serves the image.

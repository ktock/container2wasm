# Experimental support for QEMU TCG (aarch64)

c2w experimentally integrates QEMU TCG on Wasm(emscripten).
Multicore CPUs can also be enabled leveraging MTTCG(Multi-Threded TCG).

Example: aarch64 alpine on emscripten

```
$ make
$ mkdir -p /tmp/out-js5/htdocs
$ ./out/c2w --dockerfile=Dockerfile --assets=. --target-stage=js-qemu-aarch64 --build-arg=NO_BINFMT=true --target-arch=aarch64 arm64v8/alpine:3.20 /tmp/out-js5/htdocs/
$ cp -R ./examples/emscripten-qemu-tcg-aarch64/* /tmp/out-js5/
$ docker run --rm -p 8083:80 \
         -v "/tmp/out-js5/htdocs:/usr/local/apache2/htdocs/:ro" \
         -v "/tmp/out-js5/xterm-pty.conf:/usr/local/apache2/conf/extra/xterm-pty.conf:ro" \
         --entrypoint=/bin/sh httpd -c 'echo "Include conf/extra/xterm-pty.conf" >> /usr/local/apache2/conf/httpd.conf && httpd-foreground'
```

Then `localhost:8083` serves the image.

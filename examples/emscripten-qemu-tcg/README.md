# Experimental support for QEMU TCG

c2w experimentally integrates QEMU TCG on Wasm(emscripten).
Multicore CPUs can also be enabled leveraging MTTCG(Multi-Threded TCG).

- NOTE1: Booting the kernel and the containers can take about 1 min. This example enables boot logs to allow tracking the status.
- NOTE2: Works only on chrome. Networking is not supported as of now.

Example: x86_64 alpine on emscripten

```
$ make
$ mkdir -p /tmp/out-js4/htdocs
$ ./out/c2w --assets=. --target-stage=js-qemu-amd64 alpine /tmp/out-js4/htdocs/
$ cp -R ./examples/emscripten-qemu-tcg/* /tmp/out-js4/
$ docker run --rm -p 8082:80 \
         -v "/tmp/out-js4/htdocs:/usr/local/apache2/htdocs/:ro" \
         -v "/tmp/out-js4/xterm-pty.conf:/usr/local/apache2/conf/extra/xterm-pty.conf:ro" \
         --entrypoint=/bin/sh httpd -c 'echo "Include conf/extra/xterm-pty.conf" >> /usr/local/apache2/conf/httpd.conf && httpd-foreground'
```

Then `localhost:8082` serves the image.

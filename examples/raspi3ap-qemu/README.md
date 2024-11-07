# Raspberry Pi board emulation on browser

c2w experimentally integrates QEMU TCG on Wasm(emscripten), using [QEMU Wasm](https://github.com/ktock/qemu-wasm).
QEMU supports emulating a variety of borads including Rasberry Pi boards.
This document shows an example of Raspberry Pi (`raspi3ap`) emulated on browser

> NOTE1: Example of AArch64 container with QEMU TCG is available at [./../emscripten-aarch64](./../emscripten-aarch64)
> NOTE2: Example of x86_64 container with QEMU TCG is available at [./../emscripten-qemu-tcg](./../emscripten-qemu-tcg)

This example runs busybox on the emulated board.

Prepare images to use on the board.

```
$ mkdir /tmp/out/
$ docker build --output=type=local,dest=/tmp/out/ ./examples/raspi3ap-qemu/image/
```

Make them runnable on browser using c2w command and serve them on localhost.

```
$ make
$ mkdir -p /tmp/out-js6/htdocs
$ ./out/c2w --dockerfile=Dockerfile --assets=. --to-js --target-arch=aarch64 --pack=/tmp/out/ /tmp/out-js6/htdocs/
$ cp -R ./examples/raspi3ap-qemu/src/* /tmp/out-js6/
$ docker run --rm -p 8086:80 \
         -v "/tmp/out-js6/htdocs:/usr/local/apache2/htdocs/:ro" \
         -v "/tmp/out-js6/xterm-pty.conf:/usr/local/apache2/conf/extra/xterm-pty.conf:ro" \
         --entrypoint=/bin/sh httpd -c 'echo "Include conf/extra/xterm-pty.conf" >> /usr/local/apache2/conf/httpd.conf && httpd-foreground'
```

Then `localhost:8086` serves the image.

This runs the real Raspberry Pi kernel on the emulated board by QEMU.
For examples `/proc/cpuinfo` and `/sys/firmware/devicetree/base/model` show that the environment is the Raspberry Pi board.

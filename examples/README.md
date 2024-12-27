# Examples 

Examples of container-to-wasm conversion and running the WASM image on WASI runtime and browser.

Please refer to [README](./../README.md) for basic examples (e.g. Ubuntu on WASM and browser).

- [`./wasi-browser`](./wasi-browser/): Running container on browser by WASI-on-browser Polyfill.
- [`./emscripten`](./emscripten/): Running container on browser by emscripten.
- [`./python-x86_64`](./python-x86_64/): Running x86_64 Python container on the wasm runtime and browser.
- [`./python-riscv64`](./python-riscv64/): Running RISC-V Python container on the wasm runtime and browser.
- [`./php-x86_64`](./php-x86_64/): Running x86_64 PHP container on the wasm runtime and browser.
- [`./php-riscv64`](./php-riscv64/): Running RISC-V PHP container on the wasm runtime and browser.
- [`./target-aarch64`](./target-aarch64/): Running aarch64 container on the wasm runtime and browser.
- [`./networking`](./networking/): Running container on WASM with networking support.
- [`./no-conversion`](./no-conversion/): Running container on browser without pre-conversion of container images.
- [`./emscripten-qemu`](./emscripten-qemu/): Running QEMU TCG on browser (x86_64 container).
- [`./emscripten-aarch64`](./emscripten-aarch64): Running QEMU TCG on browser (AArch64 container).
- [`./raspi3ap-qemu`](./raspi3ap-qemu): Raspberry Pi board emulation on browser

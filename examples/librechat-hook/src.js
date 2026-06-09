const containerImage = 'https://ktock.github.io/image-gcc-alpine/oci/';
let done = false;
let started = false;
let inputBuffer = ""
let outputNotify = null;
let systemOutputBuf = "";
let systemOutputDelimiter = "";

async function install(axios) {
    if (done) {
        return
    }
    done = true

    const originalPost = axios.post;
    let readableCallbacks = [];

    axios.post = async function (...args) {
        const [url, body] = args;

        if (url.includes('/api/agents/tools/execute_code/call') && started) {
            console.log('Intercepted', body);
            let parsed = JSON.parse(body);
            if (parsed.lang == 'bash') {
                systemOutputBuf = "";
                systemOutputDelimiter = "AGENT COMMAND END " + crypto.randomUUID();
                inputBuffer = parsed.code + '\n';
                inputBuffer += '# ' + systemOutputDelimiter + '\n';
                for (const cb of readableCallbacks) {
                    cb();
                }
                return new Promise((resolve) => {
                    outputNotify = (result) => {
                        resolve({
                            data: {
                                result: result,
                                attachments: [],
                            },
                        });
                        outputNotify = null;
                    };
                });
            }
        }
        if (!started) {
            console.error("exec API requested but the container has not started");
        }

        return originalPost.apply(this, args);
    };

    const c2wJsBlobUri = '/librechat-c2w-patch/out.js'
    const stackWorkerJsBlobUri = '/librechat-c2w-patch/stack-worker.js';
    const imagemounterUri = '/librechat-c2w-patch/imagemounter.wasm.gzip'
    const loadjsUri = '/librechat-c2w-patch/load.js'
    const argModuleUri = '/librechat-c2w-patch/arg-module.js'

    let PTY = {
        onSignal: () => {},
        ioctl: (a) => {
            if (a == "TCGETS") {
                return {
                    iflag: 0,
                    oflag: 0,
                    cflag: 0,
                    lflag: 0,
                    cc: 0,
                };
            }
            return {};
        },
        write: () => {},
    };
    let Module = {
        preRun: [],
        pty: PTY,
    };
    Module['preRun'].push((Module) => {
        Module['TTY'].stream_ops.poll = (stream, timeout, notifyCallback) => {
            if (inputBuffer.length > 0) {
                return 1;
            }
            if (notifyCallback != null) {
                notifyCallback.registerCleanupFunc(() => {
                    const i = readableCallbacks.indexOf(notifyCallback);
                    if (i != -1) readableCallbacks.splice(i, 1);
                });
                readableCallbacks.push(notifyCallback);
            }
            return 0;
        };
        Module['TTY'].stream_ops.write = (stream, buffer, offset, length) => {
            if (!started) {
                started = true;
                console.log("container started");
            }
            systemOutputBuf += String.fromCharCode(Array.from(buffer.subarray(offset, offset + length)));
            let i = 0;
            let includesDelim = false;
            for (const char of systemOutputBuf) {
                if (char == systemOutputDelimiter[i]) {
                    i++;
                    if (i == systemOutputDelimiter.length) {
                        includesDelim = true;
                        break;
                    }
                }
            }
            if (includesDelim && outputNotify) {
                outputNotify(systemOutputBuf.split('/ #')[0]);
            }
            console.debug(systemOutputBuf);
            return length;
        }
        Module['TTY'].stream_ops.read = (stream, buffer, offset, length) => {
            let position = 0;
            var size = Math.min(inputBuffer.length, length);
            for (var i = 0; i < size; i++) buffer[offset + i] = inputBuffer.charCodeAt(position + i);
            if (size <= inputBuffer.length) {
                inputBuffer = inputBuffer.slice(size);
            }
            return size;
        }
    });

    await RunContainer.createContainerQEMUWasm(
        Module,
        c2wJsBlobUri,
        containerImage,
        stackWorkerJsBlobUri,
        imagemounterUri,
        argModuleUri,
        loadjsUri,
        (path) => '/librechat-c2w-patch/' + path,
        {
            extraInfo: `env: NO_COLOR=1\n`,
            log: (l) => console.log(l),
        }
    );
}

window.C2WPatch = {
    install,
};

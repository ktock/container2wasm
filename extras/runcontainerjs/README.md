# runcontainer

JS helper library for running containers on browser, used for some examples in this project.

Container is expected to run in a web worker.

## Example

See [`./../../examples/no-conversion/`](./../../examples/no-conversion/) for running example.

This example uses xterm-pty for the terminal of the container.

Prepare container in the main worker:

```js
const worker = new Worker("./worker.js");
const vmImage = location.origin + "/out.wasm.gzip";
const mounterImage = location.origin + "/imagemounter.wasm.gzip";
const stackWorkerFile = location.origin + "/dist/stack-worker.js";
const containerImageAddress = getImageParam();
const infoP = RunContainer.createContainer(vmImage, containerImageAddress, stackWorkerFile, mounterImage);
infoP.then((info) => {
    worker.postMessage({type: "init", info: info, args: ['/bin/sh']});
    new TtyServer(slave).start(worker);
})
```

Start container in a worker:

```js
var ttyClient = new TtyClient(msg.data);
RunContainer.startContainer(info, args, ttyClient);
```

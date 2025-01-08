importScripts("https://cdn.jsdelivr.net/npm/xterm-pty@0.9.4/workerTools.js");
importScripts(location.origin + "/dist/worker-util.js");

var info;
var args;
onmessage = (msg) => {
    const req_ = msg.data;
    if ((typeof req_ == "object") && (req_.type == "init")) {
        info = req_.info;
        args = req_.args;
        return;
    }
    var ttyClient = new TtyClient(msg.data);
    RunContainer.startContainer(info, args, ttyClient);
};

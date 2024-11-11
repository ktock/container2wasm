importScripts("https://cdn.jsdelivr.net/npm/xterm-pty@0.9.4/workerTools.js");

onmessage = (msg) => {
  importScripts(location.origin + "/module.js"+location.search);
  importScripts(location.origin + "/out.js");

  var c = new TtyClient(msg.data);
  var old = c.onRead;
  c.onRead = function(length) {
      if (!length) length = 1;
      // NOTE: poll won't work if length > 1 because the current implentation of xterm-pty
      //       doesn't report the buffered contents via poll. (FIXME)
      return old.call(this, length);
  };
  emscriptenHack(c);
};

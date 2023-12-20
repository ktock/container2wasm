importScripts("https://cdn.jsdelivr.net/npm/xterm-pty@0.9.4/workerTools.js");

onmessage = (msg) => {
  importScripts(location.origin + "/module.js"+location.search);
  importScripts(location.origin + "/out.js");
  importScripts(location.origin + "/load.js");

  var c = new TtyClient(msg.data);
  var oldRead = c.onRead;
  c.onRead = function(length) {
      if (!length) length = 1;
      // NOTE: poll won't work if length > 1 because the current implentation of xterm-pty
      //       doesn't report the buffered contents via poll. (FIXME)
      return oldRead.call(this, length);
  };
  try {
      emscriptenHack(c);
  } catch (e) {
      console.log("patch error" + e);
  }
  var oldGetChar = TTY.default_tty_ops.get_char;
  TTY.default_tty_ops.get_char = function(){
      if (!c.onWaitForReadable(0)) {
          return null; // forciblly nonblocking; TODO: let emscripten handle this
      }
      return oldGetChar.call(this);
  }
};

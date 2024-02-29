#include <stdint.h>

// allow directly controlling fd for sockets, etc.
int32_t poll_oneoff(int32_t arg0, int32_t arg1, int32_t arg2, int32_t arg3) __attribute__((
    __import_module__("wasi_snapshot_preview1"),
    __import_name__("poll_oneoff")
));

int32_t __imported_wasi_snapshot_preview1_poll_oneoff(int32_t arg0, int32_t arg1, int32_t arg2, int32_t arg3) {
  return poll_oneoff(arg0, arg1, arg2, arg3);
}

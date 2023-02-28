/*
 * TinyEMU
 * 
 * Copyright (c) 2016-2018 Fabrice Bellard
 *
 * Permission is hereby granted, free of charge, to any person obtaining a copy
 * of this software and associated documentation files (the "Software"), to deal
 * in the Software without restriction, including without limitation the rights
 * to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
 * copies of the Software, and to permit persons to whom the Software is
 * furnished to do so, subject to the following conditions:
 *
 * The above copyright notice and this permission notice shall be included in
 * all copies or substantial portions of the Software.
 *
 * THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
 * IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
 * FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL
 * THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
 * LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
 * OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
 * THE SOFTWARE.
 */
#include <stdlib.h>
#include <stdio.h>
#include <string.h>

#include <wasi/libc.h>

#include "cutils.h"
#include "fs.h"
#include "wasi.h"

// Populating preopen in the same way as wasi-libc does as well as updating
// the preopens list managed by wasi-libc.
// https://github.com/WebAssembly/wasi-libc/blob/wasi-sdk-19/libc-bottom-half/sources/preopens.c
// We use our list of preopenes for mounting them to the container.

typedef struct preopen {
    /// The path prefix associated with the file descriptor.
    const char *prefix;

    /// The file descriptor.
    __wasi_fd_t fd;
} preopen;

preopen *preopens;
size_t num_preopens = 0;
size_t preopen_capacity = 0;

static int populate_preopens() {
  for (__wasi_fd_t fd = 3; fd != 0; ++fd) {
    __wasi_prestat_t prestat;
    __wasi_errno_t ret = __wasi_fd_prestat_get(fd, &prestat);
    if (ret == __WASI_ERRNO_BADF) {
      break;
    }
    if (ret != __WASI_ERRNO_SUCCESS)
      return -1;

    switch (prestat.tag) {
    case __WASI_PREOPENTYPE_DIR: {
      char *prefix = malloc(prestat.u.dir.pr_name_len + 1);
      if (prefix == NULL)
        return -1;
      if (__wasi_fd_prestat_dir_name(fd, (uint8_t *)prefix, prestat.u.dir.pr_name_len) != __WASI_ERRNO_SUCCESS)
        return -1;
      prefix[prestat.u.dir.pr_name_len] = '\0';

      int off = 0;
      while (1) {
        if (prefix[off] == '/') {
          off++;
        } else if (prefix[off] == '.' && prefix[off + 1] == '/') {
          off += 2;
        } else if (prefix[off] == '.' && prefix[off + 1] == 0) {
          off++;
        } else {
          break;
        }
      }

      char *p = &(prefix[off]);
      if (__wasilibc_register_preopened_fd(fd, strdup(p)) != 0)
        return -1;

      if (num_preopens == preopen_capacity) {
        size_t new_capacity = preopen_capacity == 0 ? 4 : preopen_capacity * 2;
        preopen *new_preopens = calloc(sizeof(preopen), new_capacity);
        if (new_preopens == NULL) {
          return -1;
        }
        memcpy(new_preopens, preopens, num_preopens * sizeof(preopen));
        free(preopens);
        preopens = new_preopens;
        preopen_capacity = new_capacity;
      }

      char *prefix2 = strdup(p);
      if (prefix2 == NULL)
        return -1;

      preopens[num_preopens++] = (preopen) { prefix2, fd, };
      free(prefix);

      break;
    }
    default:
      break;
    }
  }

  return 0;
}

int is_preopened(const char *path1) {
  for (int j = 0; j < num_preopens; j++) {
    if (strncmp(path1, preopens[j].prefix, strlen(path1)) == 0) {
      return TRUE;
    }
  }
  return FALSE;
}

int write_preopen_info(FSVirtFile *f, int pos1)
{
  int i, p, pos = pos1;
  for (i = 0; i < num_preopens; i++) {
    const char *fname;
    if (strcmp("pack", preopens[i].prefix) == 0) {
      continue; // ignore pack
    }
    if (strcmp("", preopens[i].prefix) == 0) {
      continue; // nop on root dir; do we need to support it?
    }
    fname = preopens[i].prefix;
    p = write_info(f, pos, 3, "m: ");
    if (p < 0) {
      return -1;
    }
    pos += p;
    for (int j = 0; j < strlen(fname); j++) {
      if (fname[j] == '\n') {
        if (putchar_info(f, pos++, '\\') != 1) {
          return -1;
        }
      }
      if (putchar_info(f, pos++, fname[j]) != 1) {
        return -1;
      }
    }
    if (putchar_info(f, pos++, '\n') != 1) {
      return -1;
    }
  }
  return pos - pos1;
}

extern void __wasi_vfs_rt_init(void);

int init_wasi()
{
    __wasi_vfs_rt_init(); // initialize wasi-vfs
    if (populate_preopens() != 0) { // register mapdir and wasi-vfs dir to wasi-libc and our list
      fprintf(stderr, "failed to populate preopens");
      return -1;
    }
    return 0;
}

// TODO: Remove this; This is a hack to passthrough poll_oneoff to wasi without being intercepted by wasi-vfs.
//       This is because wasi-vfs doesn't implement this but our only use-case of this is for polling stdin as of now
//       So we do not rely on that library for this feature. Once wasi-vfs supports pool_oneoff, remove this.
//       Ref: https://github.com/kateinoigakukun/wasi-vfs/blob/v0.2.0/src/wasi_snapshot_preview1.rs#L888-L895
int32_t poll_oneoff(int32_t arg0, int32_t arg1, int32_t arg2, int32_t arg3) __attribute__((
    __import_module__("wasi_snapshot_preview1"),
    __import_name__("poll_oneoff")
));
int32_t __imported_wasi_snapshot_preview1_poll_oneoff(int32_t arg0, int32_t arg1, int32_t arg2, int32_t arg3) {
  return poll_oneoff(arg0, arg1, arg2, arg3);
}

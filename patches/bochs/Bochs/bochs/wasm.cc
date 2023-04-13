/////////////////////////////////////////////////////////////////////////
// $Id$
/////////////////////////////////////////////////////////////////////////
//
//  Copyright (C) 2002-2021  The Bochs Project
//
//  This library is free software; you can redistribute it and/or
//  modify it under the terms of the GNU Lesser General Public
//  License as published by the Free Software Foundation; either
//  version 2 of the License, or (at your option) any later version.
//
//  This library is distributed in the hope that it will be useful,
//  but WITHOUT ANY WARRANTY; without even the implied warranty of
//  MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the GNU
//  Lesser General Public License for more details.
//
//  You should have received a copy of the GNU Lesser General Public
//  License along with this library; if not, write to the Free Software
//  Foundation, Inc., 51 Franklin St, Fifth Floor, Boston, MA  02110-1301 USA

/////////////////////////////////////////////////////////////////////////
// WASI-specific features
// - management for preopened dirs
// - virtio
// 
// VIRTIO implementation is ported from TinyEMU project:
/*
 * VIRTIO driver
 * 
 * Copyright (c) 2016 Fabrice Bellard
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

#include "iodev/iodev.h"

#if BX_SUPPORT_PCI

#include <assert.h>
#include <dirent.h>
#include "iodev/pci.h"
#include "wasm.h"

#ifdef EMSCRIPTEN
#include <emscripten.h>
#endif

#include <sys/socket.h>
#include <arpa/inet.h>
// not defined in wasi-libc
#ifdef WASI
#define PF_INET 1
#define PF_INET6 2
#endif

#define LOG_THIS genlog->

/////////////////////////////////////////////////////////////////////////
// Register mapdir device via viritio-9p
/////////////////////////////////////////////////////////////////////////
bx_virtio_9p_ctrl_c *wasi0;
bx_virtio_9p_ctrl_c *wasi1;
FSVirtFile *info = NULL;

FSVirtFile *get_vm_info()
{
  if (info == NULL)
    info = (FSVirtFile *)malloc(sizeof(FSVirtFile));
  return info;
}

FSDevice *fs_disk_init_with_info(const char *root_path, const char *info_path, FSVirtFile *info);
FSDevice *fs_disk_init(const char *root_path);

PLUGIN_ENTRY_FOR_MODULE(mapdirVirtio9p)
{
  if (mode == PLUGIN_INIT) {
    FSDevice *wasi0fs = fs_disk_init("");
    wasi0 = new bx_virtio_9p_ctrl_c(BX_PLUGIN_MAPDIR_VIRTIO_9P, "wasi0", wasi0fs);
    BX_REGISTER_DEVICE_DEVMODEL(plugin, type, wasi0, BX_PLUGIN_MAPDIR_VIRTIO_9P);
  } else if (mode == PLUGIN_FINI) {
    delete wasi0;
  } else if (mode == PLUGIN_PROBE) {
    return (int)PLUGTYPE_STANDARD;
  } else if (mode == PLUGIN_FLAGS) {
    return PLUGFLAG_PCI;
  }
  return 0; // Success
}

PLUGIN_ENTRY_FOR_MODULE(packVirtio9p)
{
  if (mode == PLUGIN_INIT) {
    FSDevice *wasi1fs = fs_disk_init_with_info("pack", "info", get_vm_info());
    wasi1 = new bx_virtio_9p_ctrl_c(BX_PLUGIN_PACK_VIRTIO_9P, "wasi1", wasi1fs);
    BX_REGISTER_DEVICE_DEVMODEL(plugin, type, wasi1, BX_PLUGIN_PACK_VIRTIO_9P);
  } else if (mode == PLUGIN_FINI) {
    delete wasi1;
  } else if (mode == PLUGIN_PROBE) {
    return (int)PLUGTYPE_STANDARD;
  } else if (mode == PLUGIN_FLAGS) {
    return PLUGFLAG_PCI;
  }
  return 0; // Success
}

bool init_start = false;
bool init_done = false;

#define INIT_STR_POS_MAX 10
int init_str_pos = 0;
char init_str_buf[INIT_STR_POS_MAX];

static void console_write(void *opaque, const uint8_t *buf, int len)
{
    int off = 0;
    if (!init_start) {
      for (int i = 0; i < len; i++) {
        if (buf[i] == '=') {
          init_str_buf[init_str_pos++] = buf[i];
          off++;
          if (init_str_pos == INIT_STR_POS_MAX) {
            // NOTE: init string isn't printed
            init_start = true;
            SIM->get_param_bool(BXPN_WASM_INITDONE)->set(1);
            break;
          }
        } else {
          // flush all and reset counter
          fwrite(init_str_buf, 1, init_str_pos, stdout);
          init_str_pos = 0;
        }
      }
    }
    fwrite(buf+off, 1, len-off, stdout);
    fflush(stdout);
}

static int console_read_stdin(void *opaque, uint8_t *buf, int len)
{
    STDIODevice *s = (STDIODevice *)opaque;
    int ret, i, j;
    uint8_t ch;

    if (len <= 0)
        return 0;

    ret = read(s->stdin_fd, buf, len);
    if (ret < 0)
        return 0;
    if (ret == 0) {
        /* EOF */
        exit(1);
    }

    j = 0;
    for(i = 0; i < ret; i++) {
        ch = buf[i];
        if (s->console_esc_state) {
            s->console_esc_state = 0;
            switch(ch) {
            case 'x':
                printf("Terminated\n");
                exit(0);
            case 'h':
                printf("\n"
                       "C-a h   print this help\n"
                       "C-a x   exit emulator\n"
                       "C-a C-a send C-a\n"
                       );
                break;
            case 1:
                goto output_char;
            default:
                break;
            }
        } else {
            if (ch == 1) {
                s->console_esc_state = 1;
            } else {
            output_char:
                buf[j++] = ch;
            }
        }
    }
    return j;
}

const char *init_done_str = "=\n";
int init_done_str_pos = 0;

static inline int min_int(int a, int b);

static int console_read(void *opaque, uint8_t *buf, int len)
{
  if ((init_start) && (!init_done)) {
    int l = min_int(strlen(init_done_str) - init_done_str_pos, len);
    memcpy(buf, init_done_str + init_done_str_pos, l);
    init_done_str_pos += l;
    if (init_done_str_pos >= strlen(init_done_str)) {
      init_done = true;
    }
    return l;
  }
  return console_read_stdin(opaque, buf, len);
}

void *mallocz(size_t size);

CharacterDevice *console_init()
{
    CharacterDevice *dev;
    STDIODevice *s;

    dev = (CharacterDevice *)mallocz(sizeof(*dev));
    s = (STDIODevice *)mallocz(sizeof(*s));
    s->stdin_fd = 0;
    /* Note: the glibc does not properly tests the return value of
       write() in printf, so some messages on stdout may be lost */
    fcntl(s->stdin_fd, F_SETFL, O_NONBLOCK);

    s->resize_pending = true;
    
    dev->opaque = s;
    dev->write_data = console_write;
    dev->read_data = console_read;
    return dev;
}

bx_virtio_console_ctrl_c *stdio_v;

PLUGIN_ENTRY_FOR_MODULE(stdioVirtioConsole)
{
  if (mode == PLUGIN_INIT) {
    CharacterDevice *cs = console_init();
    stdio_v = new bx_virtio_console_ctrl_c(BX_PLUGIN_STDIO_VIRTIO_CONSOLE, cs);
    BX_REGISTER_DEVICE_DEVMODEL(plugin, type, stdio_v, BX_PLUGIN_STDIO_VIRTIO_CONSOLE);
  } else if (mode == PLUGIN_FINI) {
    delete stdio_v;
  } else if (mode == PLUGIN_PROBE) {
    return (int)PLUGTYPE_STANDARD;
  } else if (mode == PLUGIN_FLAGS) {
    return PLUGFLAG_PCI;
  }
  return 0; // Success
}

bx_virtio_net_ctrl_c *qemu0;
EthernetDevice *qemu_net;
static EthernetDevice *qemu_net_init();

PLUGIN_ENTRY_FOR_MODULE(qemuVirtioNet)
{
  if (mode == PLUGIN_INIT) {
    qemu_net = qemu_net_init();
    qemu0 = new bx_virtio_net_ctrl_c(BX_PLUGIN_QEMU_VIRTIO_NET, qemu_net);
    BX_REGISTER_DEVICE_DEVMODEL(plugin, type, qemu0, BX_PLUGIN_QEMU_VIRTIO_NET);
  } else if (mode == PLUGIN_FINI) {
    delete qemu0;
  } else if (mode == PLUGIN_PROBE) {
    return (int)PLUGTYPE_STANDARD;
  } else if (mode == PLUGIN_FLAGS) {
    return PLUGFLAG_PCI;
  }
  return 0; // Success
}


/////////////////////////////////////////////////////////////////////////
// WASI preopens
/////////////////////////////////////////////////////////////////////////
int write_info(FSVirtFile *f, int pos, int len, const char *str)
{
  if ((pos + len) > f->lim) {
    printf("too many write (%d > %d)", pos + len, f->lim);
    return -1;
  }
  for (int i = 0; i < len; i++) {
    f->contents[pos + i] = str[i];
  }
  return len;
}

int putchar_info(FSVirtFile *f, int pos, char c)
{
  if ((pos + 1) > f->lim) {
    printf("too many write (%d > %d)", pos + 1, f->lim);
    return -1;
  }
  f->contents[pos] = c;
  return 1;
}

#ifdef WASI
#include <wasi/libc.h>

preopen *preopens;
size_t num_preopens = 0;
size_t preopen_capacity = 0;

int populate_preopens() {
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
      char *prefix = (char *) malloc(prestat.u.dir.pr_name_len + 1);
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
        preopen *new_preopens = (preopen *)calloc(sizeof(preopen), new_capacity);
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
      return true;
    }
  }
  return false;
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
#endif

/////////////////////////////////////////////////////////////////////////
// Utilities
/////////////////////////////////////////////////////////////////////////
void *mallocz(size_t size)
{
    void *ptr;
    ptr = malloc(size);
    if (!ptr)
        return NULL;
    memset(ptr, 0, size);
    return ptr;
}

static inline int max_int(int a, int b)
{
    if (a > b)
        return a;
    else
        return b;
}

static inline int min_int(int a, int b)
{
    if (a < b)
        return a;
    else
        return b;
}

#define countof(x) (sizeof(x) / sizeof(x[0]))

static inline void put_le16(Bit8u *ptr, Bit16u v)
{
    ptr[0] = v;
    ptr[1] = v >> 8;
}

static inline void put_le32(Bit8u *ptr, Bit32u v)
{
    ptr[0] = v;
    ptr[1] = v >> 8;
    ptr[2] = v >> 16;
    ptr[3] = v >> 24;
}

static inline void put_le64(Bit8u *ptr, Bit64u v)
{
    put_le32(ptr, v);
    put_le32(ptr + 4, v >> 32);
}

static void set_low32(virtio_phys_addr_t *paddr, Bit32u val)
{
    *paddr = (*paddr & ~(virtio_phys_addr_t)0xffffffff) | val;
}

static void set_high32(virtio_phys_addr_t *paddr, Bit32u val)
{
    *paddr = (*paddr & 0xffffffff) | ((virtio_phys_addr_t)val << 32);
}

static inline Bit16u get_le16(const Bit8u *ptr)
{
    return ptr[0] | (ptr[1] << 8);
}

static inline Bit32u get_le32(const Bit8u *ptr)
{
    return ptr[0] | (ptr[1] << 8) | (ptr[2] << 16) | (ptr[3] << 24);
}

static inline Bit64u get_le64(const Bit8u *ptr)
{
    return get_le32(ptr) | ((uint64_t)get_le32(ptr + 4) << 32);
}

/////////////////////////////////////////////////////////////////////////
// Filesystem
/////////////////////////////////////////////////////////////////////////
static void fs_close(FSDevice *fs, FSFile *f);

static void fs_delete(FSDevice *fs, FSFile *f)
{
    if (f->is_opened)
        fs_close(fs, f);
    free(f->path);
    free(f);
}

/* warning: path belong to fid_create() */
static FSFile *fid_create(FSDevice *s1, char *path, uint32_t uid)
{
    FSFile *f;
    f = (FSFile *)mallocz(sizeof(*f));
    f->path = path;
    f->uid = uid;
    return f;
}

static int errno_table[][2] = {
    { P9_EPERM, EPERM },
    { P9_ENOENT, ENOENT },
    { P9_EIO, EIO },
    { P9_EEXIST, EEXIST },
    { P9_EINVAL, EINVAL },
    { P9_ENOSPC, ENOSPC },
    { P9_ENOTEMPTY, ENOTEMPTY },
    { P9_EPROTO, EPROTO },
    { P9_ENOTSUP, ENOTSUP },
};

static int errno_to_p9(int err)
{
    int i;
    if (err == 0)
        return 0;
    for(i = 0; i < countof(errno_table); i++) {
        if (err == errno_table[i][1])
            return errno_table[i][0];
    }
    return P9_EINVAL;
}

static int open_flags[][2] = {
    { P9_O_CREAT, O_CREAT },
    { P9_O_EXCL, O_EXCL },
    //    { P9_O_NOCTTY, O_NOCTTY },
    { P9_O_TRUNC, O_TRUNC },
    { P9_O_APPEND, O_APPEND },
    { P9_O_NONBLOCK, O_NONBLOCK },
    { P9_O_DSYNC, O_DSYNC },
    //    { P9_O_FASYNC, O_FASYNC },
    //    { P9_O_DIRECT, O_DIRECT },
    //    { P9_O_LARGEFILE, O_LARGEFILE },
    //    { P9_O_DIRECTORY, O_DIRECTORY },
    { P9_O_NOFOLLOW, O_NOFOLLOW },
    //    { P9_O_NOATIME, O_NOATIME },
    //    { P9_O_CLOEXEC, O_CLOEXEC },
    { P9_O_SYNC, O_SYNC },
};

static int p9_flags_to_host(int flags)
{
    int ret, i;

    // We can't do this for WASI because
    // O_RDONLY, O_WRONLY, O_RDWR values are different from 9p.
    // ret = (flags & P9_O_NOACCESS);
    ret = 0;
    if ((flags & P9_O_NOACCESS) == P9_O_RDONLY) {
      ret |= O_RDONLY;
    } else if ((flags & P9_O_NOACCESS) == P9_O_WRONLY) {
      ret |= O_WRONLY;
    } else if ((flags & P9_O_NOACCESS) == P9_O_RDWR) {
      ret |= O_RDWR;
    }
    for(i = 0; i < countof(open_flags); i++) {
        if (flags & open_flags[i][0])
            ret |= open_flags[i][1];
    }
    return ret;
}

static mode_t host_mode_to_p9(mode_t m) {
    mode_t ret = 0;
    ret = m & 0700;
    if (S_ISBLK(m)) {
      ret |= P9_S_IFDIR;
    } else if (S_ISCHR(m)) {
      ret |= P9_S_IFCHR;
    } else if (S_ISDIR(m)) {
      ret |= P9_S_IFDIR;
    } else if (S_ISFIFO(m)) {
      ret |= P9_S_IFIFO;
    } else if (S_ISLNK(m)) {
      ret |= P9_S_IFLNK;
    } else if (S_ISREG(m)) {
      ret |= P9_S_IFREG;
    } else if (S_ISSOCK(m)) {
      ret |= P9_S_IFSOCK;
    }
    // TODO: support suid, sgid, sticky bits
    return ret;
}

static void stat_to_qid(FSQID *qid, const struct stat *st)
{
    if (S_ISDIR(st->st_mode))
        qid->type = P9_QTDIR;
    else if (S_ISLNK(st->st_mode))
        qid->type = P9_QTSYMLINK;
    else
        qid->type = P9_QTFILE;
    qid->version = 0; /* no caching on client */
    qid->path = st->st_ino;
}

static void fs_statfs(FSDevice *fs1, FSStatFS *st)
{
    //FSDeviceDisk *fs = (FSDeviceDisk *)fs1;
    //struct statfs st1;
    //statfs(fs->root_path, &st1);
    //st->f_bsize = st1.f_bsize;
    //st->f_blocks = st1.f_blocks;
    //st->f_bfree = st1.f_bfree;
    //st->f_bavail = st1.f_bavail;
    //st->f_files = st1.f_files;
    //st->f_ffree = st1.f_ffree;

    //st->f_bsize = st1.f_bsize;
    //st->f_blocks = st1.f_blocks;
    //st->f_bfree = st1.f_bfree;
    //st->f_bavail = st1.f_bavail;
    //st->f_files = st1.f_files;
    //st->f_ffree = st1.f_ffree;

}

static char *compose_path(const char *path, const char *name)
{
    int path_len, name_len;
    char *d;

    path_len = strlen(path);
    name_len = strlen(name);
    d = (char *)malloc(path_len + 1 + name_len + 1);
    memcpy(d, path, path_len);
    d[path_len] = '/';
    memcpy(d + path_len + 1, name, name_len + 1);
    return d;
}

static int is_infofile(FSDevice *fs1, char *path) {
    FSDeviceDisk *fs = (FSDeviceDisk *)fs1;
    if (fs->info_path == NULL)
        return false;
    int cmp = strcmp(path, fs->info_path);
    return cmp == 0;
}

static int fs_attach(FSDevice *fs1, FSFile **pf,
                     FSQID *qid, uint32_t uid,
                     const char *uname, const char *aname)
{
    FSDeviceDisk *fs = (FSDeviceDisk *)fs1;
    struct stat st;
    FSFile *f;

    if (strcmp(fs->root_path, "") == 0) {
      qid->type = P9_QTDIR;
      qid->version = 0; /* no caching on client */
      qid->path = 0;
      FSFile *f;
      f = (FSFile *)mallocz(sizeof(*f));
      f->path = "";
      f->uid = uid;
      *pf =f;
      return 0;
    }
    if (lstat(fs->root_path, &st) != 0) {
        *pf = NULL;
        return -errno_to_p9(errno);
    }
    f = fid_create(fs1, strdup(fs->root_path), uid);
    stat_to_qid(qid, &st);
    *pf = f;
    return 0;
}

#ifdef WASI
static char *root_relative_path(const char *path1)
{
  int off = 0;
  char *path = strdup(path1);
  while (1) {
    if (path[off] == '/') {
      off++;
    } else if (path[off] == '.' && path[off + 1] == '/') {
      off += 2;
    } else if (path[off] == '.' && path[off + 1] == 0) {
      off++;
    } else {
      break;
    }
  }
  return &(path[off]);
}
#endif

static int fs_walk(FSDevice *fs, FSFile **pf, FSQID *qids,
                   FSFile *f, int n, char **names)
{
    char *path, *path1;
    struct stat st;
    int i;

    path = strdup(f->path);
    for(i = 0; i < n; i++) {
        path1 = compose_path(path, names[i]);
        int is_preopen = false;
#ifdef WASI
        is_preopen = is_preopened(root_relative_path(path1));
#endif
        if ((lstat(path1, &st) != 0) && !is_infofile(fs, path1) && !is_preopen) {
            free(path1);
            break;
        }
        free(path);
        path = path1;
        if (is_infofile(fs, path1)) {
            qids[i].type = P9_QTFILE;
            qids[i].version = 0;
            qids[i].path = 0;
        } else if (is_preopen) {
            qids[i].type = P9_QTDIR;
            qids[i].version = 0;
            qids[i].path = 0;
        } else {
            stat_to_qid(&qids[i], &st);
        }
    }
    *pf = fid_create(fs, path, f->uid);
    return i;
}


static int fs_mkdir(FSDevice *fs, FSQID *qid, FSFile *f,
                    const char *name, uint32_t mode, uint32_t gid)
{
    char *path;
    struct stat st;
    
    path = compose_path(f->path, name);
    if (mkdir(path, mode) < 0) {
        free(path);
        return -errno_to_p9(errno);
    }
    if (lstat(path, &st) != 0) {
        free(path);
        return -errno_to_p9(errno);
    }
    free(path);
    stat_to_qid(qid, &st);
    return 0;
}

static int fs_open(FSDevice *fs, FSQID *qid, FSFile *f, uint32_t flags, void *opaque)
{
    struct stat st;
    fs_close(fs, f);

    if ((stat(f->path, &st) != 0) && !is_infofile(fs, f->path))
        return -errno_to_p9(errno);
    stat_to_qid(qid, &st);

    if (flags & P9_O_DIRECTORY) {
        DIR *dirp;
        dirp = opendir(f->path);
        if (!dirp)
            return -errno_to_p9(errno);
        f->is_opened = true;
        f->is_dir = true;
        closedir(dirp); // We don't use this now. (see fs_readdir)
        //f->u.dirp = dirp;
    } else if (is_infofile(fs, f->path)) {
        f->is_opened = true;
        f->is_dir = false;
    } else {
        int fd;
        fd = open(f->path, p9_flags_to_host(flags) & ~O_CREAT);
        if (fd < 0) {
            return -errno_to_p9(errno);
        }
        f->is_opened = true;
        f->is_dir = false;
        f->u.fd = fd;
    }
    return 0;
}

static int fs_create(FSDevice *fs, FSQID *qid, FSFile *f, const char *name, 
                     uint32_t flags, uint32_t mode, uint32_t gid)
{
    struct stat st;
    char *path;
    int ret, fd;

    fs_close(fs, f);
    
    path = compose_path(f->path, name);
    fd = open(path, p9_flags_to_host(flags) | O_CREAT, mode);
    if (fd < 0) {
        free(path);
        return -errno_to_p9(errno);
    }
    ret = lstat(path, &st);
    if (ret != 0) {
        free(path);
        close(fd);
        return -errno_to_p9(errno);
    }
    free(f->path);
    f->path = path;
    f->is_opened = true;
    f->is_dir = false;
    f->u.fd = fd;
    stat_to_qid(qid, &st);
    return 0;
}

static int fs_readdir(FSDevice *fs, FSFile *f, uint64_t offset,
                      uint8_t *buf, int count)
{
    struct dirent *de;
    int len, pos, name_len, type, d_type;

    if (!f->is_opened || !f->is_dir)
        return -P9_EPROTO;

    // WASI doesn't support rewinding and seeking back directory entries.
    // So here we reopen the directory everytime this function is called.
    DIR *dirp;
    dirp = opendir(f->path);
    if (!dirp)
        return -errno_to_p9(errno);
    for(int i = 0; i < offset; i++) {
        de = readdir(dirp);
        if (de == NULL)
          break;
    }
    pos = 0;
    for(;;) {
        de = readdir(dirp);
        if (de == NULL)
            break;
        name_len = strlen(de->d_name);
        len = 13 + 8 + 1 + 2 + name_len;
        if ((pos + len) > count)
            break;
        offset = telldir(dirp);
        d_type = de->d_type;
        if (d_type == DT_UNKNOWN) {
            char *path;
            struct stat st;
            path = compose_path(f->path, de->d_name);
            if (lstat(path, &st) == 0) {
                d_type = st.st_mode >> 12;
            } else {
                d_type = DT_REG; /* default */
            }
            free(path);
        }
        if (d_type == DT_DIR)
            type = P9_QTDIR;
        else if (d_type == DT_LNK)
            type = P9_QTSYMLINK;
        else
            type = P9_QTFILE;
        buf[pos++] = type;
        put_le32(buf + pos, 0); /* version */
        pos += 4;
        put_le64(buf + pos, de->d_ino);
        pos += 8;
        put_le64(buf + pos, offset);
        pos += 8;
        buf[pos++] = d_type;
        put_le16(buf + pos, name_len);
        pos += 2;
        memcpy(buf + pos, de->d_name, name_len);
        pos += name_len;
    }
    closedir(dirp);
    return pos;
}

static int fs_read(FSDevice *fs, FSFile *f, uint64_t offset,
                   uint8_t *buf, int count)
{
    int ret;

    if (!f->is_opened || f->is_dir)
        return -P9_EPROTO;

    if (is_infofile(fs, f->path)) {
        FSDeviceDisk *fs1 = (FSDeviceDisk *)fs;
        if (offset > fs1->info->len)
            offset = fs1->info->len;
        if (count > (fs1->info->len - offset))
            count = fs1->info->len - offset;
        memcpy(buf, fs1->info->contents + offset, count);
        return count;
    }

    ret = pread(f->u.fd, buf, count, offset);
    if (ret < 0) 
        return -errno_to_p9(errno);
    else
        return ret;
}

static int fs_write(FSDevice *fs, FSFile *f, uint64_t offset,
                    const uint8_t *buf, int count)
{
    int ret;

    if (!f->is_opened || f->is_dir)
        return -P9_EPROTO;
    ret = pwrite(f->u.fd, buf, count, offset);
    if (ret < 0) 
        return -errno_to_p9(errno);
    else
        return ret;
}

static void fs_close(FSDevice *fs, FSFile *f)
{
    if (!f->is_opened)
        return;
    if (f->is_dir) {
      //closedir(f->u.dirp);
    }
    else {
      if (f->u.fd > 0)
        close(f->u.fd);
    }
    f->is_opened = false;
}

static int fs_stat(FSDevice *fs, FSFile *f, FSStat *st)
{
    struct stat st1;
    int is_preopen = false;

    if (is_infofile(fs, f->path)) {
        FSDeviceDisk *fs1 = (FSDeviceDisk *)fs;
        st->qid.type = P9_QTFILE;
        st->qid.version = 0;
        st->qid.path = 0;
        st->st_mode = 0777 | P9_S_IFREG;
        st->st_size = fs1->info->len;
        st->st_blksize = 4096;
        st->st_blocks = fs1->info->len / 4096 + 1;
        return 0;
    }
    if (strcmp(f->path, "") == 0) {
        st->qid.type = P9_QTDIR;
        st->qid.version = 0;
        st->qid.path = 0;
        st->st_mode = P9_S_IFDIR;
        return 0;
    }
#ifdef WASI
    is_preopen = is_preopened(root_relative_path(f->path));
#endif
    if (is_preopen) {
        st->qid.type = P9_QTDIR;
        st->qid.version = 0;
        st->qid.path = 0;
        st->st_mode = P9_S_IFDIR;
        st->st_blocks = 0;
        st->st_blksize = 4096;
        st->st_size = 0;
        st->st_atime_sec = 0;
        st->st_atime_nsec = 0;
        st->st_mtime_sec = 0;
        st->st_mtime_nsec = 0;
        st->st_ctime_sec = 0;
        st->st_ctime_nsec = 0;
        st->st_uid = 0;
        st->st_gid = 0;
        st->st_nlink = 1;
        return 0;
    }
    if (lstat(f->path, &st1) != 0) {
        return -P9_ENOENT;
    }
    stat_to_qid(&st->qid, &st1);
    st->st_mode = host_mode_to_p9(st1.st_mode);
    st->st_uid = st1.st_uid;
    st->st_gid = st1.st_gid;
    st->st_nlink = st1.st_nlink;
    st->st_rdev = st1.st_rdev;
    st->st_size = st1.st_size;
    st->st_blksize = st1.st_blksize;
    st->st_blocks = st1.st_blocks;
    st->st_atime_sec = st1.st_atim.tv_sec;
    st->st_atime_nsec = st1.st_atim.tv_nsec;
    st->st_mtime_sec = st1.st_mtim.tv_sec;
    st->st_mtime_nsec = st1.st_mtim.tv_nsec;
    st->st_ctime_sec = st1.st_ctim.tv_sec;
    st->st_ctime_nsec = st1.st_ctim.tv_nsec;
    return 0;
}

static int fs_setattr(FSDevice *fs, FSFile *f, uint32_t mask,
                      uint32_t mode, uint32_t uid, uint32_t gid,
                      uint64_t size, uint64_t atime_sec, uint64_t atime_nsec,
                      uint64_t mtime_sec, uint64_t mtime_nsec)
{
    bool ctime_updated = false;

    if (mask & (P9_SETATTR_UID | P9_SETATTR_GID)) {
        return P9_ENOTSUP; // TODO
//        if (lchown(f->path, (mask & P9_SETATTR_UID) ? uid : -1,
//                   (mask & P9_SETATTR_GID) ? gid : -1) < 0)
//            return -errno_to_p9(errno);
//        ctime_updated = true;
    }
    /* must be done after uid change for suid */
    if (mask & P9_SETATTR_MODE) {
        return P9_ENOTSUP; // TODO
//        if (chmod(f->path, mode) < 0)
//            return -errno_to_p9(errno);
//        ctime_updated = true;
    }
    if (mask & P9_SETATTR_SIZE) {
        if (truncate(f->path, size) < 0)
            return -errno_to_p9(errno);
        ctime_updated = true;
    }
    if (mask & (P9_SETATTR_ATIME | P9_SETATTR_MTIME)) {
        struct timespec ts[2];
        if (mask & P9_SETATTR_ATIME) {
            if (mask & P9_SETATTR_ATIME_SET) {
                ts[0].tv_sec = atime_sec;
                ts[0].tv_nsec = atime_nsec;
            } else {
                ts[0].tv_sec = 0;
                ts[0].tv_nsec = UTIME_NOW;
            }
        } else {
            ts[0].tv_sec = 0;
            ts[0].tv_nsec = UTIME_OMIT;
        }
        if (mask & P9_SETATTR_MTIME) {
            if (mask & P9_SETATTR_MTIME_SET) {
                ts[1].tv_sec = mtime_sec;
                ts[1].tv_nsec = mtime_nsec;
            } else {
                ts[1].tv_sec = 0;
                ts[1].tv_nsec = UTIME_NOW;
            }
        } else {
            ts[1].tv_sec = 0;
            ts[1].tv_nsec = UTIME_OMIT;
        }
        if (utimensat(AT_FDCWD, f->path, ts, AT_SYMLINK_NOFOLLOW) < 0)
            return -errno_to_p9(errno);
        ctime_updated = true;
    }
    if ((mask & P9_SETATTR_CTIME) && !ctime_updated) {
        return P9_ENOTSUP; // TODO
//        if (lchown(f->path, -1, -1) < 0)
//            return -errno_to_p9(errno);
    }
    return 0;
}

static int fs_link(FSDevice *fs, FSFile *df, FSFile *f, const char *name)
{
    char *path;
    
    path = compose_path(df->path, name);
    if (link(f->path, path) < 0) {
        free(path);
        return -errno_to_p9(errno);
    }
    free(path);
    return 0;
}

static int fs_symlink(FSDevice *fs, FSQID *qid,
                      FSFile *f, const char *name, const char *symgt, uint32_t gid)
{
    char *path;
    struct stat st;
    
    path = compose_path(f->path, name);
    if (symlink(symgt, path) < 0) {
        free(path);
        return -errno_to_p9(errno);
    }
    if (lstat(path, &st) != 0) {
        free(path);
        return -errno_to_p9(errno);
    }
    free(path);
    stat_to_qid(qid, &st);
    return 0;
}

static int fs_mknod(FSDevice *fs, FSQID *qid,
             FSFile *f, const char *name, uint32_t mode, uint32_t major,
             uint32_t minor, uint32_t gid)
{
    return P9_ENOTSUP; // TODO
//    char *path;
//    struct stat st;
//    
//    path = compose_path(f->path, name);
//    if (mknod(path, mode, makedev(major, minor)) < 0) {
//        free(path);
//        return -errno_to_p9(errno);
//    }
//    if (lstat(path, &st) != 0) {
//        free(path);
//        return -errno_to_p9(errno);
//    }
//    free(path);
//    stat_to_qid(qid, &st);
//    return 0;
}

static int fs_readlink(FSDevice *fs, char *buf, int buf_size, FSFile *f)
{
    int ret;
    ret = readlink(f->path, buf, buf_size - 1);
    if (ret < 0)
        return -errno_to_p9(errno);
    buf[ret] = '\0';
    return 0;
}

static int fs_renameat(FSDevice *fs, FSFile *f, const char *name, 
                FSFile *new_f, const char *new_name)
{
    char *path, *new_path;
    int ret;

    path = compose_path(f->path, name);
    new_path = compose_path(new_f->path, new_name);
    ret = rename(path, new_path);
    free(path);
    free(new_path);
    if (ret < 0)
        return -errno_to_p9(errno);
    return 0;
}

static int fs_unlinkat(FSDevice *fs, FSFile *f, const char *name)
{
    char *path;
    int ret;

    path = compose_path(f->path, name);
    ret = remove(path);
    free(path);
    if (ret < 0)
        return -errno_to_p9(errno);
    return 0;
    
}

static int fs_lock(FSDevice *fs, FSFile *f, const FSLock *lock)
{
    return P9_ENOTSUP; // TODO
//    int ret;
//    struct flock fl;
//    
//    /* XXX: lock directories too */
//    if (!f->is_opened || f->is_dir)
//        return -P9_EPROTO;
//
//    fl.l_type = lock->type;
//    fl.l_whence = SEEK_SET;
//    fl.l_start = lock->start;
//    fl.l_len = lock->length;
//
//    ret = fcntl(f->u.fd, F_SETLK, &fl);
//    if (ret == 0) {
//        ret = P9_LOCK_SUCCESS;
//    } else if (errno == EAGAIN || errno == EACCES) {
//        ret = P9_LOCK_BLOCKED;
//    } else {
//        ret = -errno_to_p9(errno);
//    }
//    return ret;
}

static int fs_getlock(FSDevice *fs, FSFile *f, FSLock *lock)
{
    return P9_ENOTSUP; // TODO
//    int ret;
//    struct flock fl;
//    
//    /* XXX: lock directories too */
//    if (!f->is_opened || f->is_dir)
//        return -P9_EPROTO;
//
//    fl.l_type = lock->type;
//    fl.l_whence = SEEK_SET;
//    fl.l_start = lock->start;
//    fl.l_len = lock->length;
//
//    ret = fcntl(f->u.fd, F_GETLK, &fl);
//    if (ret < 0) {
//        ret = -errno_to_p9(errno);
//    } else {
//        lock->type = fl.l_type;
//        lock->start = fl.l_start;
//        lock->length = fl.l_len;
//    }
//    return ret;
}

static void fs_disk_end(FSDevice *fs1)
{
    FSDeviceDisk *fs = (FSDeviceDisk *)fs1;
    free(fs->root_path);
}

FSDevice *fs_disk_init_with_info(const char *root_path, const char *info_path, FSVirtFile *info)
{
    FSDeviceDisk *fs;
    struct stat st;

    if (strcmp(root_path, "") != 0) {
        lstat(root_path, &st);
        if (!S_ISDIR(st.st_mode))
            return NULL;
    }

    fs = (FSDeviceDisk *)mallocz(sizeof(*fs));

    fs->common.fs_end = fs_disk_end;
    fs->common.fs_delete = fs_delete;
    fs->common.fs_statfs = fs_statfs;
    fs->common.fs_attach = fs_attach;
    fs->common.fs_walk = fs_walk;
    fs->common.fs_mkdir = fs_mkdir;
    fs->common.fs_open = fs_open;
    fs->common.fs_create = fs_create;
    fs->common.fs_stat = fs_stat;
    fs->common.fs_setattr = fs_setattr;
    fs->common.fs_close = fs_close;
    fs->common.fs_readdir = fs_readdir;
    fs->common.fs_read = fs_read;
    fs->common.fs_write = fs_write;
    fs->common.fs_link = fs_link;
    fs->common.fs_symlink = fs_symlink;
    fs->common.fs_mknod = fs_mknod;
    fs->common.fs_readlink = fs_readlink;
    fs->common.fs_renameat = fs_renameat;
    fs->common.fs_unlinkat = fs_unlinkat;
    fs->common.fs_lock = fs_lock;
    fs->common.fs_getlock = fs_getlock;
    
    fs->root_path = strdup(root_path);
    if ((info_path != NULL) && (info != NULL)) {
        fs->info_path = compose_path(root_path, info_path);
        fs->info = info;
    }
    return (FSDevice *)fs;
}

FSDevice *fs_disk_init(const char *root_path)
{
  return fs_disk_init_with_info(root_path, NULL, NULL);
}

/////////////////////////////////////////////////////////////////////////
// Virtio-PCI
/////////////////////////////////////////////////////////////////////////
Bit16u virtio_read16(virtio_phys_addr_t addr)
{
  Bit8u buffer[2];
  DEV_MEM_READ_PHYSICAL_DMA(addr, 2, buffer);
  return get_le16(buffer);
}

void virtio_write16(virtio_phys_addr_t addr, Bit16u val)
{
  DEV_MEM_WRITE_PHYSICAL_DMA(addr, 2, (Bit8u *)&val);
}

void virtio_write32(virtio_phys_addr_t addr, Bit32u val)
{
  DEV_MEM_WRITE_PHYSICAL_DMA(addr, 4, (Bit8u *)&val);
}

bx_virtio_ctrl_c::bx_virtio_ctrl_c()
{
  put("VIRTIO");
  memset(&s, 0, sizeof(s));
}

bx_virtio_ctrl_c::~bx_virtio_ctrl_c()
{
  SIM->get_bochs_root()->remove("virtio");
  BX_DEBUG(("Exit"));
}

int bx_virtio_ctrl_c::pci_add_capability(Bit8u *buf, int size)
{
    int offset;
    
    offset = BX_VIRTIO_THIS s.next_cap_offset;
    if ((offset + size) > 256)
        return -1;
    BX_VIRTIO_THIS s.next_cap_offset += size;
    BX_VIRTIO_THIS pci_conf[PCI_STATUS] |= PCI_STATUS_CAP_LIST;
    memcpy(BX_VIRTIO_THIS pci_conf + offset, buf, size);
    BX_VIRTIO_THIS pci_conf[offset + 1] = BX_VIRTIO_THIS pci_conf[PCI_CAPABILITY_LIST];
    BX_VIRTIO_THIS pci_conf[PCI_CAPABILITY_LIST] = offset;
    return offset;
}

void bx_virtio_ctrl_c::virtio_add_pci_capability(int cfg_type, Bit8u bar, Bit32u offset, Bit32u len, Bit32u mult)
{
    Bit8u cap[20];
    int cap_len;
    if (cfg_type == 2)
        cap_len = 20;
    else
        cap_len = 16;
    memset(cap, 0, cap_len);
    cap[0] = 0x09; /* vendor specific */
    cap[2] = cap_len; /* set by pci_add_capability() */
    cap[3] = cfg_type;
    cap[4] = bar;
    put_le32(cap + 8, offset);
    put_le32(cap + 12, len);
    if (cfg_type == 2)
        put_le32(cap + 16, mult);
    pci_add_capability(cap, cap_len);
}

void bx_virtio_ctrl_c::init(char *plugin_name, Bit16u pci_device_id, Bit32u class_id, Bit16u device_id, Bit32u device_features, const char *descr)
{
  BX_VIRTIO_THIS s.next_cap_offset = 0x40;
  BX_VIRTIO_THIS s.devfunc = 0;
  BX_VIRTIO_THIS s.device_features = device_features;
  DEV_register_pci_handlers(this, &BX_VIRTIO_THIS s.devfunc, plugin_name, descr);

  init_pci_conf(0x1af4,pci_device_id,0x00,class_id << 8,0,1);

  Bit8u bar_num = 0;
  virtio_add_pci_capability(1, bar_num,
                            VIRTIO_PCI_CFG_OFFSET, 0x1000, 0); /* common */
  virtio_add_pci_capability(3, bar_num,
                            VIRTIO_PCI_ISR_OFFSET, 0x1000, 0); /* isr */
  virtio_add_pci_capability(4, bar_num,
                            VIRTIO_PCI_CONFIG_OFFSET, 0x1000, 0); /* config */
  virtio_add_pci_capability(2, bar_num,
                            VIRTIO_PCI_NOTIFY_OFFSET, 0x1000, 0); /* notify */
  init_bar_mem(bar_num, 0x4000, mem_read_handler, mem_write_handler);
  put_le16(BX_VIRTIO_THIS pci_conf + 0x2c, 0x1af4);
  put_le16(BX_VIRTIO_THIS pci_conf + 0x2e, device_id);
  virtio_reset();
}

void bx_virtio_ctrl_c::virtio_config_write(Bit32u offset, Bit32u val, unsigned len)
{
    switch(len) {
    case 1:
        if (offset < BX_VIRTIO_THIS config_space_size) {
            BX_VIRTIO_THIS config_space[offset] = val;
            // if (BX_VIRTIO_THIS s.config_write)
            //     BX_VIRTIO_THIS s.config_write(s);
        }
        break;
    case 2:
        if (offset < BX_VIRTIO_THIS config_space_size - 1) {
            put_le16(BX_VIRTIO_THIS config_space + offset, val);
            // if (BX_VIRTIO_THIS s.config_write)
            //     BX_VIRTIO_THIS s.config_write(s);
        }
        break;
    case 4:
        if (offset < BX_VIRTIO_THIS config_space_size - 3) {
            put_le32(BX_VIRTIO_THIS config_space + offset, val);
            // if (BX_VIRTIO_THIS s.config_write)
            //     BX_VIRTIO_THIS s.config_write(s);
        }
        break;
    }
}

Bit32u bx_virtio_ctrl_c::virtio_config_read(uint32_t offset, unsigned len)
{
    Bit32u val;
    switch(len) {
    case 1:
        if (offset < BX_VIRTIO_THIS config_space_size) {
            val = BX_VIRTIO_THIS config_space[offset];
        } else {
            val = 0;
        }
        break;
    case 2:
        if (offset < (BX_VIRTIO_THIS config_space_size - 1)) {
            val = get_le16(&(BX_VIRTIO_THIS config_space[offset]));
        } else {
            val = 0;
        }
        break;
    case 4:
        if (offset < (BX_VIRTIO_THIS config_space_size - 3)) {
            val = get_le32(BX_VIRTIO_THIS config_space + offset);
        } else {
            val = 0;
        }
        break;
    default:
        abort();
    }
    return val;
}

int virtio_memcpy_from_ram(void *buf, virtio_phys_addr_t addr, int count)
{
  DEV_MEM_READ_PHYSICAL_DMA(addr, count, (Bit8u *)buf);
  return 0;
}

int bx_virtio_ctrl_c::virtio_memcpy_to_ram(virtio_phys_addr_t addr,  Bit8u *buf, int count)
{
  DEV_MEM_WRITE_PHYSICAL_DMA(addr, count, buf);
  return 0;
}

int bx_virtio_ctrl_c::get_desc(VIRTIODesc *desc, int queue_idx, int desc_idx)
{
  QueueState *qs = &(BX_VIRTIO_THIS s.queue[queue_idx]);
  return virtio_memcpy_from_ram((void *)desc, qs->desc_addr +
                                  desc_idx * sizeof(VIRTIODesc),
                                  sizeof(VIRTIODesc));
}

int bx_virtio_ctrl_c::get_desc_rw_size(int *pread_size, int *pwrite_size, int queue_idx, int desc_idx)
{
    VIRTIODesc desc;
    int read_size, write_size;

    read_size = 0;
    write_size = 0;
    get_desc(&desc, queue_idx, desc_idx);

    for(;;) {
        if (desc.flags & VRING_DESC_F_WRITE)
            break;
        read_size += desc.len;
        if (!(desc.flags & VRING_DESC_F_NEXT))
            goto done;
        desc_idx = desc.next;
        get_desc(&desc, queue_idx, desc_idx);
    }
    
    for(;;) {
        if (!(desc.flags & VRING_DESC_F_WRITE))
            return -1;
        write_size += desc.len;
        if (!(desc.flags & VRING_DESC_F_NEXT))
            break;
        desc_idx = desc.next;
        get_desc(&desc, queue_idx, desc_idx);
    }

 done:
    *pread_size = read_size;
    *pwrite_size = write_size;
    return 0;
}

int bx_virtio_ctrl_c::memcpy_to_from_queue(Bit8u *buf, int queue_idx, int desc_idx, int offset, int count, bool to_queue)
{
    VIRTIODesc desc;
    int l, f_write_flag;

    if (count == 0)
        return 0;

    get_desc(&desc, queue_idx, desc_idx);

    if (to_queue) {
        f_write_flag = VRING_DESC_F_WRITE;
        /* find the first write descriptor */
        for(;;) {
            if ((desc.flags & VRING_DESC_F_WRITE) == f_write_flag)
                break;
            if (!(desc.flags & VRING_DESC_F_NEXT))
                return -1;
            desc_idx = desc.next;
            get_desc(&desc, queue_idx, desc_idx);
        }
    } else {
        f_write_flag = 0;
    }

    /* find the descriptor at offset */
    for(;;) {
        if ((desc.flags & VRING_DESC_F_WRITE) != f_write_flag)
            return -1;
        if (offset < desc.len)
            break;
        if (!(desc.flags & VRING_DESC_F_NEXT))
            return -1;
        desc_idx = desc.next;
        offset -= desc.len;
        get_desc(&desc, queue_idx, desc_idx);
    }

    for(;;) {
        l = min_int(count, desc.len - offset);
        if (to_queue) {
            virtio_memcpy_to_ram(desc.addr + offset, buf, l);
        } else {
            virtio_memcpy_from_ram(buf, desc.addr + offset, l);
        }
        count -= l;
        if (count == 0)
            break;
        offset += l;
        buf += l;
        if (offset == desc.len) {
            if (!(desc.flags & VRING_DESC_F_NEXT))
                return -1;
            desc_idx = desc.next;
            get_desc(&desc, queue_idx, desc_idx);
            if ((desc.flags & VRING_DESC_F_WRITE) != f_write_flag)
                return -1;
            offset = 0;
        }
    }
    return 0;
}

int bx_virtio_ctrl_c::memcpy_from_queue(void *buf, int queue_idx, int desc_idx, int offset, int count)
{
  return memcpy_to_from_queue((Bit8u *)buf, queue_idx, desc_idx, offset, count, false);
}

int bx_virtio_ctrl_c::memcpy_to_queue(int queue_idx, int desc_idx, int offset, void *buf, int count)
{
  return memcpy_to_from_queue((Bit8u *)buf, queue_idx, desc_idx, offset, count, true);
}

void bx_virtio_ctrl_c::virtio_consume_desc(int queue_idx, int desc_idx, int desc_len)
{
    QueueState *qs = &(BX_VIRTIO_THIS s.queue[queue_idx]);
    virtio_phys_addr_t addr;
    Bit32u index;

    addr = qs->used_addr + 2;
    index = virtio_read16(addr);
    virtio_write16(addr, index + 1);

    addr = qs->used_addr + 4 + (index & (qs->num - 1)) * 8;
    virtio_write32(addr, desc_idx);
    virtio_write32(addr + 4, desc_len);

    BX_VIRTIO_THIS s.int_status |= 1;
    DEV_pci_set_irq(BX_VIRTIO_THIS s.devfunc, BX_VIRTIO_THIS pci_conf[0x3d], 1);
}

int bx_virtio_ctrl_c::device_recv(int queue_idx, int desc_idx, int read_size, int write_size)
{
  // NOP
  return 0;
}

/* XXX: test if the queue is ready ? */
void bx_virtio_ctrl_c::queue_notify(int queue_idx)
{
    QueueState *qs = &(BX_VIRTIO_THIS s.queue[queue_idx]);
    Bit16u avail_idx;
    int desc_idx, read_size, write_size;

    if (qs->manual_recv)
        return;

    avail_idx = virtio_read16(qs->avail_addr + 2);
    while (qs->last_avail_idx != avail_idx) {
        desc_idx = virtio_read16(qs->avail_addr + 4 + 
                                 (qs->last_avail_idx & (qs->num - 1)) * 2);
        if (!get_desc_rw_size(&read_size, &write_size, queue_idx, desc_idx)) {
// #ifdef DEBUG_VIRTIO
//             if (s->debug & VIRTIO_DEBUG_IO) {
//                 printf("queue_notify: idx=%d read_size=%d write_size=%d\n",
//                        queue_idx, read_size, write_size);
//             }
// #endif

            if (device_recv(queue_idx, desc_idx,
                               read_size, write_size) < 0)
                break;
        }
        qs->last_avail_idx++;
    }
}

void bx_virtio_ctrl_c::virtio_reset()
{
  int i;
  
    BX_VIRTIO_THIS s.status = 0;
    BX_VIRTIO_THIS s.queue_sel = 0;
    BX_VIRTIO_THIS s.device_features_sel = 0;
    BX_VIRTIO_THIS s.int_status = 0;
    for(i = 0; i < MAX_QUEUE; i++) {
        QueueState *qs = &BX_VIRTIO_THIS s.queue[i];
        qs->ready = 0;
        qs->num = MAX_QUEUE_NUM;
        qs->desc_addr = 0;
        qs->avail_addr = 0;
        qs->used_addr = 0;
        qs->last_avail_idx = 0;
    }
}

bool bx_virtio_ctrl_c::mem_write_handler(bx_phy_address addr, unsigned len, void *data, void *param)
{
  bx_virtio_ctrl_c *class_ptr = (bx_virtio_ctrl_c *) param;
  if (len == 8) {
    Bit32u val = (*(Bit64u *)data) & 0xffffffff;
    if (!class_ptr->mem_write(addr, 4, &val)) return false;
    val = ((*(Bit64u *)data) >> 32) & 0xffffffff;
    if (!class_ptr->mem_write(addr + 4, 4, &val)) return false;
    return true;
  }
  return class_ptr->mem_write(addr, len, data);
}

bool bx_virtio_ctrl_c::mem_write(bx_phy_address addr, unsigned len, void *data)
{
    Bit32u val = *((Bit32u *) data);
    const Bit32u offset1 = (Bit32u) (addr - pci_bar[0].addr);
    Bit32u offset;

    switch (len) {
    case 1:
      val &= 0xFF;
      break;
    case 2:
      val &= 0xFFFF;
      break;
    }

    offset = offset1 & 0xfff;
    switch(offset1 >> 12) {
    case VIRTIO_PCI_CFG_OFFSET >> 12:
        if (len == 4) {
            switch(offset) {
            case VIRTIO_PCI_DEVICE_FEATURE_SEL:
                BX_VIRTIO_THIS s.device_features_sel = val;
                break;
            case VIRTIO_PCI_QUEUE_DESC_LOW:
                set_low32(&(BX_VIRTIO_THIS s.queue[BX_VIRTIO_THIS s.queue_sel].desc_addr), val);
                break;
            case VIRTIO_PCI_QUEUE_AVAIL_LOW:
                set_low32(&(BX_VIRTIO_THIS s.queue[BX_VIRTIO_THIS s.queue_sel].avail_addr), val);
                break;
            case VIRTIO_PCI_QUEUE_USED_LOW:
                set_low32(&(BX_VIRTIO_THIS s.queue[BX_VIRTIO_THIS s.queue_sel].used_addr), val);
                break;
            case VIRTIO_PCI_QUEUE_DESC_HIGH:
                set_high32(&(BX_VIRTIO_THIS s.queue[BX_VIRTIO_THIS s.queue_sel].desc_addr), val);
                break;
            case VIRTIO_PCI_QUEUE_AVAIL_HIGH:
                set_high32(&(BX_VIRTIO_THIS s.queue[BX_VIRTIO_THIS s.queue_sel].avail_addr), val);
                break;
            case VIRTIO_PCI_QUEUE_USED_HIGH:
                set_high32(&(BX_VIRTIO_THIS s.queue[BX_VIRTIO_THIS s.queue_sel].used_addr), val);
                break;
            }
        } else if (len == 2) {
            switch(offset) {
            case VIRTIO_PCI_QUEUE_SEL:
                if (val < MAX_QUEUE)
                    BX_VIRTIO_THIS s.queue_sel = val;
                break;
            case VIRTIO_PCI_QUEUE_SIZE:
                if ((val & (val - 1)) == 0 && val > 0) {
                    BX_VIRTIO_THIS s.queue[BX_VIRTIO_THIS s.queue_sel].num = val;
                }
                break;
            case VIRTIO_PCI_QUEUE_ENABLE:
                BX_VIRTIO_THIS s.queue[BX_VIRTIO_THIS s.queue_sel].ready = val & 1;
                break;
            }
        } else if (len == 1) {
            switch(offset) {
            case VIRTIO_PCI_DEVICE_STATUS:
                BX_VIRTIO_THIS s.status = val;
                if (val == 0) {
                    /* reset */
                    DEV_pci_set_irq(BX_VIRTIO_THIS s.devfunc, BX_VIRTIO_THIS pci_conf[0x3d], 0);
                    virtio_reset();
                }
                break;
            }
        }
        break;
    case VIRTIO_PCI_CONFIG_OFFSET >> 12:
        virtio_config_write(offset, val, len);
        break;
    case VIRTIO_PCI_NOTIFY_OFFSET >> 12:
        if (val < MAX_QUEUE)
          queue_notify(val);
        break;
    default:
        break;
    }

    return 1;
}

bool bx_virtio_ctrl_c::mem_read_handler(bx_phy_address addr, unsigned len, void *data, void *param)
{
  bx_virtio_ctrl_c *class_ptr = (bx_virtio_ctrl_c *) param;
  if (len == 8) {
    Bit64u val = 0;
    if (!class_ptr->mem_read(addr, 4, &val)) return false;
    (*(Bit64u*)data) = val;
    if (!class_ptr->mem_read(addr + 4, 4, &val)) return false;
    (*(Bit64u*)data) |= val << 32;
    return true;
  }
  return class_ptr->mem_read(addr, len, data);
}

bool bx_virtio_ctrl_c::mem_read(bx_phy_address addr, unsigned len, void *data)
{
    Bit32u offset;
    Bit32u val = 0;
    const Bit32u offset1 = (Bit32u) (addr - pci_bar[0].addr);

    offset = offset1 & 0xfff;
    switch(offset1 >> 12) {
    case VIRTIO_PCI_CFG_OFFSET >> 12:
        if (len == 4) {
            switch(offset) {
            case VIRTIO_PCI_DEVICE_FEATURE:
                switch(BX_VIRTIO_THIS s.device_features_sel) {
                case 0:
                    val = BX_VIRTIO_THIS s.device_features;
                    break;
                case 1:
                    val = 1; /* version 1 */
                    break;
                default:
                    val = 0;
                    break;
                }
                break;
            case VIRTIO_PCI_DEVICE_FEATURE_SEL:
                val = BX_VIRTIO_THIS s.device_features_sel;
                break;
            case VIRTIO_PCI_QUEUE_DESC_LOW:
                val = BX_VIRTIO_THIS s.queue[BX_VIRTIO_THIS s.queue_sel].desc_addr;
                break;
            case VIRTIO_PCI_QUEUE_AVAIL_LOW:
                val = BX_VIRTIO_THIS s.queue[BX_VIRTIO_THIS s.queue_sel].avail_addr;
                break;
            case VIRTIO_PCI_QUEUE_USED_LOW:
                val = BX_VIRTIO_THIS s.queue[BX_VIRTIO_THIS s.queue_sel].used_addr;
                break;
            case VIRTIO_PCI_QUEUE_DESC_HIGH:
                val = BX_VIRTIO_THIS s.queue[BX_VIRTIO_THIS s.queue_sel].desc_addr >> 32;
                break;
            case VIRTIO_PCI_QUEUE_AVAIL_HIGH:
                val = BX_VIRTIO_THIS s.queue[BX_VIRTIO_THIS s.queue_sel].avail_addr >> 32;
                break;
            case VIRTIO_PCI_QUEUE_USED_HIGH:
                val = BX_VIRTIO_THIS s.queue[BX_VIRTIO_THIS s.queue_sel].used_addr >> 32;
                break;
            }
        } else if (len == 2) {
            switch(offset) {
            case VIRTIO_PCI_NUM_QUEUES:
                val = MAX_QUEUE_NUM;
                break;
            case VIRTIO_PCI_QUEUE_SEL:
                val = BX_VIRTIO_THIS s.queue_sel;
                break;
            case VIRTIO_PCI_QUEUE_SIZE:
                val = BX_VIRTIO_THIS s.queue[BX_VIRTIO_THIS s.queue_sel].num;
                break;
            case VIRTIO_PCI_QUEUE_ENABLE:
                val = BX_VIRTIO_THIS s.queue[BX_VIRTIO_THIS s.queue_sel].ready;
                break;
            case VIRTIO_PCI_QUEUE_NOTIFY_OFF:
                val = 0;
                break;
            }
        } else if (len == 1) {
            switch(offset) {
            case VIRTIO_PCI_DEVICE_STATUS:
                val = BX_VIRTIO_THIS s.status;
                break;
            }
        }
        break;
    case VIRTIO_PCI_ISR_OFFSET >> 12:
        if (offset == 0 && len == 1) {
            val = BX_VIRTIO_THIS s.int_status;
            BX_VIRTIO_THIS s.int_status = 0;
            DEV_pci_set_irq(BX_VIRTIO_THIS s.devfunc, BX_VIRTIO_THIS pci_conf[0x3d], 0);
        }
        break;
    case VIRTIO_PCI_CONFIG_OFFSET >> 12:
        val = virtio_config_read(offset, len);
        break;
    }
// #ifdef DEBUG_VIRTIO
//     if (s->debug & VIRTIO_DEBUG_IO) {
//         printf("virto_pci_read: offset=0x%x val=0x%x size=%d\n", 
//                offset1, val, 1 << size_log2);
//     }
// #endif

  switch (len) {
    case 1:
      val &= 0xFF;
      *((Bit8u *) data) = (Bit8u) val;
      break;
    case 2:
      val &= 0xFFFF;
      *((Bit16u *) data) = (Bit16u) val;
      break;
    case 4:
      *((Bit32u *) data) = val;
      break;
  }

    return 1;
}

/////////////////////////////////////////////////////////////////////////
// Virtio-9p
/////////////////////////////////////////////////////////////////////////
bx_virtio_9p_ctrl_c::bx_virtio_9p_ctrl_c(char *plugin_name, char *mount_tag, FSDevice *fs)
{
  put("VIRTIO 9P");
  BX_VIRTIO_9P_THIS mount_tag = mount_tag;
  BX_VIRTIO_9P_THIS plugin_name = plugin_name;
  BX_VIRTIO_9P_THIS p9fs.fs = fs;
  BX_VIRTIO_9P_THIS p9fs.msize = 8192;
}

bx_virtio_9p_ctrl_c::~bx_virtio_9p_ctrl_c()
{
  SIM->get_bochs_root()->remove("virtio_9p");
  BX_DEBUG(("Exit"));
}

void bx_virtio_9p_ctrl_c::init()
{
  bx_virtio_ctrl_c::init(BX_VIRTIO_9P_THIS plugin_name, 0x1040 + 9, 0x2, 9, 1<<0, "Virtio-9p"); // initialize as virtio-9p

  int mount_tag_len = strlen(BX_VIRTIO_9P_THIS mount_tag);
  BX_VIRTIO_9P_THIS config_space_size = 2 + (Bit32u) mount_tag_len;
  Bit8u *cfg;
  cfg = BX_VIRTIO_9P_THIS config_space;
  cfg[0] = mount_tag_len;
  cfg[1] = mount_tag_len >> 8;
  memcpy(cfg + 2, mount_tag, mount_tag_len);

  init_list_head(&BX_VIRTIO_9P_THIS p9fs.fid_list);
}

FIDDesc *bx_virtio_9p_ctrl_c::fid_find1(Bit32u fid)
{
    struct list_head *el;
    FIDDesc *f;

    list_for_each(el, &BX_VIRTIO_9P_THIS p9fs.fid_list) {
        f = list_entry(el, FIDDesc, link);
        if (f->fid == fid)
            return f;
    }
    return NULL;
}

FSFile *bx_virtio_9p_ctrl_c::fid_find(uint32_t fid)
{
    FIDDesc *f;

    f = fid_find1(fid);
    if (!f)
        return NULL;
    return f->fd;
}

void bx_virtio_9p_ctrl_c::fid_delete(uint32_t fid)
{
    FIDDesc *f;

    f = fid_find1(fid);
    if (f) {
        BX_VIRTIO_9P_THIS p9fs.fs->fs_delete(BX_VIRTIO_9P_THIS p9fs.fs, f->fd);
        list_del(&f->link);
        free(f);
    }
}

void bx_virtio_9p_ctrl_c::fid_set(uint32_t fid, FSFile *fd)
{
    FIDDesc *f;

    f = fid_find1(fid);
    if (f) {
        BX_VIRTIO_9P_THIS p9fs.fs->fs_delete(BX_VIRTIO_9P_THIS p9fs.fs, f->fd);
        f->fd = fd;
    } else {
      f = (FIDDesc *) malloc(sizeof(*f));
        f->fid = fid;
        f->fd = fd;
        list_add(&f->link, &BX_VIRTIO_9P_THIS p9fs.fid_list);
    }
}

void bx_virtio_9p_ctrl_c::virtio_9p_send_reply(int queue_idx, int desc_idx, Bit8u id, Bit16u tag, Bit8u *buf, int buf_len)
{
    Bit8u *buf1;
    int len;

// #ifdef DEBUG_VIRTIO
//     if (s->common.debug & VIRTIO_DEBUG_9P) {
//         if (id == 6)
//             printf(" (error)");
//         printf("\n");
//     }
// #endif
    len = buf_len + 7;
    buf1 = (Bit8u *) malloc(len);
    put_le32(buf1, len);
    buf1[4] = id + 1;
    put_le16(buf1 + 5, tag);
    memcpy(buf1 + 7, buf, buf_len);
    memcpy_to_queue(queue_idx, desc_idx, 0, buf1, len);
    virtio_consume_desc(queue_idx, desc_idx, len);
    free(buf1);
}

static int marshall(Bit8u *buf1, int max_len, const char *fmt, ...)
{
    va_list ap;
    int c;
    Bit32u val;
    Bit64u val64;
    Bit8u *buf, *buf_end;

// #ifdef DEBUG_VIRTIO
//     if (s->common.debug & VIRTIO_DEBUG_9P)
//         printf(" ->");
// #endif
    va_start(ap, fmt);
    buf = buf1;
    buf_end = buf1 + max_len;
    for(;;) {
        c = *fmt++;
        if (c == '\0')
            break;
        switch(c) {
        case 'b':
            assert(buf + 1 <= buf_end);
            val = va_arg(ap, int);
// #ifdef DEBUG_VIRTIO
//             if (s->common.debug & VIRTIO_DEBUG_9P)
//                 printf(" b=%d", val);
// #endif
            buf[0] = val;
            buf += 1;
            break;
        case 'h':
            assert(buf + 2 <= buf_end);
            val = va_arg(ap, int);
// #ifdef DEBUG_VIRTIO
//             if (s->common.debug & VIRTIO_DEBUG_9P)
//                 printf(" h=%d", val);
// #endif
            put_le16(buf, val);
            buf += 2;
            break;
        case 'w':
            assert(buf + 4 <= buf_end);
            val = va_arg(ap, int);
// #ifdef DEBUG_VIRTIO
//             if (s->common.debug & VIRTIO_DEBUG_9P)
//                 printf(" w=%d", val);
// #endif
            put_le32(buf, val);
            buf += 4;
            break;
        case 'd':
            assert(buf + 8 <= buf_end);
            val64 = va_arg(ap, uint64_t);
// #ifdef DEBUG_VIRTIO
//             if (s->common.debug & VIRTIO_DEBUG_9P)
//                 printf(" d=%" PRId64, val64);
// #endif
            put_le64(buf, val64);
            buf += 8;
            break;
        case 's':
            {
                char *str;
                int len;
                str = va_arg(ap, char *);
// #ifdef DEBUG_VIRTIO
//                 if (s->common.debug & VIRTIO_DEBUG_9P)
//                     printf(" s=\"%s\"", str);
// #endif
                len = strlen(str);
                assert(len <= 65535);
                assert(buf + 2 + len <= buf_end);
                put_le16(buf, len);
                buf += 2;
                memcpy(buf, str, len);
                buf += len;
            }
            break;
        case 'Q':
            {
                FSQID *qid;
                assert(buf + 13 <= buf_end);
                qid = va_arg(ap, FSQID *);
// #ifdef DEBUG_VIRTIO
//                 if (s->common.debug & VIRTIO_DEBUG_9P)
//                     printf(" Q=%d:%d:%" PRIu64, qid->type, qid->version, qid->path);
// #endif
                buf[0] = qid->type;
                put_le32(buf + 1, qid->version);
                put_le64(buf + 5, qid->path);
                buf += 13;
            }
            break;
        default:
            abort();
        }
    }
    va_end(ap);
    return buf - buf1;
}

int bx_virtio_9p_ctrl_c::unmarshall(int queue_idx, int desc_idx, int *poffset, const char *fmt, ...)
{
    va_list ap;
    int offset, c;
    Bit8u buf[16];

    offset = *poffset;
    va_start(ap, fmt);
    for(;;) {
        c = *fmt++;
        if (c == '\0')
            break;
        switch(c) {
        case 'b':
            {
                Bit8u *ptr;
                if (memcpy_from_queue(buf, queue_idx, desc_idx, offset, 1))
                    return -1;
                ptr = va_arg(ap, Bit8u *);
                *ptr = buf[0];
                offset += 1;
// #ifdef DEBUG_VIRTIO
//                 if (s->common.debug & VIRTIO_DEBUG_9P)
//                     printf(" b=%d", *ptr);
// #endif
            }
            break;
        case 'h':
            {
                Bit16u *ptr;
                if (memcpy_from_queue(buf, queue_idx, desc_idx, offset, 2))
                    return -1;
                ptr = va_arg(ap, Bit16u *);
                *ptr = get_le16(buf);
                offset += 2;
// #ifdef DEBUG_VIRTIO
//                 if (s->common.debug & VIRTIO_DEBUG_9P)
//                     printf(" h=%d", *ptr);
// #endif
            }
            break;
        case 'w':
            {
                Bit32u *ptr;
                if (memcpy_from_queue(buf, queue_idx, desc_idx, offset, 4))
                    return -1;
                ptr = va_arg(ap, Bit32u *);
                *ptr = get_le32(buf);
                offset += 4;
// #ifdef DEBUG_VIRTIO
//                 if (s->common.debug & VIRTIO_DEBUG_9P)
//                     printf(" w=%d", *ptr);
// #endif
            }
            break;
        case 'd':
            {
                Bit64u *ptr;
                if (memcpy_from_queue(buf, queue_idx, desc_idx, offset, 8))
                    return -1;
                ptr = va_arg(ap, Bit64u *);
                *ptr = get_le64(buf);
                offset += 8;
// #ifdef DEBUG_VIRTIO
//                 if (s->common.debug & VIRTIO_DEBUG_9P)
//                     printf(" d=%" PRId64, *ptr);
// #endif
            }
            break;
        case 's':
            {
                char *str, **ptr;
                int len;

                if (memcpy_from_queue(buf, queue_idx, desc_idx, offset, 2))
                    return -1;
                len = get_le16(buf);
                offset += 2;
                str = (char *) malloc(len + 1);
                if (memcpy_from_queue(str, queue_idx, desc_idx, offset, len))
                    return -1;
                str[len] = '\0';
                offset += len;
                ptr = va_arg(ap, char **);
                *ptr = str;
// #ifdef DEBUG_VIRTIO
//                 if (s->common.debug & VIRTIO_DEBUG_9P)
//                     printf(" s=\"%s\"", *ptr);
// #endif
            }
            break;
        default:
            abort();
        }
    }
    va_end(ap);
    *poffset = offset;
    return 0;
}

void bx_virtio_9p_ctrl_c::virtio_9p_send_error(int queue_idx, int desc_idx, Bit16u tag, Bit32u error)
{
    Bit8u buf[4];
    int buf_len;

    buf_len = marshall(buf, sizeof(buf), "w", -error);
    virtio_9p_send_reply(queue_idx, desc_idx, 6, tag, buf, buf_len);
}

void bx_virtio_9p_ctrl_c::virtio_9p_open_reply(FSDevice *fs, FSQID *qid, int err, P9OpenInfo *oi)
{
    uint8_t buf[32];
    int buf_len;
    
    if (err < 0) {
        virtio_9p_send_error(oi->queue_idx, oi->desc_idx, oi->tag, err);
    } else {
        buf_len = marshall(buf, sizeof(buf),
                           "Qw", qid, BX_VIRTIO_9P_THIS p9fs.msize - 24);
        virtio_9p_send_reply(oi->queue_idx, oi->desc_idx, 12, oi->tag,
                             buf, buf_len);
    }
    free(oi);
}

int bx_virtio_9p_ctrl_c::device_recv(int queue_idx, int desc_idx, int read_size, int write_size)
{
    VIRTIO9PDevice *s = &BX_VIRTIO_9P_THIS p9fs;
    int offset, header_len;
    Bit8u id;
    Bit16u tag;
    Bit8u buf[1024];
    int buf_len, err;
    FSDevice *fs = BX_VIRTIO_9P_THIS p9fs.fs;

    if (queue_idx != 0)
        return 0;

    offset = 0;
    header_len = 4 + 1 + 2;
    if (memcpy_from_queue(buf, queue_idx, desc_idx, offset, header_len)) {
        tag = 0;
        goto protocol_error;
    }
    //size = get_le32(buf);
    id = buf[4];
    tag = get_le16(buf + 5);
    offset += header_len;
    
// #ifdef DEBUG_VIRTIO
//     if (s1->debug & VIRTIO_DEBUG_9P) {
//         const char *name;
//         name = get_9p_op_name(id);
//         printf("9p: op=");
//         if (name)
//             printf("%s", name);
//         else
//             printf("%d", id);
//     }
// #endif

    /* Note: same subset as JOR1K */
    switch(id) {
    case 8: /* statfs */
        {
            FSStatFS st;

            fs->fs_statfs(fs, &st);
            buf_len = marshall(buf, sizeof(buf),
                               "wwddddddw", 
                               0,
                               st.f_bsize,
                               st.f_blocks,
                               st.f_bfree,
                               st.f_bavail,
                               st.f_files,
                               st.f_ffree,
                               0, /* id */
                               256 /* max filename length */
                               );
            virtio_9p_send_reply(queue_idx, desc_idx, id, tag, buf, buf_len);
        }
        break;
    case 12: /* lopen */
        {
            Bit32u fid, flags;
            FSFile *f;
            FSQID qid;
            P9OpenInfo *oi;
            
            if (unmarshall(queue_idx, desc_idx, &offset,
                           "ww", &fid, &flags))
                goto protocol_error;
            f = fid_find(fid);
            if (!f)
                goto fid_not_found;
            oi = (P9OpenInfo *) malloc(sizeof(*oi));
            oi->dev = s;
            oi->queue_idx = queue_idx;
            oi->desc_idx = desc_idx;
            oi->tag = tag;
            err = fs->fs_open(fs, &qid, f, flags, oi);
            virtio_9p_open_reply(fs, &qid, err, oi);
        }
        break;
    case 14: /* lcreate */
        {
            Bit32u fid, flags, mode, gid;
            char *name;
            FSFile *f;
            FSQID qid;

            if (unmarshall(queue_idx, desc_idx, &offset,
                           "wswww", &fid, &name, &flags, &mode, &gid))
                goto protocol_error;
            f = fid_find(fid);
            if (!f) {
                err = -P9_EPROTO;
            } else {
                err = fs->fs_create(fs, &qid, f, name, flags, mode, gid);
            }
            free(name);
            if (err) 
                goto error;
            buf_len = marshall(buf, sizeof(buf),
                               "Qw", &qid, s->msize - 24);
            virtio_9p_send_reply(queue_idx, desc_idx, id, tag, buf, buf_len);
        }
        break;
    case 16: /* symlink */
        {
            Bit32u fid, gid;
            char *name, *symgt;
            FSFile *f;
            FSQID qid;

            if (unmarshall(queue_idx, desc_idx, &offset,
                           "wssw", &fid, &name, &symgt, &gid))
                goto protocol_error;
            f = fid_find(fid);
            if (!f) {
                err = -P9_EPROTO;
            } else {
                err = fs->fs_symlink(fs, &qid, f, name, symgt, gid);
            }
            free(name);
            free(symgt);
            if (err)
                goto error;
            buf_len = marshall(buf, sizeof(buf),
                               "Q", &qid);
            virtio_9p_send_reply(queue_idx, desc_idx, id, tag, buf, buf_len);
        }
        break;
    case 18: /* mknod */
        {
            Bit32u fid, mode, major, minor, gid;
            char *name;
            FSFile *f;
            FSQID qid;

            if (unmarshall(queue_idx, desc_idx, &offset,
                           "wswwww", &fid, &name, &mode, &major, &minor, &gid))
                goto protocol_error;
            f = fid_find(fid);
            if (!f) {
                err = -P9_EPROTO;
            } else {
                err = fs->fs_mknod(fs, &qid, f, name, mode, major, minor, gid);
            }
            free(name);
            if (err)
                goto error;
            buf_len = marshall(buf, sizeof(buf),
                               "Q", &qid);
            virtio_9p_send_reply(queue_idx, desc_idx, id, tag, buf, buf_len);
        }
        break;
    case 22: /* readlink */
        {
            Bit32u fid;
            char buf1[1024];
            FSFile *f;

            if (unmarshall(queue_idx, desc_idx, &offset,
                           "w", &fid))
                goto protocol_error;
            f = fid_find(fid);
            if (!f) {
                err = -P9_EPROTO;
            } else {
                err = fs->fs_readlink(fs, buf1, sizeof(buf1), f);
            }
            if (err)
                goto error;
            buf_len = marshall(buf, sizeof(buf), "s", buf1);
            virtio_9p_send_reply(queue_idx, desc_idx, id, tag, buf, buf_len);
        }
        break;
    case 24: /* getattr */
        {
            Bit32u fid;
            uint64_t mask;
            FSFile *f;
            FSStat st;

            if (unmarshall(queue_idx, desc_idx, &offset,
                           "wd", &fid, &mask))
                goto protocol_error;
            f = fid_find(fid);
            if (!f)
                goto fid_not_found;
            err = fs->fs_stat(fs, f, &st);
            if (err)
                goto error;

            buf_len = marshall(buf, sizeof(buf),
                               "dQwwwddddddddddddddd", 
                               mask, &st.qid,
                               st.st_mode, st.st_uid, st.st_gid,
                               st.st_nlink, st.st_rdev, st.st_size,
                               st.st_blksize, st.st_blocks,
                               st.st_atime_sec, (uint64_t)st.st_atime_nsec,
                               st.st_mtime_sec, (uint64_t)st.st_mtime_nsec,
                               st.st_ctime_sec, (uint64_t)st.st_ctime_nsec,
                               (uint64_t)0, (uint64_t)0,
                               (uint64_t)0, (uint64_t)0);
            virtio_9p_send_reply(queue_idx, desc_idx, id, tag, buf, buf_len);
        }
        break;
    case 26: /* setattr */
        {
            Bit32u fid, mask, mode, uid, gid;
            uint64_t size, atime_sec, atime_nsec, mtime_sec, mtime_nsec;
            FSFile *f;

            if (unmarshall(queue_idx, desc_idx, &offset,
                           "wwwwwddddd", &fid, &mask, &mode, &uid, &gid,
                           &size, &atime_sec, &atime_nsec, 
                           &mtime_sec, &mtime_nsec))
                goto protocol_error;
            f = fid_find(fid);
            if (!f)
                goto fid_not_found;
            err = fs->fs_setattr(fs, f, mask, mode, uid, gid, size, atime_sec,
                                 atime_nsec, mtime_sec, mtime_nsec);
            if (err)
                goto error;
            virtio_9p_send_reply(queue_idx, desc_idx, id, tag, NULL, 0);
        }
        break;
    case 30: /* xattrwalk */
        {
            /* not supported yet */
            err = -P9_ENOTSUP;
            goto error;
        }
        break;
    case 40: /* readdir */
        {
            Bit32u fid, count;
            uint64_t offs;
            Bit8u *buf;
            int n;
            FSFile *f;

            if (unmarshall(queue_idx, desc_idx, &offset,
                           "wdw", &fid, &offs, &count))
                goto protocol_error;
            f = fid_find(fid);
            if (!f)
                goto fid_not_found;
            buf = (Bit8u *) malloc(count + 4);
            n = fs->fs_readdir(fs, f, offs, buf + 4, count);
            if (n < 0) {
                err = n;
                goto error;
            }
            put_le32(buf, n);
            virtio_9p_send_reply(queue_idx, desc_idx, id, tag, buf, n + 4);
            free(buf);
        }
        break;
    case 50: /* fsync */
        {
            Bit32u fid;
            if (unmarshall(queue_idx, desc_idx, &offset,
                           "w", &fid))
                goto protocol_error;
            /* ignored */
            virtio_9p_send_reply(queue_idx, desc_idx, id, tag, NULL, 0);
        }
        break;
    case 52: /* lock */
        {
            Bit32u fid;
            FSFile *f;
            FSLock lock;
            
            if (unmarshall(queue_idx, desc_idx, &offset,
                           "wbwddws", &fid, &lock.type, &lock.flags,
                           &lock.start, &lock.length,
                           &lock.proc_id, &lock.client_id))
                goto protocol_error;
            f = fid_find(fid);
            if (!f)
                err = -P9_EPROTO;
            else
                err = fs->fs_lock(fs, f, &lock);
            free(lock.client_id);
            if (err < 0)
                goto error;
            buf_len = marshall(buf, sizeof(buf), "b", err);
            virtio_9p_send_reply(queue_idx, desc_idx, id, tag, buf, buf_len);
        }
        break;
    case 54: /* getlock */
        {
            Bit32u fid;
            FSFile *f;
            FSLock lock;
            
            if (unmarshall(queue_idx, desc_idx, &offset,
                           "wbddws", &fid, &lock.type,
                           &lock.start, &lock.length,
                           &lock.proc_id, &lock.client_id))
                goto protocol_error;
            f = fid_find(fid);
            if (!f)
                err = -P9_EPROTO;
            else
                err = fs->fs_getlock(fs, f, &lock);
            if (err < 0) {
                free(lock.client_id);
                goto error;
            }
            buf_len = marshall(buf, sizeof(buf), "bddws",
                               &lock.type,
                               &lock.start, &lock.length,
                               &lock.proc_id, &lock.client_id);
            free(lock.client_id);
            virtio_9p_send_reply(queue_idx, desc_idx, id, tag, buf, buf_len);
        }
        break;
    case 70: /* link */
        {
            Bit32u dfid, fid;
            char *name;
            FSFile *f, *df;

            if (unmarshall(queue_idx, desc_idx, &offset,
                           "wws", &dfid, &fid, &name))
                goto protocol_error;
            df = fid_find(dfid);
            f = fid_find(fid);
            if (!df || !f) {
                err = -P9_EPROTO;
            } else {
                err = fs->fs_link(fs, df, f, name);
            }
            free(name);
            if (err)
                goto error;
            virtio_9p_send_reply(queue_idx, desc_idx, id, tag, NULL, 0);
        }
        break;
    case 72: /* mkdir */
        {
            Bit32u fid, mode, gid;
            char *name;
            FSFile *f;
            FSQID qid;

            if (unmarshall(queue_idx, desc_idx, &offset,
                           "wsww", &fid, &name, &mode, &gid))
                goto protocol_error;
            f = fid_find(fid);
            if (!f)
                goto fid_not_found;
            err = fs->fs_mkdir(fs, &qid, f, name, mode, gid);
            if (err != 0)
                goto error;
            buf_len = marshall(buf, sizeof(buf), "Q", &qid);
            virtio_9p_send_reply(queue_idx, desc_idx, id, tag, buf, buf_len);
        }
        break;
    case 74: /* renameat */
        {
            Bit32u fid, new_fid;
            char *name, *new_name;
            FSFile *f, *new_f;

            if (unmarshall(queue_idx, desc_idx, &offset,
                           "wsws", &fid, &name, &new_fid, &new_name))
                goto protocol_error;
            f = fid_find(fid);
            new_f = fid_find(new_fid);
            if (!f || !new_f) {
                err = -P9_EPROTO;
            } else {
                err = fs->fs_renameat(fs, f, name, new_f, new_name);
            }
            free(name);
            free(new_name);
            if (err != 0)
                goto error;
            virtio_9p_send_reply(queue_idx, desc_idx, id, tag, NULL, 0);
        }
        break;
    case 76: /* unlinkat */
        {
            Bit32u fid, flags;
            char *name;
            FSFile *f;

            if (unmarshall(queue_idx, desc_idx, &offset,
                           "wsw", &fid, &name, &flags))
                goto protocol_error;
            f = fid_find(fid);
            if (!f) {
                err = -P9_EPROTO;
            } else {
                err = fs->fs_unlinkat(fs, f, name);
            }
            free(name);
            if (err != 0)
                goto error;
            virtio_9p_send_reply(queue_idx, desc_idx, id, tag, NULL, 0);
        }
        break;
    case 100: /* version */
        {
            Bit32u msize;
            char *version;
            if (unmarshall(queue_idx, desc_idx, &offset, 
                           "ws", &msize, &version))
                goto protocol_error;
            s->msize = msize;
            //            printf("version: msize=%d version=%s\n", msize, version);
            free(version);
            buf_len = marshall(buf, sizeof(buf), "ws", s->msize, "9P2000.L");
            virtio_9p_send_reply(queue_idx, desc_idx, id, tag, buf, buf_len);
        }
        break;
    case 104: /* attach */
        {
            Bit32u fid, afid, uid;
            char *uname, *aname;
            FSQID qid;
            FSFile *f;

            if (unmarshall(queue_idx, desc_idx, &offset, 
                           "wwssw", &fid, &afid, &uname, &aname, &uid))
                goto protocol_error;
            err = fs->fs_attach(fs, &f, &qid, uid, uname, aname);
            if (err != 0)
                goto error;
            fid_set(fid, f);
            free(uname);
            free(aname);
            buf_len = marshall(buf, sizeof(buf), "Q", &qid);
            virtio_9p_send_reply(queue_idx, desc_idx, id, tag, buf, buf_len);
        }
        break;
    case 108: /* flush */
        {
            Bit16u oldtag;
            if (unmarshall(queue_idx, desc_idx, &offset, 
                           "h", &oldtag))
                goto protocol_error;
            /* ignored */
            virtio_9p_send_reply(queue_idx, desc_idx, id, tag, NULL, 0);
        }
        break;
    case 110: /* walk */
        {
            Bit32u fid, newfid;
            Bit16u nwname;
            FSQID *qids;
            char **names;
            FSFile *f;
            int i;

            if (unmarshall(queue_idx, desc_idx, &offset, 
                           "wwh", &fid, &newfid, &nwname))
                goto protocol_error;
            f = fid_find(fid);
            if (!f)
                goto fid_not_found;
            names = (char **)mallocz(sizeof(names[0]) * nwname);
            qids = (FSQID *)malloc(sizeof(qids[0]) * nwname);
            for(i = 0; i < nwname; i++) {
                if (unmarshall(queue_idx, desc_idx, &offset, 
                               "s", &names[i])) {
                    err = -P9_EPROTO;
                    goto walk_done;
                }
            }
            err = fs->fs_walk(fs, &f, qids, f, nwname, names);
        walk_done:
            for(i = 0; i < nwname; i++) {
                free(names[i]);
            }
            free(names);
            if (err < 0) {
                free(qids);
                goto error;
            }
            buf_len = marshall(buf, sizeof(buf), "h", err);
            for(i = 0; i < err; i++) {
                buf_len += marshall(buf + buf_len, sizeof(buf) - buf_len,
                                    "Q", &qids[i]);
            }
            free(qids);
            fid_set(newfid, f);
            virtio_9p_send_reply(queue_idx, desc_idx, id, tag, buf, buf_len);
        }
        break;
    case 116: /* read */
        {
            Bit32u fid, count;
            uint64_t offs;
            Bit8u *buf;
            int n;
            FSFile *f;

            if (unmarshall(queue_idx, desc_idx, &offset,
                           "wdw", &fid, &offs, &count))
                goto protocol_error;
            f = fid_find(fid);
            if (!f)
                goto fid_not_found;
            buf = (Bit8u *)malloc(count + 4);
            n = fs->fs_read(fs, f, offs, buf + 4, count);
            if (n < 0) {
                err = n;
                free(buf);
                goto error;
            }
            put_le32(buf, n);
            virtio_9p_send_reply(queue_idx, desc_idx, id, tag, buf, n + 4);
            free(buf);
        }
        break;
    case 118: /* write */
        {
            Bit32u fid, count;
            uint64_t offs;
            Bit8u *buf1;
            int n;
            FSFile *f;

            if (unmarshall(queue_idx, desc_idx, &offset,
                           "wdw", &fid, &offs, &count))
                goto protocol_error;
            f = fid_find(fid);
            if (!f)
                goto fid_not_found;
            buf1 = (Bit8u *)malloc(count);
            if (memcpy_from_queue(buf1, queue_idx, desc_idx, offset,
                                  count)) {
                free(buf1);
                goto protocol_error;
            }
            n = fs->fs_write(fs, f, offs, buf1, count);
            free(buf1);
            if (n < 0) {
                err = n;
                goto error;
            }
            buf_len = marshall(buf, sizeof(buf), "w", n);
            virtio_9p_send_reply(queue_idx, desc_idx, id, tag, buf, buf_len);
        }
        break;
    case 120: /* clunk */
        {
            Bit32u fid;
            
            if (unmarshall(queue_idx, desc_idx, &offset, 
                           "w", &fid))
                goto protocol_error;
            fid_delete(fid);
            virtio_9p_send_reply(queue_idx, desc_idx, id, tag, NULL, 0);
        }
        break;
    default:
        goto protocol_error;
    }

    return 0;
 error:
    virtio_9p_send_error(queue_idx, desc_idx, tag, err);
    return 0;
 protocol_error:
 fid_not_found:
    err = -P9_EPROTO;
    goto error;
}

/////////////////////////////////////////////////////////////////////////
// Virtio-console
/////////////////////////////////////////////////////////////////////////
bx_virtio_console_ctrl_c::bx_virtio_console_ctrl_c(char *plugin_name, CharacterDevice *cs)
{
  put("VIRTIO CONSOLE");
  BX_VIRTIO_CONSOLE_THIS plugin_name = plugin_name;
  BX_VIRTIO_CONSOLE_THIS dev.cs = cs;
}

bx_virtio_console_ctrl_c::~bx_virtio_console_ctrl_c()
{
  SIM->get_bochs_root()->remove("virtio_console");
  BX_DEBUG(("Exit"));
}

void bx_virtio_console_ctrl_c::init()
{
  bx_virtio_ctrl_c::init(BX_VIRTIO_CONSOLE_THIS plugin_name, 0x1003, 0x0780, 3, 1 << 0, "Virtio-console"); // initialize as virtio-console

  BX_VIRTIO_CONSOLE_THIS config_space_size = 4;
  QueueState *qs = &BX_VIRTIO_CONSOLE_THIS s.queue[0];
  qs->manual_recv = true;

  BX_VIRTIO_CONSOLE_THIS timer_id = DEV_register_timer(this, rx_timer_handler, 0, false, false, "virtio-console.rx");
  bx_pc_system.activate_timer(BX_VIRTIO_CONSOLE_THIS timer_id, 100, false); /* not continuous */
}

int bx_virtio_console_ctrl_c::device_recv(int queue_idx, int desc_idx, int read_size, int write_size)
{
    CharacterDevice *cs = BX_VIRTIO_CONSOLE_THIS dev.cs;
    uint8_t *buf;

    if (queue_idx == 1) {
        /* send to console */
        buf = (uint8_t *)malloc(read_size);
        memcpy_from_queue(buf, queue_idx, desc_idx, 0, read_size);
        cs->write_data(cs->opaque, buf, read_size);
        free(buf);
        virtio_consume_desc(queue_idx, desc_idx, 0);
    }
    return 0;
}

bool bx_virtio_console_ctrl_c::virtio_console_can_write_data()
{
    QueueState *qs = &BX_VIRTIO_CONSOLE_THIS s.queue[0];
    uint16_t avail_idx;

    if (!qs->ready)
        return false;
    avail_idx = virtio_read16(qs->avail_addr + 2);
    return qs->last_avail_idx != avail_idx;
}

int bx_virtio_console_ctrl_c::virtio_console_get_write_len()
{
    int queue_idx = 0;
    QueueState *qs = &BX_VIRTIO_CONSOLE_THIS s.queue[queue_idx];
    int desc_idx;
    int read_size, write_size;
    uint16_t avail_idx;

    if (!qs->ready)
        return 0;
    avail_idx = virtio_read16(qs->avail_addr + 2);
    if (qs->last_avail_idx == avail_idx)
        return 0;
    desc_idx = virtio_read16(qs->avail_addr + 4 + 
                             (qs->last_avail_idx & (qs->num - 1)) * 2);
    if (get_desc_rw_size(&read_size, &write_size, queue_idx, desc_idx))
        return 0;
    return write_size;
}

int bx_virtio_console_ctrl_c::virtio_console_write_data(const uint8_t *buf, int buf_len)
{
    int queue_idx = 0;
    QueueState *qs = &BX_VIRTIO_CONSOLE_THIS s.queue[queue_idx];
    int desc_idx;
    uint16_t avail_idx;

    if (!qs->ready)
        return 0;
    avail_idx = virtio_read16(qs->avail_addr + 2);
    if (qs->last_avail_idx == avail_idx)
        return 0;
    desc_idx = virtio_read16(qs->avail_addr + 4 + 
                             (qs->last_avail_idx & (qs->num - 1)) * 2);
    memcpy_to_queue(queue_idx, desc_idx, 0, (void *)buf, buf_len);
    virtio_consume_desc(queue_idx, desc_idx, buf_len);
    qs->last_avail_idx++;
    return buf_len;
}

/* send a resize event */
void bx_virtio_console_ctrl_c::virtio_console_resize_event(int width, int height)
{
    /* indicate the console size */
    put_le16(BX_VIRTIO_CONSOLE_THIS config_space + 0, width);
    put_le16(BX_VIRTIO_CONSOLE_THIS config_space + 2, height);

    BX_VIRTIO_CONSOLE_THIS s.int_status |= 2;
    DEV_pci_set_irq(BX_VIRTIO_CONSOLE_THIS s.devfunc, BX_VIRTIO_CONSOLE_THIS pci_conf[0x3d], 1);
}

static void console_get_size(STDIODevice *s, int *pw, int *ph)
{
//  struct winsize ws;
   int width, height;
   /* default values */
   width = 80;
   height = 25;
//  if (ioctl(s->stdin_fd, TIOCGWINSZ, &ws) == 0 &&
//      ws.ws_col >= 4 && ws.ws_row >= 4) {
//      width = ws.ws_col;
//      height = ws.ws_row;
//  }
   *pw = width;
   *ph = height;
}

void bx_virtio_console_ctrl_c::rx_timer_handler(void *this_ptr)
{
    bx_virtio_console_ctrl_c *class_ptr = (bx_virtio_console_ctrl_c *)this_ptr;
    fd_set rfds, wfds, efds;
    int fd_max, ret = 0, delay;
    struct timeval tv;
    int stdin_fd;
    bool enable_stdin = !(SIM->get_param_bool(BXPN_WASM_NOSTDIN)->get());

    FD_ZERO(&rfds);
    FD_ZERO(&wfds);
    FD_ZERO(&efds);
    fd_max = -1;
    if (enable_stdin && class_ptr->virtio_console_can_write_data()) {
      STDIODevice *s = (STDIODevice *)class_ptr->dev.cs->opaque;
      stdin_fd = s->stdin_fd;
      FD_SET(stdin_fd, &rfds);
      fd_max = stdin_fd;

      if (s->resize_pending) {
        int width, height;
        console_get_size(s, &width, &height);
        class_ptr->virtio_console_resize_event(width, height);
        s->resize_pending = false;
      }
    }
    tv.tv_sec = 0;
    tv.tv_usec = 0;
    if (enable_stdin && init_done)
        ret = select(fd_max + 1, &rfds, &wfds, NULL, &tv);

    if ((ret > 0) || (init_start && !init_done)) {
      if ((FD_ISSET(stdin_fd, &rfds) || (init_start && !init_done))) {
        uint8_t buf[128];
        int ret, len;
        len = class_ptr->virtio_console_get_write_len();
        len = min_int(len, sizeof(buf));
        ret = class_ptr->dev.cs->read_data(class_ptr->dev.cs->opaque, buf, len);
        if (ret > 0) {
          class_ptr->virtio_console_write_data(buf, ret);
        }
      }
    }
    if (ret > 0) {
      bx_pc_system.activate_timer(class_ptr->timer_id, 100, false); /* not continuous */
    } else {
      bx_pc_system.activate_timer(class_ptr->timer_id, 10000/*10ms*/, false); /* not continuous */
    }
}

/////////////////////////////////////////////////////////////////////////
// Virtio-net
/////////////////////////////////////////////////////////////////////////
bx_virtio_net_ctrl_c::bx_virtio_net_ctrl_c(char *plugin_name, EthernetDevice *es)
{
  put("VIRTIO NET");
  BX_VIRTIO_NET_THIS plugin_name = plugin_name;
  BX_VIRTIO_NET_THIS es = es; // TODO
}

bx_virtio_net_ctrl_c::~bx_virtio_net_ctrl_c()
{
  SIM->get_bochs_root()->remove("virtio_net");
  BX_DEBUG(("Exit"));
}

bool bx_virtio_net_ctrl_c::virtio_net_can_write_packet(EthernetDevice *es)
{
    QueueState *qs = &BX_VIRTIO_THIS s.queue[0];
    uint16_t avail_idx;

    if (!qs->ready)
        return false;
    avail_idx = virtio_read16(qs->avail_addr + 2);
    return qs->last_avail_idx != avail_idx;
}

void bx_virtio_net_ctrl_c::virtio_net_write_packet(EthernetDevice *es, const uint8_t *buf, int buf_len)
{
    VIRTIONetDevice *s1 = &BX_VIRTIO_NET_THIS net;
    int queue_idx = 0;
    QueueState *qs = &BX_VIRTIO_THIS s.queue[0];
    int desc_idx;
    VIRTIONetHeader h;
    int len, read_size, write_size;
    uint16_t avail_idx;

    if (!qs->ready)
        return;
    avail_idx = virtio_read16(qs->avail_addr + 2);
    if (qs->last_avail_idx == avail_idx)
        return;
    desc_idx = virtio_read16(qs->avail_addr + 4 + 
                             (qs->last_avail_idx & (qs->num - 1)) * 2);
    if (get_desc_rw_size(&read_size, &write_size, queue_idx, desc_idx))
        return;
    len = s1->header_size + buf_len; 
    if (len > write_size)
        return;
    memset(&h, 0, s1->header_size);
    memcpy_to_queue(queue_idx, desc_idx, 0, &h, s1->header_size);
    memcpy_to_queue(queue_idx, desc_idx, s1->header_size, (void*)buf, buf_len);
    virtio_consume_desc(queue_idx, desc_idx, len);
    qs->last_avail_idx++;
}

void bx_virtio_net_ctrl_c::init()
{
  bx_virtio_ctrl_c::init(BX_VIRTIO_NET_THIS plugin_name, 0x1000, 0x0200, 0x1, 1 << 5, "Virtio-net"); // initialize as virtio-net

  BX_VIRTIO_NET_THIS config_space_size = 6 + 2;
  Bit8u *cfg;
  cfg = BX_VIRTIO_NET_THIS config_space;
  memcpy(cfg, BX_VIRTIO_NET_THIS es->mac_addr, 6);
  /* status */
  cfg[6] = 0;
  cfg[7] = 0;
  QueueState *qs = &BX_VIRTIO_NET_THIS s.queue[0];
  qs->manual_recv = true;
  BX_VIRTIO_NET_THIS net.header_size = sizeof(VIRTIONetHeader);
  BX_VIRTIO_NET_THIS es->virtio_device = this;
  BX_VIRTIO_NET_THIS timer_id = DEV_register_timer(this, rx_timer_handler, 0, false, false, "virtio-net.rx");;
  bx_pc_system.activate_timer(BX_VIRTIO_NET_THIS timer_id, 100, false); /* not continuous */
}

void bx_virtio_net_ctrl_c::rx_timer_handler(void *this_ptr)
{
    bx_virtio_net_ctrl_c *class_ptr = (bx_virtio_net_ctrl_c *)this_ptr;
    EthernetDevice *es = class_ptr->es;
    int n_fd_max = -1, delay = 0, n_ret = 0;
    fd_set n_rfds, n_wfds, n_efds;
    struct timeval tv;

    FD_ZERO(&n_rfds);
    FD_ZERO(&n_wfds);
    FD_ZERO(&n_efds);
    es->select_fill(es, &n_fd_max, &n_rfds, &n_wfds, &n_efds, &delay);
    if (n_fd_max >= 0) {
      tv.tv_sec = 0;
      tv.tv_usec = 0;
      n_ret = select(n_fd_max + 1, &n_rfds, NULL, NULL, &tv);
      es->select_poll(es, &n_rfds, &n_wfds, &n_efds, n_ret);
    }
    int watch_res = es->watch(es);
    int duration = 100;
    if (watch_res < 0) {
      duration = 1000000;
    } else if (n_ret <= 0) {
      duration = 100000;
    }
    bx_pc_system.activate_timer(class_ptr->timer_id, duration, false); /* not continuous */
}

int bx_virtio_net_ctrl_c::device_recv(int queue_idx, int desc_idx, int read_size, int write_size)
{
    VIRTIONetDevice *s1 = &BX_VIRTIO_NET_THIS net;
    EthernetDevice *es = BX_VIRTIO_NET_THIS es;
    VIRTIONetHeader h;
    uint8_t *buf;
    int len;

    if (queue_idx == 1) {
        /* send to network */
        if (memcpy_from_queue(&h, queue_idx, desc_idx, 0, s1->header_size) < 0)
            return 0;
        len = read_size - s1->header_size;
        buf = (uint8_t *)malloc(len);
        memcpy_from_queue(buf, queue_idx, desc_idx, s1->header_size, len);
        es->write_packet(es, buf, len);
        free(buf);
        virtio_consume_desc(queue_idx, desc_idx, 0);
    }
    return 0;
}

typedef struct {
    int fd;
    bool select_filled;
    char *raw_flag;

    int tmpfd;
    bool enabled;
    int retrynum;

    uint32_t sizebuf;
    int sizeoff;
    int pktsize;
    int pktoff;
    uint8_t *pktbuf;
} QemuSocketState;

static void qemu_write_packet(EthernetDevice *net,
                               const uint8_t *buf, int len)
{
    QemuSocketState *s = (QemuSocketState *)net->opaque;
    uint32_t size = htonl(len);
    int ret;

    if (s->fd < 0) {
      return;
    }

    ret = send(s->fd, &size, 4, 0); // TODO: check error
    if (ret < 0) {
      close(s->fd);
      s->fd = -1; // invalid fd. hopefully the watch loop will recover this.
      return;
    }
    ret = send(s->fd, buf, len, 0); // TODO: check error
    if (ret < 0) {
      close(s->fd);
      s->fd = -1; // invalid fd. hopefully the watch loop will recover this.
      return;
    }
}

static int try_get_fd(QemuSocketState *s);

static int qemu_watch1(EthernetDevice *net)
{
  QemuSocketState *s = (QemuSocketState *)net->opaque;

  if (!s->enabled)
    return -1;

  if ((s->fd >= 0) || (try_get_fd((QemuSocketState *)net->opaque) >= 0))
    return 0;

  return -1;
}

static void qemu_select_fill1(EthernetDevice *net, int *pfd_max,
                               fd_set *rfds, fd_set *wfds, fd_set *efds,
                               int *pdelay)
{
   QemuSocketState *s = (QemuSocketState *)net->opaque;
   int fd = s->fd;

   if (fd < 0) {
     return;
   }

   s->select_filled = net->virtio_device->virtio_net_can_write_packet(net);
   if (s->select_filled) {
       FD_SET(fd, rfds);
       *pfd_max = max_int(*pfd_max, fd);
   }
}

static void qemu_select_poll1(EthernetDevice *net, 
                               fd_set *rfds, fd_set *wfds, fd_set *efds,
                               int select_ret)
{
   QemuSocketState *s = (QemuSocketState *)net->opaque;
   int fd = s->fd;
   uint32_t size = 0;
   int ret;
   
   if (fd < 0) {
     return;
   }

   if (select_ret <= 0) {
     if (select_ret < 0) {
       fflush(stdout);
       close(s->fd);
       s->fd = -1; // invalid fd. hopefully the watch loop will recover this.
     }
     return;
   }

   if (s->select_filled && FD_ISSET(fd, rfds)) {
     if (s->pktsize <= 0) {
       while (s->sizeoff < 4) {
         ret = recv(fd, &s->sizebuf + s->sizeoff, 4 - s->sizeoff, 0);
         if (ret <= 0) {
           if (errno == EAGAIN)
             return; // try later
           close(s->fd);
           s->fd = -1; // invalid fd. hopefully the watch loop will recover this.
           return;
         }
         s->sizeoff += ret;
       }
       s->pktsize = ntohl(s->sizebuf);
       s->sizeoff = 0;
       s->sizebuf = 0;
     }

     if (s->pktsize > 0) {
       size = s->pktsize;
       if (s->pktbuf == NULL) {
         s->pktoff = 0;
         s->pktbuf = (uint8_t *)mallocz(size); // TODO: make limit
       }
       while (s->pktoff < size) {
         ret = recv(fd, s->pktbuf + s->pktoff, size - s->pktoff, 0);
         if (ret <= 0) {
           if (errno == EAGAIN)
             return; // try later
           close(s->fd);
           s->fd = -1; // invalid fd. hopefully the watch loop will recover this.
           return;
         }
         s->pktoff += ret;
       }
       net->virtio_device->virtio_net_write_packet(net, s->pktbuf, size);
       free(s->pktbuf);
       s->pktbuf = NULL;
       s->pktsize = 0;
       s->pktoff = 0;
     }
   }
}

static EthernetDevice *qemu_net_init()
{
    EthernetDevice *net;
    QemuSocketState *s;

    net = (EthernetDevice *)mallocz(sizeof(*net));
    net->mac_addr[0] = 0x02;
    net->mac_addr[1] = 0x00;
    net->mac_addr[2] = 0x00;
    net->mac_addr[3] = 0x00;
    net->mac_addr[4] = 0x00;
    net->mac_addr[5] = 0x01;
    net->opaque = NULL;
    s = (QemuSocketState *)mallocz(sizeof(*s));
    s->fd = -1;
    s->tmpfd = -1;
    s->enabled = false;
    net->opaque = s;
    net->write_packet = qemu_write_packet;
    net->select_fill = qemu_select_fill1;
    net->select_poll = qemu_select_poll1;
    net->watch = qemu_watch1;

    return net;
}

static void reset_qemu_socket_state(QemuSocketState *s)
{
    s->sizebuf = 0;
    s->sizeoff = 0;
    s->pktsize = 0;
    s->pktoff = 0;
    if (s->pktbuf != NULL) {
      free(s->pktbuf);
    }
    s->pktbuf = NULL;
}

#ifdef EMSCRIPTEN
static int try_get_fd(QemuSocketState *s)
{
    int sock= s->fd;
    fd_set wfds;
    struct sockaddr_in qemuAddr;

    if (s->tmpfd < 0) {
      if ((sock = socket(PF_INET, SOCK_STREAM, IPPROTO_TCP)) < 0) {
        BX_INFO(("failed to prepare socket: %s", strerror(errno)));
        return -1;
      }
      // connect to the proxy
      memset(&qemuAddr, 0, sizeof(qemuAddr));
      qemuAddr.sin_family = AF_INET;
      int cret = connect(sock, (struct sockaddr *) &qemuAddr, sizeof(qemuAddr));
      bool connected = false;
      if ((cret == 0) || (errno == EISCONN)) {
        BX_INFO(("socket connected"));
        s->fd = sock;
        s->tmpfd = -1;
        reset_qemu_socket_state(s);
        return 0;
      } else if (errno != EINPROGRESS) {
        BX_INFO(("failed to connect: %s", strerror(errno)));
        s->fd = -1;
        s->tmpfd = -1;
        return -1;
      }
      s->retrynum = 0;
      s->tmpfd = sock;
    }

    BX_INFO(("waiting for connection..."));
    struct timeval tv;
    tv.tv_sec = 0;
    tv.tv_usec = 0;
    FD_ZERO(&wfds);
    FD_SET(s->tmpfd, &wfds);
    int sret = select(s->tmpfd + 1, NULL, &wfds, NULL, &tv);
    if (sret > 0) {
      s->fd = s->tmpfd;
      s->tmpfd = -1;
      reset_qemu_socket_state(s);
      return 0;
    } else if (sret < 0) {
      s->fd = -1;
      s->tmpfd = -1;
    } // else, still wait for the connection.
    BX_INFO(("select result: %d", sret));

    s->retrynum++;
    if (s->retrynum > 5) { // TODO: make max retry number configurable
      close(s->tmpfd);
      s->tmpfd = -1; // too many errors. retry connection.
    }

    return -1;
}
#elif defined(WASI)
static int try_get_fd(QemuSocketState *s)
{
  int sock = 3;
  if ((s->raw_flag != NULL) && (!strncmp(s->raw_flag, "qemu=", 5))) {
    // TODO: allow specifying more options
    if (!strncmp(s->raw_flag + 5, "listenfd=", 9)) {
      sock = atoi(s->raw_flag + 5 + 9);
    }
  }
  int sock_a = 0;
  // wait for connection
  BX_INFO(("accept trying...(sockfd=%d)", sock));
  sock_a = accept4(sock, NULL, NULL, SOCK_NONBLOCK);
  if (sock_a > 0) {
    BX_INFO(("accepted fd=%d", sock_a));
    s->fd = sock_a;
    reset_qemu_socket_state(s);
    return 0;
  }
  BX_INFO(("failed to accept socket: %s", strerror(errno)));
  return -1;
}
#endif

int start_qemu_net(char *flag)
{
  if (qemu_net != NULL) {
    ((QemuSocketState *)qemu_net->opaque)->enabled = true;
    ((QemuSocketState *)qemu_net->opaque)->raw_flag = flag;
  }
  return 0;
}
#endif /* BX_SUPPORT_PCI */

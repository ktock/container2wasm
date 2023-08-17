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

#ifndef BX_WASM_H
#define BX_WASM_H

#include "iodev/iodev.h"

/////////////////////////////////////////////////////////////////////////
// WASI preopens
/////////////////////////////////////////////////////////////////////////
typedef struct {
  char *contents;
  int len;
  int lim;
} FSVirtFile;

int write_info(FSVirtFile *f, int pos, int len, const char *str);
int putchar_info(FSVirtFile *f, int pos, char c);

#ifdef WASI
typedef struct preopen {
    /// The path prefix associated with the file descriptor.
    const char *prefix;

    /// The file descriptor.
    __wasi_fd_t fd;
} preopen;

int populate_preopens();
int write_preopen_info(FSVirtFile *f, int pos1);
#endif

/////////////////////////////////////////////////////////////////////////
// Utilities
/////////////////////////////////////////////////////////////////////////
struct list_head {
    struct list_head *prev;
    struct list_head *next;
};

/* return the pointer of type 'type *' containing 'el' as field 'member' */
#define list_entry(el, type, member) \
    ((type *)((uint8_t *)(el) - offsetof(type, member)))

static inline void init_list_head(struct list_head *head)
{
    head->prev = head;
    head->next = head;
}

/* insert 'el' between 'prev' and 'next' */
static inline void __list_add(struct list_head *el, 
                              struct list_head *prev, struct list_head *next)
{
    prev->next = el;
    el->prev = prev;
    el->next = next;
    next->prev = el;
}

/* add 'el' at the head of the list 'head' (= after element head) */
static inline void list_add(struct list_head *el, struct list_head *head)
{
    __list_add(el, head, head->next);
}

/* add 'el' at the end of the list 'head' (= before element head) */
static inline void list_add_tail(struct list_head *el, struct list_head *head)
{
    __list_add(el, head->prev, head);
}

static inline void list_del(struct list_head *el)
{
    struct list_head *prev, *next;
    prev = el->prev;
    next = el->next;
    prev->next = next;
    next->prev = prev;
    el->prev = NULL; /* fail safe */
    el->next = NULL; /* fail safe */
}

static inline int list_empty(struct list_head *el)
{
    return el->next == el;
}

#define list_for_each(el, head) \
  for(el = (head)->next; el != (head); el = el->next)

#define list_for_each_safe(el, el1, head)                \
    for(el = (head)->next, el1 = el->next; el != (head); \
        el = el1, el1 = el->next)

#define list_for_each_prev(el, head) \
  for(el = (head)->prev; el != (head); el = el->prev)

#define list_for_each_prev_safe(el, el1, head)           \
    for(el = (head)->prev, el1 = el->prev; el != (head); \
        el = el1, el1 = el->prev)

/////////////////////////////////////////////////////////////////////////
// Filesystem
/////////////////////////////////////////////////////////////////////////
typedef struct FSDevice FSDevice;
typedef struct FSFile FSFile;

typedef struct {
    uint32_t f_bsize;
    uint64_t f_blocks;
    uint64_t f_bfree;
    uint64_t f_bavail;
    uint64_t f_files;
    uint64_t f_ffree;
} FSStatFS;

typedef struct {
    uint8_t type; /* P9_IFx */
    uint32_t version;
    uint64_t path;
} FSQID;

typedef struct {
    FSQID qid;
    uint32_t st_mode;
    uint32_t st_uid;
    uint32_t st_gid;
    uint64_t st_nlink;
    uint64_t st_rdev;
    uint64_t st_size;
    uint64_t st_blksize;
    uint64_t st_blocks;
    uint64_t st_atime_sec;
    uint32_t st_atime_nsec;
    uint64_t st_mtime_sec;
    uint32_t st_mtime_nsec;
    uint64_t st_ctime_sec;
    uint32_t st_ctime_nsec;
} FSStat;

#define P9_LOCK_TYPE_RDLCK 0
#define P9_LOCK_TYPE_WRLCK 1
#define P9_LOCK_TYPE_UNLCK 2

#define P9_LOCK_FLAGS_BLOCK 1
#define P9_LOCK_FLAGS_RECLAIM 2

#define P9_LOCK_SUCCESS 0
#define P9_LOCK_BLOCKED 1
#define P9_LOCK_ERROR   2
#define P9_LOCK_GRACE   3

typedef struct {
    uint8_t type;
    uint32_t flags;
    uint64_t start;
    uint64_t length;
    uint32_t proc_id;
    char *client_id;
} FSLock;

struct FSDevice {
    void (*fs_end)(FSDevice *s);
    void (*fs_delete)(FSDevice *s, FSFile *f);
    void (*fs_statfs)(FSDevice *fs, FSStatFS *st);
    int (*fs_attach)(FSDevice *fs, FSFile **pf, FSQID *qid, uint32_t uid,
                     const char *uname, const char *aname);
    int (*fs_walk)(FSDevice *fs, FSFile **pf, FSQID *qids,
                   FSFile *f, int n, char **names);
    int (*fs_mkdir)(FSDevice *fs, FSQID *qid, FSFile *f,
                    const char *name, uint32_t mode, uint32_t gid);
    int (*fs_open)(FSDevice *fs, FSQID *qid, FSFile *f, uint32_t flags, void *opaque);
    int (*fs_create)(FSDevice *fs, FSQID *qid, FSFile *f, const char *name, 
                     uint32_t flags, uint32_t mode, uint32_t gid);
    int (*fs_stat)(FSDevice *fs, FSFile *f, FSStat *st);
    int (*fs_setattr)(FSDevice *fs, FSFile *f, uint32_t mask,
                      uint32_t mode, uint32_t uid, uint32_t gid,
                      uint64_t size, uint64_t atime_sec, uint64_t atime_nsec,
                      uint64_t mtime_sec, uint64_t mtime_nsec);
    void (*fs_close)(FSDevice *fs, FSFile *f);
    int (*fs_readdir)(FSDevice *fs, FSFile *f, uint64_t offset,
                      uint8_t *buf, int count);
    int (*fs_read)(FSDevice *fs, FSFile *f, uint64_t offset,
            uint8_t *buf, int count);
    int (*fs_write)(FSDevice *fs, FSFile *f, uint64_t offset,
             const uint8_t *buf, int count);
    int (*fs_link)(FSDevice *fs, FSFile *df, FSFile *f, const char *name);
    int (*fs_symlink)(FSDevice *fs, FSQID *qid,
                      FSFile *f, const char *name, const char *symgt, uint32_t gid);
    int (*fs_mknod)(FSDevice *fs, FSQID *qid,
                    FSFile *f, const char *name, uint32_t mode, uint32_t major,
                    uint32_t minor, uint32_t gid);
    int (*fs_readlink)(FSDevice *fs, char *buf, int buf_size, FSFile *f);
    int (*fs_renameat)(FSDevice *fs, FSFile *f, const char *name, 
                       FSFile *new_f, const char *new_name);
    int (*fs_unlinkat)(FSDevice *fs, FSFile *f, const char *name);
    int (*fs_lock)(FSDevice *fs, FSFile *f, const FSLock *lock);
    int (*fs_getlock)(FSDevice *fs, FSFile *f, FSLock *lock);
};

struct FSFile {
    Bit32u uid;
    char *path; /* complete path */
    bool is_opened;
    bool is_dir;
    union {
        int fd;
        //DIR *dirp;
    } u;
};

typedef struct {
    struct list_head link;
    uint32_t fid;
    FSFile *fd;
} FIDDesc;

typedef struct {
    FSDevice common;
    char *root_path;
    char *info_path;
    FSVirtFile *info;
} FSDeviceDisk;

/////////////////////////////////////////////////////////////////////////
// Virtio-PCI
/////////////////////////////////////////////////////////////////////////
#define BX_VIRTIO_THIS this->

typedef uint64_t virtio_phys_addr_t;

typedef struct {
    uint32_t ready; /* 0 or 1 */
    uint32_t num;
    uint16_t last_avail_idx;
    virtio_phys_addr_t desc_addr;
    virtio_phys_addr_t avail_addr;
    virtio_phys_addr_t used_addr;
    bool manual_recv; /* if TRUE, the device_recv() callback is not called */
} QueueState;

/* PCI registers */
#define VIRTIO_PCI_DEVICE_FEATURE_SEL	0x000
#define VIRTIO_PCI_DEVICE_FEATURE	0x004
#define VIRTIO_PCI_GUEST_FEATURE_SEL	0x008
#define VIRTIO_PCI_GUEST_FEATURE	0x00c
#define VIRTIO_PCI_MSIX_CONFIG          0x010
#define VIRTIO_PCI_NUM_QUEUES           0x012
#define VIRTIO_PCI_DEVICE_STATUS        0x014
#define VIRTIO_PCI_CONFIG_GENERATION    0x015
#define VIRTIO_PCI_QUEUE_SEL		0x016
#define VIRTIO_PCI_QUEUE_SIZE	        0x018
#define VIRTIO_PCI_QUEUE_MSIX_VECTOR    0x01a
#define VIRTIO_PCI_QUEUE_ENABLE         0x01c
#define VIRTIO_PCI_QUEUE_NOTIFY_OFF     0x01e
#define VIRTIO_PCI_QUEUE_DESC_LOW	0x020
#define VIRTIO_PCI_QUEUE_DESC_HIGH	0x024
#define VIRTIO_PCI_QUEUE_AVAIL_LOW	0x028
#define VIRTIO_PCI_QUEUE_AVAIL_HIGH	0x02c
#define VIRTIO_PCI_QUEUE_USED_LOW	0x030
#define VIRTIO_PCI_QUEUE_USED_HIGH	0x034

#define VIRTIO_PCI_CFG_OFFSET          0x0000
#define VIRTIO_PCI_ISR_OFFSET          0x1000
#define VIRTIO_PCI_CONFIG_OFFSET       0x2000
#define VIRTIO_PCI_NOTIFY_OFFSET       0x3000

#define VIRTIO_PCI_CAP_LEN 16

/* PCI config addresses */
#define PCI_VENDOR_ID		0x00	/* 16 bits */
#define PCI_DEVICE_ID		0x02	/* 16 bits */
#define PCI_COMMAND		0x04	/* 16 bits */
#define PCI_COMMAND_IO		(1 << 0)
#define PCI_COMMAND_MEMORY	(1 << 1)
#define PCI_STATUS		0x06	/* 16 bits */
#define  PCI_STATUS_CAP_LIST	(1 << 4)
#define PCI_CLASS_PROG		0x09
#define PCI_SUBSYSTEM_VENDOR_ID	0x2c    /* 16 bits */
#define PCI_SUBSYSTEM_ID	0x2e    /* 16 bits */
#define PCI_CAPABILITY_LIST	0x34    /* 8 bits */
#define PCI_INTERRUPT_LINE	0x3c    /* 8 bits */
#define PCI_INTERRUPT_PIN	0x3d    /* 8 bits */

#define MAX_QUEUE 8
#define MAX_CONFIG_SPACE_SIZE 256
#define MAX_QUEUE_NUM 16

#define VRING_DESC_F_NEXT	1
#define VRING_DESC_F_WRITE	2
#define VRING_DESC_F_INDIRECT	4

typedef struct {
    Bit64u addr;
    Bit32u len;
    Bit16u flags; /* VRING_DESC_F_x */
    Bit16u next;
} VIRTIODesc;

class bx_virtio_ctrl_c : public bx_pci_device_c {
public:
  bx_virtio_ctrl_c();
  virtual ~bx_virtio_ctrl_c();

  static bool mem_write_handler(bx_phy_address addr, unsigned len, void *data, void *param);
  static bool mem_read_handler(bx_phy_address addr, unsigned len, void *data, void *param);
  
  void init(char *plugin_name, Bit16u pci_device_id, Bit32u class_id, Bit16u device_id, Bit32u device_features, const char *descr);
  int memcpy_from_queue(void *buf, int queue_idx, int desc_idx, int offset, int count);
  int memcpy_to_queue(int queue_idx, int desc_idx, int offset, void *buf, int count);
  void virtio_consume_desc(int queue_idx, int desc_idx, int desc_len);

  virtual int device_recv(int queue_idx, int desc_idx, int read_size, int write_size);

  Bit32u config_space_size; /* in bytes, must be multiple of 4 */
  Bit8u config_space[MAX_CONFIG_SPACE_SIZE];

  struct {
    Bit8u devfunc;
    Bit32u int_status;
    Bit32u status;
    Bit32u device_features_sel;
    Bit32u queue_sel; /* currently selected queue */
    QueueState queue[MAX_QUEUE];

    /* device specific */
    Bit32u device_features;
    Bit32u next_cap_offset;
  } s;

  int get_desc_rw_size(int *pread_size, int *pwrite_size, int queue_idx, int desc_idx);


private:
  bool mem_read(bx_phy_address addr, unsigned len, void *data);
  bool mem_write(bx_phy_address addr, unsigned len, void *data);
  int pci_add_capability(Bit8u *buf, int size);
  void virtio_add_pci_capability(int cfg_type, Bit8u bar, Bit32u offset, Bit32u len, Bit32u mult);
  void virtio_config_write(Bit32u offset, Bit32u val, unsigned len);
  Bit32u virtio_config_read(uint32_t offset, unsigned len);
  int get_desc(VIRTIODesc *desc, int queue_idx, int desc_idx);
  void queue_notify(int queue_idx);
  void virtio_reset();
  int memcpy_to_from_queue(Bit8u *buf, int queue_idx, int desc_idx, int offset, int count, bool to_queue);
  int virtio_memcpy_to_ram(virtio_phys_addr_t addr,  Bit8u *buf, int count);
};

/////////////////////////////////////////////////////////////////////////
// Virtio-9p
/////////////////////////////////////////////////////////////////////////
#define BX_VIRTIO_9P_THIS this->

/* FSQID.type */
#define P9_QTDIR 0x80
#define P9_QTAPPEND 0x40
#define P9_QTEXCL 0x20
#define P9_QTMOUNT 0x10
#define P9_QTAUTH 0x08
#define P9_QTTMP 0x04
#define P9_QTSYMLINK 0x02
#define P9_QTLINK 0x01
#define P9_QTFILE 0x00

/* mode bits */
#define P9_S_IRWXUGO 0x01FF
#define P9_S_ISVTX   0x0200
#define P9_S_ISGID   0x0400
#define P9_S_ISUID   0x0800

#define P9_S_IFMT   0xF000
#define P9_S_IFIFO  0x1000
#define P9_S_IFCHR  0x2000
#define P9_S_IFDIR  0x4000
#define P9_S_IFBLK  0x6000
#define P9_S_IFREG  0x8000
#define P9_S_IFLNK  0xA000
#define P9_S_IFSOCK 0xC000

/* flags for lopen()/lcreate() */
#define P9_O_RDONLY        0x00000000
#define P9_O_WRONLY        0x00000001
#define P9_O_RDWR          0x00000002
#define P9_O_NOACCESS      0x00000003
#define P9_O_CREAT         0x00000040
#define P9_O_EXCL          0x00000080
#define P9_O_NOCTTY        0x00000100
#define P9_O_TRUNC         0x00000200
#define P9_O_APPEND        0x00000400
#define P9_O_NONBLOCK      0x00000800
#define P9_O_DSYNC         0x00001000
#define P9_O_FASYNC        0x00002000
#define P9_O_DIRECT        0x00004000
#define P9_O_LARGEFILE     0x00008000
#define P9_O_DIRECTORY     0x00010000
#define P9_O_NOFOLLOW      0x00020000
#define P9_O_NOATIME       0x00040000
#define P9_O_CLOEXEC       0x00080000
#define P9_O_SYNC          0x00100000

/* for fs_setattr() */
#define P9_SETATTR_MODE      0x00000001
#define P9_SETATTR_UID       0x00000002
#define P9_SETATTR_GID       0x00000004
#define P9_SETATTR_SIZE      0x00000008
#define P9_SETATTR_ATIME     0x00000010
#define P9_SETATTR_MTIME     0x00000020
#define P9_SETATTR_CTIME     0x00000040
#define P9_SETATTR_ATIME_SET 0x00000080
#define P9_SETATTR_MTIME_SET 0x00000100

#define P9_EPERM     1
#define P9_ENOENT    2
#define P9_EIO       5
#define	P9_EEXIST    17
#define	P9_ENOTDIR   20
#define P9_EINVAL    22
#define	P9_ENOSPC    28
#define P9_ENOTEMPTY 39
#define P9_EPROTO    71
#define P9_ENOTSUP   524

typedef struct VIRTIO9PDevice {
    FSDevice *fs;
    int msize; /* maximum message size */
    struct list_head fid_list; /* list of FIDDesc */
} VIRTIO9PDevice;

typedef struct {
    VIRTIO9PDevice *dev;
    int queue_idx;
    int desc_idx;
    uint16_t tag;
} P9OpenInfo;

class bx_virtio_9p_ctrl_c : public bx_virtio_ctrl_c {
public:
  bx_virtio_9p_ctrl_c(char *plugin_name, char *mount_tag, FSDevice *fs);
  virtual ~bx_virtio_9p_ctrl_c();
  virtual void init(void);
  virtual int device_recv(int queue_idx, int desc_idx, int read_size, int write_size);

  FIDDesc *fid_find1(Bit32u fid);
  FSFile *fid_find(uint32_t fid);
  void fid_delete(uint32_t fid);
  void fid_set(uint32_t fid, FSFile *fd);
  void virtio_9p_send_reply(int queue_idx, int desc_idx, Bit8u id, Bit16u tag, Bit8u *buf, int buf_len);
  void virtio_9p_send_error(int queue_idx, int desc_idx, Bit16u tag, Bit32u error);
  void virtio_9p_open_reply(FSDevice *fs, FSQID *qid, int err, P9OpenInfo *oi);
  int unmarshall(int queue_idx, int desc_idx, int *poffset, const char *fmt, ...);

private:
  char *mount_tag;
  char *plugin_name;
  VIRTIO9PDevice p9fs;
};

FSVirtFile *get_vm_info();

/////////////////////////////////////////////////////////////////////////
// Virtio-console
/////////////////////////////////////////////////////////////////////////
#define BX_VIRTIO_CONSOLE_THIS this->

typedef struct bx_virtio_console_ctrl_c bx_virtio_console_ctrl_c;

typedef struct {
    int stdin_fd;
    int console_esc_state;
    bool resize_pending;
} STDIODevice;

typedef struct {
    void *opaque;
    void (*write_data)(void *opaque, const uint8_t *buf, int len);
    int (*read_data)(void *opaque, uint8_t *buf, int len);
} CharacterDevice;

typedef struct VIRTIOConsoleDevice {
    CharacterDevice *cs;
} VIRTIOConsoleDevice;

class bx_virtio_console_ctrl_c : public bx_virtio_ctrl_c {
public:
  bx_virtio_console_ctrl_c(char *plugin_name, CharacterDevice *cs);
  virtual ~bx_virtio_console_ctrl_c();
  virtual void init(void);
  virtual int device_recv(int queue_idx, int desc_idx, int read_size, int write_size);

  bool virtio_console_can_write_data();
  int virtio_console_get_write_len();
  int virtio_console_write_data(const uint8_t *buf, int buf_len);
  void virtio_console_resize_event(int width, int height);

  static void rx_timer_handler(void *this_ptr);

private:
  char *plugin_name;
  int timer_id;
  VIRTIOConsoleDevice dev;
};

#endif

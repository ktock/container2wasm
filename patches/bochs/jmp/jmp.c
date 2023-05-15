// Inspierd by Alon Zakai's Asyncify-based setjmp/longjmp implementation and patched for our usecase.
// Source: https://github.com/kripken/talks/blob/991fb1e4b6d7e4b0ea6b3e462d5643f11d422771/jmp.c
// License: MIT License (https://github.com/kripken/talks/blob/991fb1e4b6d7e4b0ea6b3e462d5643f11d422771/LICENSE)
// Copyright (C) 2011-2012 Hakim El Hattab, http://hakim.se

#include "jmp.h"

#define NULL 0

#define NOINLINE __attribute__((noinline))

NOINLINE
void async_buf_init(struct async_buf* buf) {
  buf->top = &buf->buffer[0];
  buf->end = &buf->buffer[ASYNC_BUF_BUFFER_SIZE];
}

NOINLINE
void async_buf_note_unwound(struct async_buf* buf) {
  buf->unwound = buf->top;
}

NOINLINE
void async_buf_rewind(struct async_buf* buf) {
  buf->top = buf->unwound;
}

static jmp_buf* __active_jmp_buf = NULL;

NOINLINE
__attribute__((export_name("wasm_setjmp_internal")))
int wasm_setjmp_internal(jmp_buf* buf) {
  if (buf->state == 0) {
    buf->value = 0;
    buf->state = 1;
    __active_jmp_buf = buf;
    async_buf_init(&buf->setjmp_buf);
    asyncify_start_unwind(&buf->setjmp_buf);
  } else {
    asyncify_stop_rewind();
    if (buf->state == 2) {
      // We returned from the longjmp, all done.
      __active_jmp_buf = NULL;
    }
  }
  return buf->value;
}

NOINLINE
__attribute__((export_name("wasm_longjmp")))
void wasm_longjmp(jmp_buf* buf, int value) {
  buf->value = value;
  buf->state = 2;
  __active_jmp_buf = buf;
  async_buf_init(&buf->longjmp_buf);
  asyncify_start_unwind(&buf->longjmp_buf);
}

NOINLINE
__attribute__((export_name("handle_jmp")))
void *handle_jmp() {
  if (!__active_jmp_buf) {
    return NULL;
  }
  if (__active_jmp_buf->state == 1) {
    // Setjmp unwound to here. Prepare to rewind it twice.
    async_buf_note_unwound(&__active_jmp_buf->setjmp_buf);
  } else if (__active_jmp_buf->state == 2) {
    // Longjmp unwound to here. Rewind to the setjmp.
    async_buf_rewind(&__active_jmp_buf->setjmp_buf);
  }
  return &__active_jmp_buf->setjmp_buf;
}

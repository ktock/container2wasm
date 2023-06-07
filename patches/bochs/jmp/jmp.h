// Inspierd by Alon Zakai's Asyncify-based setjmp/longjmp implementation and patched for our usecase.
// Source: https://github.com/kripken/talks/blob/991fb1e4b6d7e4b0ea6b3e462d5643f11d422771/jmp.c
// License: MIT License (https://github.com/kripken/talks/blob/991fb1e4b6d7e4b0ea6b3e462d5643f11d422771/LICENSE)
// Copyright (C) 2011-2012 Hakim El Hattab, http://hakim.se

#ifndef BX_JMP_H
#define BX_JMP_H

#define WASM_IMPORT(module, base) \
  __attribute__((__import_module__(#module), __import_name__(#base)))
void asyncify_start_unwind(void* buf) WASM_IMPORT(asyncify, start_unwind);
void asyncify_stop_unwind() WASM_IMPORT(asyncify, stop_unwind);
void asyncify_start_rewind(void* buf) WASM_IMPORT(asyncify, start_rewind);
void asyncify_stop_rewind() WASM_IMPORT(asyncify, stop_rewind);

#define ASYNC_BUF_BUFFER_SIZE 65536

// An Asyncify buffer.
struct async_buf {
  void* top; // current top of the used part of tjhe buffer
  void* end; // fixed end of the buffer
  void* unwound; // top of the buffer when full (unwound and ready to rewind)
  char buffer[ASYNC_BUF_BUFFER_SIZE];
};

// A setjmp/longjmp buffer.
struct wasm_jmp_buf {
  // A buffer for the setjmp. Unwound and rewound immediately, and can
  // be rewound a second time to get to the setjmp from the longjmp.
  struct async_buf setjmp_buf;
  // A buffer for the longjmp. Unwound once and never rewound.
  struct async_buf longjmp_buf;
  // The value to return.
  int value;
  // FIXME We assume this is initialized to zero.
  int state;
};

typedef struct wasm_jmp_buf jmp_buf;

int wasm_setjmp(jmp_buf* buf);
void wasm_longjmp(jmp_buf* buf, int value);

#define setjmp(env) ((env).state = 0, wasm_setjmp(&(env)))
#define longjmp(env, payload) wasm_longjmp(&env, payload)
void *handle_jmp();

#endif

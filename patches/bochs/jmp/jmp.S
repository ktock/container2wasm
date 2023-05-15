# setjmp wrapper for saving and restoring on-memory stack pointer, inspired by 
# Ruby's setjmp implementation.
# Source: https://github.com/ruby/ruby/blob/v3_2_2/wasm/setjmp_core.S
# License: 2-clause BSDL (https://github.com/ruby/ruby/blob/master/COPYING)
# Copyright (C) 1993-2013 Yukihiro Matsumoto. All rights reserved.
# 
# TODO: this should be handled by Asyncify maybe.
  .globl wasm_setjmp

  .globaltype __stack_pointer, i32

  .functype wasm_setjmp_internal(i32) -> (i32)

wasm_setjmp:
  .functype wasm_setjmp(i32) -> (i32)
  .local i32, i32
  
  # save stack pointer to local variable (captured to asyncify stack)
  global.get __stack_pointer
  local.set 1

  local.get 0
  call wasm_setjmp_internal
  
  # restore stack pointer (local variable has been restored from asyncify stack)
  local.get 1
  global.set __stack_pointer
  
  end_function

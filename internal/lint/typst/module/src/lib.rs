//! typst2vast behind three exports: the host writes a document into memory
//! it asked for, converts it, and reads the HTML back.

use std::alloc::{alloc, dealloc, Layout};

fn layout(len: usize) -> Layout {
    // Never zero-sized: the allocator does not take those.
    Layout::from_size_align(len.max(1), 1).unwrap()
}

/// Reserves `len` bytes for the host to write into.
#[no_mangle]
pub extern "C" fn t2v_alloc(len: usize) -> *mut u8 {
    unsafe { alloc(layout(len)) }
}

/// Releases what `t2v_alloc` or `t2v_convert` returned.
///
/// # Safety
/// `ptr` and `len` must be a pair one of them handed out.
#[no_mangle]
pub unsafe extern "C" fn t2v_free(ptr: *mut u8, len: usize) {
    dealloc(ptr, layout(len))
}

/// Converts the document at `ptr`, returning the HTML's address in the high
/// half of the result and its length in the low half.
///
/// # Safety
/// `ptr` must hold `len` readable bytes.
#[no_mangle]
pub unsafe extern "C" fn t2v_convert(ptr: *const u8, len: usize) -> u64 {
    let doc = String::from_utf8_lossy(std::slice::from_raw_parts(ptr, len));
    let html = typst2vast::to_vale_ast(&doc).into_bytes();

    let n = html.len();
    let out = alloc(layout(n));
    std::ptr::copy_nonoverlapping(html.as_ptr(), out, n);

    ((out as u64) << 32) | n as u64
}

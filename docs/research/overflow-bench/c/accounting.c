/* The accounting-sum kernel with the overflow builtins that Tov emits
   (crates/tov/src/codegen/mod.rs L1879-L1881 at ae0f67f5), for comparison
   with the Go variants in ../kernels.go. It is compiled for its assembly
   only, not timed:

       gcc -O2 -S -o - docs/research/overflow-bench/c/accounting.c

   The Go compiler has no equivalent of these builtins, so this is the
   instruction sequence a C or LLVM backend reaches and direct-Go output
   cannot. */
#include <stdint.h>

typedef struct {
    int64_t qty, price, discount;
} item;

extern void trap(void) __attribute__((noreturn, cold));

int64_t sum_wrap(const item *items, long n) {
    uint64_t total = 0;
    for (long i = 0; i < n; i++) {
        total += (uint64_t)items[i].qty * (uint64_t)items[i].price - (uint64_t)items[i].discount;
    }
    return (int64_t)total;
}

int64_t sum_checked(const item *items, long n) {
    int64_t total = 0;
    for (long i = 0; i < n; i++) {
        int64_t line, net;
        if (__builtin_expect(__builtin_mul_overflow(items[i].qty, items[i].price, &line), 0)) trap();
        if (__builtin_expect(__builtin_sub_overflow(line, items[i].discount, &net), 0)) trap();
        if (__builtin_expect(__builtin_add_overflow(total, net, &total), 0)) trap();
    }
    return total;
}

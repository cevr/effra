# #108 overflow-bench run 20261027T150404Z

```
date: 2026-10-27T15:04:04Z
head: eb2a376175874481fdc14869b0d98a8492383eea
package clean: false
go: go version go1.27.0 linux/amd64
go env GOAMD64 GOARCH GOOS: v1 amd64 linux
kernel: Linux 6.12.93 x86_64
cpu: AMD EPYC 9554P 64-Core Processor
logical cpus: 16
loadavg at start: 19.68 18.08 15.10 7/1505 3323104
```

## Host load (one-minute load average, sampled every 10 s)

| Phase | Samples | Median | Min | Max |
| --- | ---: | ---: | ---: | ---: |
| env | 2 | 19.68 | 19.68 | 19.68 |
| tests | 1 | 19.68 | 19.68 | 19.68 |
| count10 | 41 | 4.68 | 3.30 | 19.68 |
| interleaved | 41 | 5.02 | 3.63 | 18.89 |
| asm | 1 | 16.44 | 16.44 | 16.44 |

## Contiguous run (-count 10)

| Benchmark | n | Median ns/op | Min | Max | Median vs wrap | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| AccountingSum/wrap | 10 | 802 | 704 | 958 | 1.00x | 0 | 0 |
| AccountingSum/defect-cmp | 10 | 4008 | 3530 | 4187 | 5.00x | 0 | 0 |
| AccountingSum/defect-post | 10 | 2384 | 2084 | 2641 | 2.97x | 0 | 0 |
| AccountingSum/defect-xor | 10 | 2233 | 2084 | 2489 | 2.79x | 0 | 0 |
| AccountingSum/defect-bits | 10 | 2809 | 2666 | 3305 | 3.50x | 0 | 0 |
| AccountingSum/defect-range | 10 | 1714 | 1581 | 1895 | 2.14x | 0 | 0 |
| AccountingSum/fail-cmp | 10 | 4108 | 3584 | 4515 | 5.12x | 0 | 0 |
| AccountingSum/fail-post | 10 | 2940 | 2566 | 4145 | 3.67x | 0 | 0 |
| AccountingSum/fail-xor | 10 | 2298 | 2245 | 2855 | 2.87x | 0 | 0 |
| AccountingSum/fail-bits | 10 | 3376 | 3179 | 3797 | 4.21x | 0 | 0 |
| AccountingSum/fail-range | 10 | 2078 | 1785 | 2643 | 2.59x | 0 | 0 |
| HashMix/short/wrap | 10 | 2250 | 2185 | 2510 | 1.00x | 0 | 0 |
| HashMix/short/explicit-wrap | 10 | 2304 | 2016 | 2573 | 1.02x | 0 | 0 |
| HashMix/short/defect-cmp | 10 | 4450 | 3988 | 4920 | 1.98x | 0 | 0 |
| HashMix/short/defect-bits | 10 | 5028 | 4745 | 5975 | 2.24x | 0 | 0 |
| HashMix/short/fail-cmp | 10 | 6274 | 5486 | 7227 | 2.79x | 0 | 0 |
| HashMix/short/fail-bits | 10 | 5128 | 4752 | 6598 | 2.28x | 0 | 0 |
| HashMix/long/wrap | 10 | 3577 | 3513 | 3819 | 1.00x | 0 | 0 |
| HashMix/long/explicit-wrap | 10 | 3493 | 3432 | 4072 | 0.98x | 0 | 0 |
| Op/add/wrap | 10 | 356 | 334 | 448 | 1.00x | 0 | 0 |
| Op/add/cmp | 10 | 1742 | 1537 | 2031 | 4.90x | 0 | 0 |
| Op/add/post | 10 | 669 | 650 | 784 | 1.88x | 0 | 0 |
| Op/add/xor | 10 | 622 | 592 | 667 | 1.75x | 0 | 0 |
| Op/add/bits | 10 | 995 | 955 | 1065 | 2.80x | 0 | 0 |
| Op/sub/wrap | 10 | 398 | 347 | 512 | 1.00x | 0 | 0 |
| Op/sub/cmp | 10 | 1692 | 1460 | 2130 | 4.25x | 0 | 0 |
| Op/sub/post | 10 | 693 | 644 | 814 | 1.74x | 0 | 0 |
| Op/sub/xor | 10 | 526 | 464 | 587 | 1.32x | 0 | 0 |
| Op/sub/bits | 10 | 983 | 921 | 1108 | 2.47x | 0 | 0 |
| Op/mul/wrap | 10 | 537 | 472 | 596 | 1.00x | 0 | 0 |
| Op/mul/cmp | 10 | 2304 | 2162 | 2845 | 4.29x | 0 | 0 |
| Op/mul/bits | 10 | 1358 | 1239 | 1494 | 2.53x | 0 | 0 |
| Op/mul/range | 10 | 1097 | 998 | 1173 | 2.04x | 0 | 0 |

Raw ns/op samples, in run order:

```
AccountingSum/wrap: 785 745 704 761 773 894 842 958 818 849
AccountingSum/defect-cmp: 3857 3828 4135 3530 4069 4084 3815 3948 4187 4129
AccountingSum/defect-post: 2399 2368 2188 2455 2084 2641 2329 2347 2480 2537
AccountingSum/defect-xor: 2094 2283 2252 2084 2214 2417 2489 2375 2117 2204
AccountingSum/defect-bits: 3272 3305 2957 2989 2831 2679 2704 2787 2724 2666
AccountingSum/defect-range: 1739 1581 1718 1895 1710 1660 1671 1733 1667 1816
AccountingSum/fail-cmp: 3584 4515 4416 4291 3633 3746 4161 4056 3923 4209
AccountingSum/fail-post: 2734 2873 2785 2566 3159 3315 4145 3097 3006 2611
AccountingSum/fail-xor: 2312 2245 2285 2486 2540 2273 2268 2462 2252 2855
AccountingSum/fail-bits: 3797 3638 3179 3390 3239 3517 3356 3441 3362 3225
AccountingSum/fail-range: 1785 2000 2148 2602 2643 1920 2010 2031 2125 2271
HashMix/short/wrap: 2294 2260 2215 2437 2253 2193 2246 2510 2185 2203
HashMix/short/explicit-wrap: 2300 2016 2235 2573 2083 2307 2422 2448 2369 2056
HashMix/short/defect-cmp: 4196 4553 4316 3988 4348 4881 4920 4623 4097 4642
HashMix/short/defect-bits: 5105 4952 5165 5226 4937 4745 4822 4838 5454 5975
HashMix/short/fail-cmp: 6493 6152 6448 6407 7227 6237 6193 6312 5499 5486
HashMix/short/fail-bits: 5197 5889 6598 4890 4904 4752 5218 5081 5176 4996
HashMix/long/wrap: 3533 3819 3513 3571 3611 3593 3545 3583 3561 3717
HashMix/long/explicit-wrap: 4072 3628 3531 3432 3493 3432 3493 3585 3469 3487
Op/add/wrap: 335 352 334 349 359 367 370 337 428 448
Op/add/cmp: 2031 1747 1735 1537 1563 1856 1671 1768 1736 1775
Op/add/post: 687 665 650 656 670 694 784 668 660 700
Op/add/xor: 667 592 633 612 592 631 649 612 604 638
Op/add/bits: 994 996 1063 955 997 976 960 986 1065 1029
Op/sub/wrap: 422 449 347 512 373 360 367 422 423 369
Op/sub/cmp: 1675 2130 1658 1642 1541 1708 1809 1762 1460 1794
Op/sub/post: 748 814 777 702 671 704 676 644 678 684
Op/sub/xor: 576 561 541 480 587 511 562 486 474 464
Op/sub/bits: 1050 921 940 998 1030 1108 969 960 1022 938
Op/mul/wrap: 472 532 500 505 528 561 548 542 596 557
Op/mul/cmp: 2845 2815 2497 2357 2187 2367 2162 2217 2251 2238
Op/mul/bits: 1441 1317 1357 1360 1376 1452 1494 1306 1328 1239
Op/mul/range: 1021 1013 1173 1132 1125 1069 1131 1133 1041 998
```

## Interleaved run (one process per sample, shuffled each round)

| Benchmark | n | Median ns/op | Min | Max | Median vs wrap | Paired ratio median [min, max] | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| AccountingSum/wrap | 10 | 779 | 649 | 1296 | 1.00x | 1.00x [1.00, 1.00] | 0 | 0 |
| AccountingSum/defect-cmp | 10 | 3796 | 3189 | 6269 | 4.88x | 4.85x [3.43, 7.12] | 0 | 0 |
| AccountingSum/defect-post | 10 | 2535 | 2192 | 3387 | 3.26x | 3.45x [2.24, 4.28] | 0 | 0 |
| AccountingSum/defect-xor | 10 | 2328 | 1945 | 2720 | 2.99x | 2.72x [1.89, 3.59] | 0 | 0 |
| AccountingSum/defect-bits | 10 | 3068 | 2634 | 4854 | 3.94x | 3.86x [3.19, 5.37] | 0 | 0 |
| AccountingSum/defect-range | 10 | 1801 | 1498 | 2030 | 2.31x | 2.23x [1.48, 2.68] | 0 | 0 |
| AccountingSum/fail-cmp | 10 | 4066 | 3524 | 6160 | 5.22x | 4.82x [3.91, 8.20] | 0 | 0 |
| AccountingSum/fail-post | 10 | 2871 | 2539 | 4025 | 3.69x | 3.51x [3.08, 4.57] | 0 | 0 |
| AccountingSum/fail-xor | 10 | 2632 | 2269 | 3062 | 3.38x | 3.20x [2.13, 3.98] | 0 | 0 |
| AccountingSum/fail-bits | 10 | 3428 | 3028 | 3825 | 4.40x | 4.32x [2.44, 4.88] | 0 | 0 |
| AccountingSum/fail-range | 10 | 2174 | 1898 | 2636 | 2.79x | 2.67x [2.03, 3.53] | 0 | 0 |
| HashMix/short/wrap | 10 | 2394 | 2171 | 3660 | 1.00x | 1.00x [1.00, 1.00] | 0 | 0 |
| HashMix/short/explicit-wrap | 10 | 2388 | 2153 | 3743 | 1.00x | 0.99x [0.82, 1.16] | 0 | 0 |
| HashMix/short/defect-cmp | 10 | 4744 | 4240 | 7388 | 1.98x | 1.92x [1.57, 2.90] | 0 | 0 |
| HashMix/short/defect-bits | 10 | 5714 | 4718 | 7234 | 2.39x | 2.34x [1.94, 2.84] | 0 | 0 |
| HashMix/short/fail-cmp | 10 | 6282 | 4979 | 7351 | 2.62x | 2.46x [1.99, 3.14] | 0 | 0 |
| HashMix/short/fail-bits | 10 | 5719 | 4897 | 8960 | 2.39x | 2.32x [2.05, 3.39] | 0 | 0 |
| HashMix/long/wrap | 10 | 3573 | 3419 | 6178 | 1.00x | 1.00x [1.00, 1.00] | 0 | 0 |
| HashMix/long/explicit-wrap | 10 | 3577 | 3448 | 5009 | 1.00x | 1.00x [0.81, 1.05] | 0 | 0 |
| Op/add/wrap | 10 | 396 | 333 | 578 | 1.00x | 1.00x [1.00, 1.00] | 0 | 0 |
| Op/add/cmp | 10 | 1821 | 1578 | 3252 | 4.60x | 4.47x [4.16, 6.18] | 0 | 0 |
| Op/add/post | 10 | 686 | 633 | 1235 | 1.73x | 1.91x [1.24, 2.21] | 0 | 0 |
| Op/add/xor | 10 | 648 | 614 | 780 | 1.63x | 1.66x [1.12, 2.34] | 0 | 0 |
| Op/add/bits | 10 | 1038 | 899 | 1514 | 2.62x | 2.64x [1.69, 3.20] | 0 | 0 |
| Op/sub/wrap | 10 | 415 | 341 | 560 | 1.00x | 1.00x [1.00, 1.00] | 0 | 0 |
| Op/sub/cmp | 10 | 1718 | 1462 | 2522 | 4.14x | 4.18x [3.21, 4.89] | 0 | 0 |
| Op/sub/post | 10 | 704 | 629 | 1045 | 1.70x | 1.81x [1.40, 2.18] | 0 | 0 |
| Op/sub/xor | 10 | 538 | 482 | 607 | 1.30x | 1.24x [1.02, 1.57] | 0 | 0 |
| Op/sub/bits | 10 | 1023 | 914 | 1264 | 2.47x | 2.52x [1.84, 3.15] | 0 | 0 |
| Op/mul/wrap | 10 | 488 | 445 | 575 | 1.00x | 1.00x [1.00, 1.00] | 0 | 0 |
| Op/mul/cmp | 10 | 2428 | 1944 | 3108 | 4.97x | 4.96x [4.15, 6.34] | 0 | 0 |
| Op/mul/bits | 10 | 1296 | 1174 | 3238 | 2.65x | 2.69x [2.40, 5.63] | 0 | 0 |
| Op/mul/range | 10 | 1174 | 1029 | 1604 | 2.40x | 2.42x [2.09, 3.08] | 0 | 0 |

Raw ns/op samples, in run order:

```
AccountingSum/wrap: 649 938 671 761 745 796 882 1296 880 757
AccountingSum/defect-cmp: 3467 3827 3744 3760 3189 3792 3801 4446 6269 4472
AccountingSum/defect-post: 2485 2515 2555 2192 2295 2434 3370 2900 3387 3238
AccountingSum/defect-xor: 2099 2404 1978 2260 1945 2113 2396 2448 2403 2720
AccountingSum/defect-bits: 2978 3272 2930 2634 2756 3157 2814 4854 4103 4064
AccountingSum/defect-range: 1618 1812 1790 1708 1984 1498 1575 1919 1948 2030
AccountingSum/fail-cmp: 5324 4260 3995 3524 3656 3762 4138 5063 6160 3924
AccountingSum/fail-post: 2770 3180 3017 2904 2708 2677 2838 3998 4025 2539
AccountingSum/fail-xor: 2586 3062 2471 2358 2269 2965 2858 2765 2679 2392
AccountingSum/fail-bits: 3152 3770 3028 3152 3302 3825 3555 3157 3705 3696
AccountingSum/fail-range: 2289 2029 2039 2157 1898 2191 2025 2636 2286 2566
HashMix/short/wrap: 2171 2323 2496 2381 2235 2408 2212 3660 2546 2851
HashMix/short/explicit-wrap: 2421 2493 2355 2267 2153 2329 2571 3743 2761 2326
HashMix/short/defect-cmp: 5363 4757 4854 4351 4240 4528 4732 5740 7388 4561
HashMix/short/defect-bits: 5815 5784 5265 5644 5027 4718 5087 7099 7234 7060
HashMix/short/fail-cmp: 6239 6325 4979 5245 5678 7026 6938 7351 6058 6728
HashMix/short/fail-bits: 4912 6572 5476 5083 4897 4929 5962 8960 8638 6800
HashMix/long/wrap: 3464 3419 3680 3579 3525 3616 3528 3567 6178 3642
HashMix/long/explicit-wrap: 3502 3563 3505 3692 3591 3592 3448 3729 5009 3543
Op/add/wrap: 403 375 379 390 366 416 533 436 578 333
Op/add/cmp: 1709 2039 1578 1633 1592 1771 2448 2691 3252 1871
Op/add/post: 666 817 633 688 643 856 661 961 1235 685
Op/add/xor: 641 688 625 647 642 614 728 743 648 780
Op/add/bits: 1051 1039 1172 1036 993 1036 899 1037 1514 1067
Op/sub/wrap: 345 428 379 341 389 459 497 560 548 402
Op/sub/cmp: 1645 1762 1853 1462 1553 1787 1594 2353 2522 1674
Op/sub/post: 753 629 712 660 695 666 696 848 1045 740
Op/sub/xor: 540 528 489 482 485 535 541 579 558 607
Op/sub/bits: 1016 1131 1030 967 929 983 914 1234 1039 1264
Op/mul/wrap: 473 486 517 468 480 494 445 530 575 490
Op/mul/cmp: 2742 2129 2235 1944 2365 2460 2395 2942 2621 3108
Op/mul/bits: 1245 1331 1242 1174 1227 1403 1684 1571 3238 1261
Op/mul/range: 1093 1225 1350 1029 1123 1032 1110 1604 1307 1509
```
